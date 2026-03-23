// handlers_files.go - Handles file management and WOPI integration for LastWar alliance files.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	ranks := map[string]int{"R1": 1, "R2": 2, "R3": 3, "R4": 4, "R5": 5, "Admin": 6}
	userVal, ok1 := ranks[userRank]
	reqVal, ok2 := ranks[requiredRank]
	if !ok1 || !ok2 {
		return false
	}
	return userVal >= reqVal
}

func getFilesList(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)
	var userRank string

	if isAdmin {
		userRank = "Admin"
	} else if memberID, ok := session.Values["member_id"].(int); ok {
		db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&userRank)
	} else {
		userRank = "R1"
	}

	rows, err := db.Query(`
		SELECT f.id, f.title, f.file_name, f.file_type, f.min_rank, f.min_edit_rank, f.created_at, u.username as owner_name, f.owner_user_id 
		FROM files f 
		JOIN users u ON f.owner_user_id = u.id 
		ORDER BY f.created_at DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var files []AllianceFile
	for rows.Next() {
		var f AllianceFile
		rows.Scan(&f.ID, &f.Title, &f.FileName, &f.FileType, &f.MinRank, &f.MinEditRank, &f.CreatedAt, &f.OwnerName, &f.OwnerUserID)

		f.IsOwner = (f.OwnerUserID == userID)

		if isAdmin || f.IsOwner || hasSufficientRank(userRank, f.MinRank) {
			files = append(files, f)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func uploadFile(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	r.Body = http.MaxBytesReader(w, r.Body, MaxFileUploadSize)
	err := r.ParseMultipartForm(MaxFileUploadSize)
	if err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	minRank := r.FormValue("min_rank")
	minEditRank := r.FormValue("min_edit_rank")
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
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
		log.Printf("Save File Error: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}
	defer outFile.Close()
	io.Copy(outFile, file)

	_, err = db.Exec("INSERT INTO files (title, file_name, file_type, min_rank, min_edit_rank, owner_user_id) VALUES (?, ?, ?, ?, ?, ?)",
		title, internalName, fileType, minRank, minEditRank, userID)
	if err != nil {
		log.Printf("Database Insert Error: %v", err)
		http.Error(w, fmt.Sprintf("DB error: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func updateFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	var ownerID int
	err := db.QueryRow("SELECT owner_user_id FROM files WHERE id = ?", fileID).Scan(&ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canManage := isAdmin || userID == ownerID
	if !canManage {
		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			perms := getRankPermissions(rank)
			canManage = perms.ManageFiles
		}
	}

	if !canManage {
		http.Error(w, "Forbidden: You do not have permission to edit this file.", http.StatusForbidden)
		return
	}

	var req struct {
		Title       string `json:"title"`
		MinRank     string `json:"min_rank"`
		MinEditRank string `json:"min_edit_rank"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	_, err = db.Exec("UPDATE files SET title = ?, min_rank = ?, min_edit_rank = ? WHERE id = ?",
		req.Title, req.MinRank, req.MinEditRank, fileID)
	if err != nil {
		log.Printf("Database Update Error: %v", err)
		http.Error(w, "Failed to update database", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID := vars["id"]

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	var fileName string
	var ownerID int
	err := db.QueryRow("SELECT file_name, owner_user_id FROM files WHERE id = ?", fileID).Scan(&fileName, &ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canManage := isAdmin || userID == ownerID
	if !canManage {
		if memberID, ok := session.Values["member_id"].(int); ok {
			var rank string
			db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&rank)
			perms := getRankPermissions(rank)
			canManage = perms.ManageFiles
		}
	}

	if !canManage {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	os.Remove(filepath.Join(getStoragePath(), fileName))
	db.Exec("DELETE FROM files WHERE id = ?", fileID)
	w.WriteHeader(http.StatusOK)
}

func downloadFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	isAdmin, _ := session.Values["is_admin"].(bool)

	var fileName, minRank string
	var ownerID int
	err := db.QueryRow("SELECT file_name, min_rank, owner_user_id FROM files WHERE id = ?", vars["id"]).Scan(&fileName, &minRank, &ownerID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	if !isAdmin && ownerID != userID {
		var userRank string
		if memberID, ok := session.Values["member_id"].(int); ok {
			db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&userRank)
		} else {
			userRank = "R1"
		}
		if !hasSufficientRank(userRank, minRank) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	http.ServeFile(w, r, filepath.Join(getStoragePath(), fileName))
}

func generateWOPIToken(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	fileID, _ := strconv.Atoi(vars["id"])

	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)
	username, _ := session.Values["username"].(string)
	isAdmin, _ := session.Values["is_admin"].(bool)

	var minRank, minEditRank string
	var ownerID int
	err := db.QueryRow("SELECT min_rank, min_edit_rank, owner_user_id FROM files WHERE id = ?", fileID).Scan(&minRank, &minEditRank, &ownerID)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	canView := false
	canEdit := false

	if isAdmin || userID == ownerID {
		canView = true
		canEdit = true
	} else if memberID, ok := session.Values["member_id"].(int); ok {
		var userRank string
		db.QueryRow("SELECT rank FROM members WHERE id = ?", memberID).Scan(&userRank)
		canView = hasSufficientRank(userRank, minRank)
		canEdit = hasSufficientRank(userRank, minEditRank)
	}

	if !canView {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	claims := WOPIClaims{
		UserID:   userID,
		Username: username,
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
	if collaboraDomain == "" {
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
		log.Printf("WOPI Error: Requested file does not exist on disk: %s", filePath)
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

	var fileName string
	// 1. Verify database record exists before trying to save
	err := db.QueryRow("SELECT file_name FROM files WHERE id = ?", vars["id"]).Scan(&fileName)
	if err != nil {
		http.Error(w, "Database record not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(getStoragePath(), fileName)

	// 2. Verify physical file exists before overwriting it
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("WOPI Put Error: Attempted to save to missing file: %s", filePath)
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
