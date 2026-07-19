// handlers_files.go - Handles file management and WOPI integration for LastWar alliance files.

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

func getStoragePath() string {
	path := os.Getenv("STORAGE_PATH")
	if path == "" {
		return "/var/lib/lastwar/files"
	}
	return path
}

func hasSufficientRank(userRank, requiredRank string) bool {
	uv := rankTier(userRank)
	rv := rankTier(requiredRank)
	if uv == 0 || rv == 0 {
		return false
	}
	return uv >= rv
}

// effectiveUserRank normalizes an auth user to a rank string for comparisons:
// admins are the synthetic "Admin" tier; a user with no member rank is treated as R1.
func effectiveUserRank(user *AuthUser) string {
	if user.IsAdmin {
		return "Admin"
	}
	if user.Rank != "" {
		return user.Rank
	}
	return "R1"
}

// loadAllFileTags returns every file_tag ordered for display. The caller must not
// hold another DB cursor open across this call (single-connection rule).
func loadAllFileTags() ([]FileTag, error) {
	rows, err := db.Query(`SELECT id, name, min_rank, color, sort_order FROM file_tags ORDER BY sort_order, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []FileTag{}
	for rows.Next() {
		var t FileTag
		if err := rows.Scan(&t.ID, &t.Name, &t.MinRank, &t.Color, &t.SortOrder); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// loadFileTagAssignments returns file_id -> ordered tag ids (ordered by the tag's
// sort_order so badges render consistently). Cursor fully closed on return.
func loadFileTagAssignments() (map[int][]int, error) {
	rows, err := db.Query(`
		SELECT m.file_id, m.tag_id
		FROM file_tag_map m
		JOIN file_tags t ON t.id = m.tag_id
		ORDER BY m.file_id, t.sort_order, t.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int][]int)
	for rows.Next() {
		var fileID, tagID int
		if err := rows.Scan(&fileID, &tagID); err != nil {
			return nil, err
		}
		out[fileID] = append(out[fileID], tagID)
	}
	return out, rows.Err()
}

// fileTagIDs returns the tag ids attached to a single file. Cursor closed on return.
func fileTagIDs(fileID int) ([]int, error) {
	rows, err := db.Query(`SELECT tag_id FROM file_tag_map WHERE file_id = ?`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// fileTagMinRanks returns the min_rank of every tag attached to a file. Used by the
// download and WOPI-token paths to compute a file's effective view rank. The file
// row is read via QueryRow before this is called, so no cursor overlaps.
func fileTagMinRanks(fileID int) ([]string, error) {
	rows, err := db.Query(`SELECT t.min_rank FROM file_tag_map m JOIN file_tags t ON t.id = m.tag_id WHERE m.file_id = ?`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ranks []string
	for rows.Next() {
		var mr string
		if err := rows.Scan(&mr); err != nil {
			return nil, err
		}
		ranks = append(ranks, mr)
	}
	return ranks, rows.Err()
}

// effectiveMinRankValue returns the numeric tier a viewer must meet to see a file:
// the max of the file's own min_rank and all its tags' min_ranks. Compared via
// rankTier in Go — never a lexical SQL compare (the "rank is TEXT" gotcha).
func effectiveMinRankValue(fileMinRank string, tagMinRanks []string) int {
	v := rankTier(fileMinRank)
	for _, mr := range tagMinRanks {
		if rv := rankTier(mr); rv > v {
			v = rv
		}
	}
	return v
}

// resolveSubmittedTags validates a submitted set of tag ids against the caller's
// visible tag set (a tag is visible when userRank >= tag.min_rank). Returns the
// de-duplicated submitted ids, the caller's full visible-id list, a lookup map, and
// ok=false when any submitted id is not visible (or does not exist) — callers turn
// ok=false into a 400. Reads all tags once; caller must hold no cursor across it.
func resolveSubmittedTags(submitted []int, userRank string) (out, visibleIDs []int, tagByID map[int]FileTag, ok bool, err error) {
	allTags, err := loadAllFileTags()
	if err != nil {
		return nil, nil, nil, false, err
	}
	tagByID = make(map[int]FileTag, len(allTags))
	visibleSet := make(map[int]bool)
	uv := rankTier(userRank)
	for _, t := range allTags {
		tagByID[t.ID] = t
		if uv >= rankTier(t.MinRank) {
			visibleSet[t.ID] = true
			visibleIDs = append(visibleIDs, t.ID)
		}
	}
	seen := make(map[int]bool)
	for _, id := range submitted {
		if !visibleSet[id] {
			return nil, nil, nil, false, nil
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, visibleIDs, tagByID, true, nil
}

// parseTagIDs converts raw string form values to ints, erroring on any non-numeric
// value (never a silent skip or a zero-value insert). Blank entries are dropped.
func parseTagIDs(raw []string) ([]int, error) {
	ids := make([]int, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// mergeFileTagsTx replaces this user's *visible* tag assignments on a file with the
// submitted set, leaving assignments the user can't see untouched (merge semantics).
// submitted must already be validated as a subset of visibleIDs.
func mergeFileTagsTx(tx *sql.Tx, fileID int, submitted, visibleIDs []int) error {
	if len(visibleIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(visibleIDs)), ",")
		args := make([]interface{}, 0, len(visibleIDs)+1)
		args = append(args, fileID)
		for _, id := range visibleIDs {
			args = append(args, id)
		}
		if _, err := tx.Exec(`DELETE FROM file_tag_map WHERE file_id = ? AND tag_id IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	for _, tid := range submitted {
		if _, err := tx.Exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, ?)`, fileID, tid); err != nil {
			return err
		}
	}
	return nil
}

// shouldLogFileEdit reports whether enough time has elapsed since a file's last
// recorded modification to warrant a fresh activity-log row, throttling the flood of
// Collabora autosaves. The comparison is age-based only (updated_at records when, not
// who) — see the document-history follow-up for per-editor attribution.
func shouldLogFileEdit(prev sql.NullString) bool {
	if !prev.Valid || prev.String == "" {
		return true
	}
	t, err := time.Parse("2006-01-02 15:04:05", prev.String)
	if err != nil {
		return true // unparseable -> don't suppress
	}
	return time.Since(t) >= FileEditLogWindow
}

func getFilesList(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	userID := user.ID
	userRankVal := rankTier(effectiveUserRank(user))

	// Read everything into memory and CLOSE each cursor BEFORE the next query — the
	// app runs on a single DB connection, so a query issued while a cursor is open
	// deadlocks the whole process (see CLAUDE.md).
	allTags, err := loadAllFileTags()
	if err != nil {
		slog.Error("getFilesList: load tags failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	tagByID := make(map[int]FileTag, len(allTags))
	for _, t := range allTags {
		tagByID[t.ID] = t
	}

	tagIDsByFile, err := loadFileTagAssignments()
	if err != nil {
		slog.Error("getFilesList: load tag assignments failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// strftime normalizes both columns to one format ("YYYY-MM-DD HH:MM:SS"). The driver
	// otherwise returns created_at (a DEFAULT CURRENT_TIMESTAMP column) as ISO "…T…Z" but
	// updated_at (set via an explicit CURRENT_TIMESTAMP) as the space form — which would
	// break client date parsing AND make updated_at != created_at for un-edited files.
	rows, err := db.Query(`
		SELECT f.id, f.title, f.file_name, f.file_type, f.min_rank, f.min_edit_rank,
		       strftime('%Y-%m-%d %H:%M:%S', f.created_at),
		       strftime('%Y-%m-%d %H:%M:%S', COALESCE(f.updated_at, f.created_at)),
		       u.username, f.owner_user_id, COALESCE(uu.username, '')
		FROM files f
		JOIN users u ON f.owner_user_id = u.id
		LEFT JOIN users uu ON uu.id = f.updated_by
		ORDER BY f.created_at DESC`)
	if err != nil {
		slog.Error("getFilesList: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Scan every row into memory before doing any per-file logic — the effective-rank
	// filter and tag attachment run in Go, after the cursor is closed.
	type rawFile struct {
		f      AllianceFile
		tagIDs []int
	}
	var raws []rawFile
	for rows.Next() {
		var f AllianceFile
		if err := rows.Scan(&f.ID, &f.Title, &f.FileName, &f.FileType, &f.MinRank, &f.MinEditRank,
			&f.CreatedAt, &f.UpdatedAt, &f.OwnerName, &f.OwnerUserID, &f.UpdatedByName); err != nil {
			slog.Error("getFilesList: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		raws = append(raws, rawFile{f: f, tagIDs: tagIDsByFile[f.ID]})
	}
	if err := rows.Err(); err != nil {
		slog.Error("getFilesList: rows error", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rows.Close()

	files := []AllianceFile{}
	for _, rw := range raws {
		f := rw.f
		f.IsOwner = f.OwnerUserID == userID
		f.Tags = []FileTag{}

		// Effective view rank = max(file.min_rank, tags' min_ranks). Only tags the
		// viewer is cleared for are attached as badges.
		effVal := rankTier(f.MinRank)
		for _, tid := range rw.tagIDs {
			t, exists := tagByID[tid]
			if !exists {
				continue
			}
			if tv := rankTier(t.MinRank); tv > effVal {
				effVal = tv
			}
			if userRankVal >= rankTier(t.MinRank) {
				f.Tags = append(f.Tags, t)
			}
		}

		if user.IsAdmin || f.IsOwner || userRankVal >= effVal {
			files = append(files, f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func uploadFile(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	userID := user.ID

	r.Body = http.MaxBytesReader(w, r.Body, MaxFileUploadSize)
	err := r.ParseMultipartForm(MaxFileUploadSize)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	minRank := r.FormValue("min_rank")
	minEditRank := r.FormValue("min_edit_rank")

	// Validate tag selection BEFORE writing any bytes, so a bad request never leaves
	// an orphaned file on disk.
	rawTagIDs, err := parseTagIDs(r.Form["tag_ids"])
	if err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	tagIDs, _, _, ok, err := resolveSubmittedTags(rawTagIDs, effectiveUserRank(user))
	if err != nil {
		slog.Error("uploadFile: tag validation failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Invalid tag selection", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))

	allowedExtensions := map[string]bool{
		".pdf": true, ".docx": true, ".xlsx": true, ".pptx": true,
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
		".webp": true, ".txt": true, ".csv": true, ".ods": true,
	}
	if !allowedExtensions[ext] {
		http.Error(w, "file extension not allowed", http.StatusUnsupportedMediaType)
		return
	}

	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	_, _ = file.Seek(0, io.SeekStart)
	detectedMIME := strings.SplitN(http.DetectContentType(buf[:n]), ";", 2)[0]
	allowedMIMEs := map[string]bool{
		"application/pdf": true,
		"application/zip": true, // docx, xlsx, pptx, ods are all ZIP-based
		"image/jpeg":      true,
		"image/png":       true,
		"image/gif":       true,
		"image/webp":      true,
		"text/plain":      true,
	}
	if !allowedMIMEs[detectedMIME] {
		http.Error(w, "file type not allowed", http.StatusUnsupportedMediaType)
		return
	}

	fileType := "document"
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" {
		fileType = "image"
	} else if ext == ".xlsx" || ext == ".csv" || ext == ".ods" {
		fileType = "spreadsheet"
	}

	internalName := fmt.Sprintf("%d_%d%s", time.Now().Unix(), userID, ext)
	outPath := filepath.Join(getStoragePath(), internalName)

	outFile, err := os.Create(outPath)
	if err != nil {
		slog.Error("Save file error", "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer outFile.Close()
	io.Copy(outFile, file)

	// Remove the just-written file if the DB write below fails, so a failed upload
	// leaves nothing behind on disk.
	committed := false
	defer func() {
		if !committed {
			os.Remove(outPath)
		}
	}()

	tx, err := db.Begin()
	if err != nil {
		slog.Error("uploadFile: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// No column default for updated_at (see migration 060) — set it explicitly.
	res, err := tx.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		title, internalName, fileType, minRank, minEditRank, userID)
	if err != nil {
		tx.Rollback()
		slog.Error("uploadFile: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	fileID, _ := res.LastInsertId()
	for _, tid := range tagIDs {
		if _, err := tx.Exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, ?)`, fileID, tid); err != nil {
			tx.Rollback()
			slog.Error("uploadFile: tag map insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("uploadFile: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	committed = true

	logActivity(userID, user.Username, "created", "file", title, false)

	w.WriteHeader(http.StatusOK)
}

// createBlankFile generates an empty .docx/.xlsx on disk and registers it like an
// upload, so it can be opened straight into Collabora. Same permission as upload.
func createBlankFile(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var req struct {
		Title       string `json:"title"`
		Kind        string `json:"kind"` // "document" | "spreadsheet"
		MinRank     string `json:"min_rank"`
		MinEditRank string `json:"min_edit_rank"`
		TagIDs      []int  `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		http.Error(w, "Title is required", http.StatusBadRequest)
		return
	}

	data, ext, err := blankDocumentBytes(req.Kind)
	if err != nil {
		http.Error(w, "Invalid document type", http.StatusBadRequest)
		return
	}

	// Default any missing/invalid ranks rather than rejecting (same leniency as upload).
	if !isValidRank(req.MinRank) {
		req.MinRank = "R1"
	}
	if !isValidRank(req.MinEditRank) {
		req.MinEditRank = "R4"
	}

	// Validate tag selection before touching disk (single-connection rule: this reads
	// the tag tables, so it must complete before db.Begin()).
	tagIDs, _, _, ok, err := resolveSubmittedTags(req.TagIDs, effectiveUserRank(user))
	if err != nil {
		slog.Error("createBlankFile: tag validation failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "Invalid tag selection", http.StatusBadRequest)
		return
	}

	internalName := fmt.Sprintf("%d_%d%s", time.Now().Unix(), user.ID, ext)
	outPath := filepath.Join(getStoragePath(), internalName)
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		slog.Error("createBlankFile: write failed", "error", err)
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		return
	}

	committed := false
	defer func() {
		if !committed {
			os.Remove(outPath)
		}
	}()

	tx, err := db.Begin()
	if err != nil {
		slog.Error("createBlankFile: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(`INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		req.Title, internalName, req.Kind, req.MinRank, req.MinEditRank, user.ID)
	if err != nil {
		tx.Rollback()
		slog.Error("createBlankFile: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	fileID, _ := res.LastInsertId()
	for _, tid := range tagIDs {
		if _, err := tx.Exec(`INSERT INTO file_tag_map (file_id, tag_id) VALUES (?, ?)`, fileID, tid); err != nil {
			tx.Rollback()
			slog.Error("createBlankFile: tag map insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("createBlankFile: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	committed = true

	logActivity(user.ID, user.Username, "created", "file", req.Title, false)

	// Return the id + ext so the client can open the new blank file in Collabora.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":        fileID,
		"ext":       ext,
		"file_type": req.Kind,
	})
}

func updateFile(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	user := getAuthUser(r)

	// Read old values (for the manage check and the change diff) before any write.
	var oldTitle, oldMinRank, oldMinEditRank string
	var ownerID int
	err = db.QueryRow("SELECT title, min_rank, min_edit_rank, owner_user_id FROM files WHERE id = ?", fileID).
		Scan(&oldTitle, &oldMinRank, &oldMinEditRank, &ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canManage := user.IsAdmin || user.ID == ownerID
	if !canManage && user.Rank != "" {
		perms := getRankPermissions(user.Rank)
		canManage = perms.ManageFiles
	}

	if !canManage {
		http.Error(w, "Forbidden: You do not have permission to edit this file.", http.StatusForbidden)
		return
	}

	var req struct {
		Title       string `json:"title"`
		MinRank     string `json:"min_rank"`
		MinEditRank string `json:"min_edit_rank"`
		TagIDs      *[]int `json:"tag_ids"` // nil = leave tags unchanged
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate + resolve tags (only when the client submitted a tag set). All work
	// that touches the DB happens BEFORE db.Begin() — single-connection rule.
	userRank := effectiveUserRank(user)
	var submitted, visibleIDs []int
	var tagByID map[int]FileTag
	var oldTagNames []string
	if req.TagIDs != nil {
		var ok bool
		submitted, visibleIDs, tagByID, ok, err = resolveSubmittedTags(*req.TagIDs, userRank)
		if err != nil {
			slog.Error("updateFile: tag validation failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "Invalid tag selection", http.StatusBadRequest)
			return
		}
		// Old visible tag names, for the diff.
		currentIDs, err := fileTagIDs(fileID)
		if err != nil {
			slog.Error("updateFile: load current tags failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		visibleSet := make(map[int]bool, len(visibleIDs))
		for _, id := range visibleIDs {
			visibleSet[id] = true
		}
		for _, id := range currentIDs {
			if visibleSet[id] {
				oldTagNames = append(oldTagNames, tagByID[id].Name)
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("updateFile: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("UPDATE files SET title = ?, min_rank = ?, min_edit_rank = ?, updated_at = CURRENT_TIMESTAMP, updated_by = ? WHERE id = ?",
		req.Title, req.MinRank, req.MinEditRank, user.ID, fileID); err != nil {
		tx.Rollback()
		slog.Error("updateFile: update failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if req.TagIDs != nil {
		if err := mergeFileTagsTx(tx, fileID, submitted, visibleIDs); err != nil {
			tx.Rollback()
			slog.Error("updateFile: tag merge failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("updateFile: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Build a field-level change summary for the activity log.
	var changes []string
	if req.Title != oldTitle {
		changes = append(changes, "title: "+oldTitle+" → "+req.Title)
	}
	if req.MinRank != oldMinRank {
		changes = append(changes, "view rank: "+oldMinRank+" → "+req.MinRank)
	}
	if req.MinEditRank != oldMinEditRank {
		changes = append(changes, "edit rank: "+oldMinEditRank+" → "+req.MinEditRank)
	}
	if req.TagIDs != nil {
		var newTagNames []string
		for _, id := range submitted {
			newTagNames = append(newTagNames, tagByID[id].Name)
		}
		sort.Strings(oldTagNames)
		sort.Strings(newTagNames)
		if strings.Join(oldTagNames, ",") != strings.Join(newTagNames, ",") {
			changes = append(changes, "tags: ["+strings.Join(oldTagNames, ", ")+"] → ["+strings.Join(newTagNames, ", ")+"]")
		}
	}
	logActivity(user.ID, user.Username, "updated", "file", req.Title, false, strings.Join(changes, "; "))

	w.WriteHeader(http.StatusOK)
}

func deleteFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	user := getAuthUser(r)

	var fileName, fileTitle string
	var ownerID int
	err := db.QueryRow("SELECT file_name, title, owner_user_id FROM files WHERE id = ?", fileID).Scan(&fileName, &fileTitle, &ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canManage := user.IsAdmin || user.ID == ownerID
	if !canManage && user.Rank != "" {
		perms := getRankPermissions(user.Rank)
		canManage = perms.ManageFiles
	}

	if !canManage {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// FKs aren't enforced, so ON DELETE CASCADE won't fire — remove the tag map rows
	// explicitly, in one transaction with the file delete.
	tx, err := db.Begin()
	if err != nil {
		slog.Error("deleteFile: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM file_tag_map WHERE file_id = ?", fileID); err != nil {
		tx.Rollback()
		slog.Error("deleteFile: tag map delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec("DELETE FROM files WHERE id = ?", fileID); err != nil {
		tx.Rollback()
		slog.Error("deleteFile: file delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("deleteFile: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	os.Remove(filepath.Join(getStoragePath(), fileName))

	logActivity(user.ID, user.Username, "deleted", "file", fileTitle, false)

	w.WriteHeader(http.StatusOK)
}

func downloadFile(w http.ResponseWriter, r *http.Request) {
	fileID, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	user := getAuthUser(r)

	var fileName, minRank string
	var ownerID int
	err = db.QueryRow("SELECT file_name, min_rank, owner_user_id FROM files WHERE id = ?", fileID).Scan(&fileName, &minRank, &ownerID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if !user.IsAdmin && ownerID != user.ID {
		// Effective view rank includes the min_rank of every tag on the file — a
		// tag can hide a file whose own min_rank would otherwise permit it.
		tagRanks, err := fileTagMinRanks(fileID)
		if err != nil {
			slog.Error("downloadFile: tag ranks failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if rankTier(effectiveUserRank(user)) < effectiveMinRankValue(minRank, tagRanks) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	http.ServeFile(w, r, filepath.Join(getStoragePath(), fileName))
}

func generateWOPIToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID, _ := strconv.Atoi(vars["id"])

	user := getAuthUser(r)

	var minRank, minEditRank string
	var ownerID int
	err := db.QueryRow("SELECT min_rank, min_edit_rank, owner_user_id FROM files WHERE id = ?", fileID).Scan(&minRank, &minEditRank, &ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canView := false
	canEdit := false

	if user.IsAdmin || user.ID == ownerID {
		canView = true
		canEdit = true
	} else if user.Rank != "" {
		// Effective view rank includes every tag's min_rank; edit rank is unchanged.
		tagRanks, err := fileTagMinRanks(fileID)
		if err != nil {
			slog.Error("generateWOPIToken: tag ranks failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		canView = rankTier(user.Rank) >= effectiveMinRankValue(minRank, tagRanks)
		canEdit = hasSufficientRank(user.Rank, minEditRank)
	}

	if !canView {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	claims := WOPIClaims{
		UserID:   user.ID,
		Username: user.Username,
		FileID:   fileID,
		CanEdit:  canEdit,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	secretKey := os.Getenv("SESSION_KEY")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte(secretKey))

	collaboraDomain := os.Getenv("COLLABORA_DOMAIN")
	if port := os.Getenv("COLLABORA_PORT"); port != "" {
		// Serve Collabora at the SAME host the browser reached the app at, with this
		// fixed port. Lets one dev stack work at both localhost:PORT (the Windows
		// machine, where the LAN IP is firewalled) and <LAN-IP>:PORT (a phone on the
		// network) with no per-client config. Takes precedence over COLLABORA_DOMAIN.
		collaboraDomain = strings.Split(r.Host, ":")[0] + ":" + port
	} else if collaboraDomain == "" {
		collaboraDomain = "collabora." + strings.Split(r.Host, ":")[0]
	}

	// --- DOCKER NETWORKING FIX ---
	// Generate the internal WOPISrc so Collabora talks directly to Go
	// over the private Docker bridge network, bypassing the firewall entirely.
	internalHost := "alliance-manager:8080"
	wopiSrc := fmt.Sprintf("http://%s/wopi/files/%d", internalHost, fileID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token":            tokenString,
		"collabora_domain": collaboraDomain,
		"wopi_src":         wopiSrc, // Passing the internal route down to the frontend
	})
}

func wopiCheckFileInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	claims := r.Context().Value("wopi_claims").(*WOPIClaims)

	var title, fileName string
	var ownerID int
	err := db.QueryRow("SELECT title, file_name, owner_user_id FROM files WHERE id = ?", vars["id"]).Scan(&title, &fileName, &ownerID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	fileInfo, err := os.Stat(filepath.Join(getStoragePath(), fileName))
	if err != nil {
		http.Error(w, "File missing", http.StatusNotFound)
		return
	}

	// BaseFileName must include the file extension so Collabora picks the
	// correct application (e.g. Calc for .csv/.xlsx, Writer for .docx).
	baseFileName := title + filepath.Ext(fileName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"BaseFileName":     baseFileName,
		"OwnerId":          fmt.Sprintf("%d", ownerID),
		"Size":             fileInfo.Size(),
		"UserId":           fmt.Sprintf("%d", claims.UserID),
		"UserFriendlyName": claims.Username,
		"UserCanWrite":     claims.CanEdit,
		"SupportsUpdate":   true,
		"SupportsLocks":    true,
	})
}

func wopiGetFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var fileName string

	// 1. Verify the file actually exists in the database first
	err := db.QueryRow("SELECT file_name FROM files WHERE id = ?", vars["id"]).Scan(&fileName)
	if err != nil {
		http.Error(w, "Database record not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(getStoragePath(), fileName)

	// 2. Verify the physical file hasn't been deleted from the hard drive
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		slog.Error("WOPI error: requested file does not exist on disk", "path", filePath)
		http.Error(w, "Document not found on disk", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, filePath)
}

func wopiPutFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	claims := r.Context().Value("wopi_claims").(*WOPIClaims)

	if !claims.CanEdit {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var fileName, title string
	var prevUpdated sql.NullString
	// 1. Verify database record exists before trying to save. Read updated_at now
	//    (before the overwrite) so the log throttle sees the PRIOR modification time.
	err := db.QueryRow("SELECT file_name, title, updated_at FROM files WHERE id = ?", vars["id"]).Scan(&fileName, &title, &prevUpdated)
	if err != nil {
		http.Error(w, "Database record not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(getStoragePath(), fileName)

	// 2. Verify physical file exists before overwriting it
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		slog.Error("WOPI put error: attempted to save to missing file", "path", filePath)
		http.Error(w, "Target document missing from disk", http.StatusNotFound)
		return
	}

	fileData, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	os.WriteFile(filePath, fileData, 0644)

	// Tier 1 edit tracking: bump updated_at + updated_by on every save; write an activity
	// row only once per FileEditLogWindow so Collabora's frequent autosaves don't flood.
	db.Exec("UPDATE files SET updated_at = CURRENT_TIMESTAMP, updated_by = ? WHERE id = ?", claims.UserID, vars["id"])
	if shouldLogFileEdit(prevUpdated) {
		logActivity(claims.UserID, claims.Username, "updated", "file", title, false)
	}

	// WOPI spec requires a JSON body with LastModifiedTime on success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"LastModifiedTime": time.Now().UTC().Format(time.RFC3339),
	})
}

func wopiActionHandler(w http.ResponseWriter, r *http.Request) {
	override := r.Header.Get("X-WOPI-Override")
	lockToken := r.Header.Get("X-WOPI-Lock")

	if override == "LOCK" || override == "UNLOCK" || override == "REFRESH_LOCK" {
		w.Header().Set("X-WOPI-Lock", lockToken)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// end of handlers_files.go
