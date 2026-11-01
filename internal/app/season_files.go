// season_files.go — season tags on Comms and Files, and an archived season's file copies
// (private-docs 272).
//
// A file, a Comms resource, a poll or a mail template may name a Season Hub season. While the
// season is open the Hub lists the live records; when it archives, archiveSeason copies every
// tagged file's bytes to <storage>/season-<id>/ and records each copy with the view rank it had
// then — the file's own rank with every tag's folded in, frozen because tags can change later
// and the copy cannot. The live file stays editable on the Files page.

package app

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
)

// parseOptionalSeasonID reads a form value naming a season; "" means none.
func parseOptionalSeasonID(v string) (*int, error) {
	if v == "" {
		return nil, nil
	}
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("invalid season id %q", v)
	}
	return &id, nil
}

func sameSeason(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// seasonTagName is "S4" (or "none") for an activity-log diff.
func seasonTagName(id *int) string {
	if id == nil {
		return "none"
	}
	var n int
	if db.QueryRow(`SELECT season_number FROM seasons WHERE id = ?`, *id).Scan(&n) != nil {
		return "#" + strconv.Itoa(*id)
	}
	return "S" + strconv.Itoa(n)
}

// seasonTagCheck refuses moving a record into or out of an archived season, or naming one
// that does not exist. A zero status means the change may go ahead.
func seasonTagCheck(oldID, newID *int) (int, string) {
	for _, id := range []*int{oldID, newID} {
		if id == nil {
			continue
		}
		var archived sql.NullString
		err := db.QueryRow(`SELECT archived_at FROM seasons WHERE id = ?`, *id).Scan(&archived)
		if err == sql.ErrNoRows {
			if id == newID {
				return http.StatusBadRequest, "Season not found"
			}
			continue // a stale old tag never blocks clearing it
		}
		if err != nil {
			slog.Error("seasonTagCheck", "error", err)
			return http.StatusInternalServerError, "Database error"
		}
		if archived.Valid && archived.String != "" {
			return http.StatusConflict, "Season is archived: nothing can be tagged to it or moved out of it"
		}
	}
	return 0, ""
}

// rankAtTier is the rank string whose tier is the highest of the file's and its tags'.
func rankAtTier(fileMinRank string, tagMinRanks []string) string {
	best, bestTier := fileMinRank, rankTier(fileMinRank)
	for _, r := range tagMinRanks {
		if t := rankTier(r); t > bestTier {
			best, bestTier = r, t
		}
	}
	return best
}

// listSeasonFilesToCopy reads the files tagged to a season and the effective view rank of
// each, for the archive's copies.
func listSeasonFilesToCopy(seasonID int) ([]seasonFileCopy, error) {
	rows, err := db.Query(`SELECT id, title, file_name, file_type, min_rank, owner_user_id FROM files
		WHERE season_id = ? ORDER BY id`, seasonID)
	if err != nil {
		return nil, err
	}
	var out []seasonFileCopy
	var ranks []string
	for rows.Next() {
		var f seasonFileCopy
		var minRank string
		if err := rows.Scan(&f.FileID, &f.Title, &f.FileName, &f.FileType, &minRank, &f.OwnerUserID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, f)
		ranks = append(ranks, minRank)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		tagRanks, err := fileTagMinRanks(out[i].FileID)
		if err != nil {
			return nil, err
		}
		out[i].EffectiveMinRank = rankAtTier(ranks[i], tagRanks)
	}
	return out, nil
}

// copySeasonFileBytes copies one file into dir and checks the copy. A Collabora save can
// rewrite the live file at any moment, so the source is stat'ed before and after: a change in
// between means the copy may be torn, and it is made once more; a second change fails the
// archive rather than freezing bytes nobody ever saved.
func copySeasonFileBytes(dir string, f seasonFileCopy) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	src := filepath.Join(getStoragePath(), f.FileName)
	dst := filepath.Join(dir, f.FileName)
	for attempt := 0; attempt < 2; attempt++ {
		before, err := os.Stat(src)
		if err != nil {
			return err
		}
		if err := copyFileBytes(src, dst); err != nil {
			return err
		}
		after, err := os.Stat(src)
		if err != nil {
			return err
		}
		copied, err := os.Stat(dst)
		if err != nil {
			return err
		}
		if before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && copied.Size() == after.Size() {
			return nil
		}
	}
	os.Remove(dst)
	return fmt.Errorf("the file kept changing while it was copied; try archiving again")
}

func copyFileBytes(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// seasonHubFile is one row of the Hub's Files tab: a live file, or an archived season's copy.
type seasonHubFile struct {
	ID          int       `json:"id"`
	SnapshotID  int       `json:"snapshot_id,omitempty"`
	Title       string    `json:"title"`
	FileName    string    `json:"file_name"`
	FileType    string    `json:"file_type"`
	OwnerName   string    `json:"owner_name"`
	UpdatedAt   string    `json:"updated_at"`
	Frozen      bool      `json:"frozen"`
	SnapshotAt  string    `json:"snapshot_at,omitempty"`
	Tags        []FileTag `json:"tags"`
	MinEditRank string    `json:"min_edit_rank,omitempty"`
	IsOwner     bool      `json:"is_owner"`
}

// GET /api/season-hub/files?season_id= — the season's files: the live ones (through the Files
// page's own lister and rank rule) while it is open, the frozen copies once it is archived.
func handleSeasonHubFiles(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if sid == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	var archived sql.NullString
	if err := db.QueryRow(`SELECT archived_at FROM seasons WHERE id = ?`, sid).Scan(&archived); err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonHubFiles: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	out := []seasonHubFile{}
	if !archived.Valid || archived.String == "" {
		files, err := listFilesForUser(user, &sid)
		if err != nil {
			slog.Error("handleSeasonHubFiles: list", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		for _, f := range files {
			out = append(out, seasonHubFile{ID: f.ID, Title: f.Title, FileName: f.FileName, FileType: f.FileType,
				OwnerName: f.OwnerName, UpdatedAt: f.UpdatedAt, Tags: f.Tags, MinEditRank: f.MinEditRank, IsOwner: f.IsOwner})
		}
		writeJSON(w, map[string]any{"archived": false, "files": out})
		return
	}

	rows, err := db.Query(`SELECT s.id, s.file_id, s.title, s.file_name, s.file_type, s.effective_min_rank,
		COALESCE(s.owner_user_id, 0), COALESCE(u.username, ''), strftime('%Y-%m-%d %H:%M:%S', s.snapshot_at)
		FROM season_file_snapshots s LEFT JOIN users u ON u.id = s.owner_user_id
		WHERE s.season_id = ? ORDER BY s.title COLLATE NOCASE`, sid)
	if err != nil {
		slog.Error("handleSeasonHubFiles: snapshots", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	userTier := rankTier(effectiveUserRank(user))
	for rows.Next() {
		var f seasonHubFile
		var rank string
		var owner int
		if err := rows.Scan(&f.SnapshotID, &f.ID, &f.Title, &f.FileName, &f.FileType, &rank, &owner, &f.OwnerName, &f.SnapshotAt); err != nil {
			slog.Error("handleSeasonHubFiles: scan", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		// The frozen view rank, with the Files page's owner and admin exception.
		if !user.IsAdmin && owner != user.ID && userTier < rankTier(rank) {
			continue
		}
		f.Frozen, f.Tags, f.IsOwner = true, []FileTag{}, owner == user.ID
		out = append(out, f)
	}
	writeJSON(w, map[string]any{"archived": true, "files": out})
}

// GET /api/season-hub/files/{id}/download — an archived season's copy, under the rank it had
// when the season archived (the owner and admins always).
func handleSeasonFileSnapshotDownload(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var seasonID, owner int
	var fileName, rank, title string
	if err := db.QueryRow(`SELECT season_id, file_name, effective_min_rank, COALESCE(owner_user_id, 0), title
		FROM season_file_snapshots WHERE id = ?`, id).Scan(&seasonID, &fileName, &rank, &owner, &title); err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !user.IsAdmin && owner != user.ID && rankTier(effectiveUserRank(user)) < rankTier(rank) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", title+filepath.Ext(fileName)))
	http.ServeFile(w, r, filepath.Join(seasonArchiveDir(seasonID), filepath.Base(fileName)))
}
