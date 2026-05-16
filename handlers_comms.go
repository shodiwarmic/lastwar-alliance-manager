package main

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// Page handler
// ---------------------------------------------------------------------------

func handleCommsPage(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Alliance Communications", "comms")
	renderTemplate(w, r, "comms.html", data)
}

// ---------------------------------------------------------------------------
// Templates — list / by-slug / create / update / delete
// ---------------------------------------------------------------------------

func handleCommsTemplateList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typeFilter := q.Get("type")
	seasonIDStr := q.Get("season_id")

	query := `
		SELECT ct.id, ct.type, ct.title, ct.category, ct.content,
		       ct.season_id, ct.slug, ct.required_vars,
		       COALESCE(u.username,'(deleted)'), ct.created_at, ct.updated_at
		FROM comms_templates ct
		LEFT JOIN users u ON u.id = ct.created_by
		WHERE 1=1`
	args := []any{}

	if typeFilter != "" {
		query += " AND ct.type = ?"
		args = append(args, typeFilter)
	}
	if seasonIDStr != "" {
		sid, err := strconv.Atoi(seasonIDStr)
		if err != nil {
			http.Error(w, "Invalid season_id", http.StatusBadRequest)
			return
		}
		query += " AND ct.season_id = ?"
		args = append(args, sid)
	}
	query += " ORDER BY ct.category ASC, ct.created_at ASC"

	rows, err := db.Query(query, args...)
	if err != nil {
		slog.Error("handleCommsTemplateList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []CommsTemplate{}
	for rows.Next() {
		var t CommsTemplate
		if err := rows.Scan(&t.ID, &t.Type, &t.Title, &t.Category, &t.Content,
			&t.SeasonID, &t.Slug, &t.RequiredVars,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		items = append(items, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func handleCommsTemplateBySlug(w http.ResponseWriter, r *http.Request) {
	slug := mux.Vars(r)["slug"]

	var t CommsTemplate
	err := db.QueryRow(`
		SELECT ct.id, ct.type, ct.title, ct.category, ct.content,
		       ct.season_id, ct.slug, ct.required_vars,
		       COALESCE(u.username,'(deleted)'), ct.created_at, ct.updated_at
		FROM comms_templates ct
		LEFT JOIN users u ON u.id = ct.created_by
		WHERE ct.slug = ?`, slug).
		Scan(&t.ID, &t.Type, &t.Title, &t.Category, &t.Content,
			&t.SeasonID, &t.Slug, &t.RequiredVars,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleCommsTemplateBySlug: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func handleCommsTemplateCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		Type         string `json:"type"`
		Title        string `json:"title"`
		Category     string `json:"category"`
		Content      string `json:"content"`
		SeasonID     *int   `json:"season_id"`
		RequiredVars string `json:"required_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Category = strings.TrimSpace(body.Category)
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if body.Type != "mail" && body.Type != "announcement" {
		http.Error(w, "type must be mail or announcement", http.StatusBadRequest)
		return
	}
	if body.Category == "" {
		body.Category = "General"
	}
	if body.RequiredVars == "" {
		body.RequiredVars = "[]"
	}
	// Validate required_vars is a JSON array
	var rvCheck []string
	if err := json.Unmarshal([]byte(body.RequiredVars), &rvCheck); err != nil {
		http.Error(w, "required_vars must be a JSON array", http.StatusBadRequest)
		return
	}

	if body.SeasonID != nil {
		s, err := loadSeasonByID(*body.SeasonID)
		if err == sql.ErrNoRows {
			http.Error(w, "Season not found", http.StatusNotFound)
			return
		}
		if err != nil {
			slog.Error("handleCommsTemplateCreate: load season", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if s.ArchivedAt != "" {
			http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
			return
		}
	}

	res, err := db.Exec(
		`INSERT INTO comms_templates (type, title, category, content, season_id, required_vars, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		body.Type, body.Title, body.Category, body.Content, body.SeasonID, body.RequiredVars, user.ID,
	)
	if err != nil {
		slog.Error("handleCommsTemplateCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(user.ID, user.Username, "created", "comms_template", body.Title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleCommsTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Title        string `json:"title"`
		Category     string `json:"category"`
		Content      string `json:"content"`
		RequiredVars string `json:"required_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if body.RequiredVars == "" {
		body.RequiredVars = "[]"
	}
	var rvCheck []string
	if err := json.Unmarshal([]byte(body.RequiredVars), &rvCheck); err != nil {
		http.Error(w, "required_vars must be a JSON array", http.StatusBadRequest)
		return
	}

	// Fetch old record for diff and archived check
	var old CommsTemplate
	err = db.QueryRow(
		`SELECT title, category, content, required_vars, season_id FROM comms_templates WHERE id = ?`, id).
		Scan(&old.Title, &old.Category, &old.Content, &old.RequiredVars, &old.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleCommsTemplateUpdate: fetch old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if old.SeasonID != nil {
		s, _ := loadSeasonByID(*old.SeasonID)
		if s != nil && s.ArchivedAt != "" {
			http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
			return
		}
	}

	if _, err := db.Exec(
		`UPDATE comms_templates SET title=?, category=?, content=?, required_vars=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		body.Title, body.Category, body.Content, body.RequiredVars, id,
	); err != nil {
		slog.Error("handleCommsTemplateUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Title != body.Title {
		changes = append(changes, "title: "+old.Title+" → "+body.Title)
	}
	if old.Category != body.Category {
		changes = append(changes, "category: "+old.Category+" → "+body.Category)
	}
	if old.Content != body.Content {
		changes = append(changes, "content updated")
	}
	if old.RequiredVars != body.RequiredVars {
		changes = append(changes, "required_vars: "+old.RequiredVars+" → "+body.RequiredVars)
	}
	logActivity(user.ID, user.Username, "updated", "comms_template", body.Title, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleCommsTemplateDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var title string
	var seasonID *int
	err = db.QueryRow(`SELECT title, season_id FROM comms_templates WHERE id = ?`, id).
		Scan(&title, &seasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleCommsTemplateDelete: fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if seasonID != nil {
		s, _ := loadSeasonByID(*seasonID)
		if s != nil && s.ArchivedAt != "" {
			http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
			return
		}
	}

	if _, err := db.Exec(`DELETE FROM comms_templates WHERE id = ?`, id); err != nil {
		slog.Error("handleCommsTemplateDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "deleted", "comms_template", title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

// ---------------------------------------------------------------------------
// Resources — list / create / update / delete
// ---------------------------------------------------------------------------

func handleCommsResourceList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT cr.id, cr.title, cr.url, cr.description,
		       COALESCE(u.username,'(deleted)'), cr.created_at
		FROM comms_resources cr
		LEFT JOIN users u ON u.id = cr.created_by
		ORDER BY cr.created_at ASC`)
	if err != nil {
		slog.Error("handleCommsResourceList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []CommsResource{}
	for rows.Next() {
		var res CommsResource
		if err := rows.Scan(&res.ID, &res.Title, &res.URL, &res.Description,
			&res.CreatedBy, &res.CreatedAt); err != nil {
			continue
		}
		items = append(items, res)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func handleCommsResourceCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.URL = strings.TrimSpace(body.URL)
	if body.Title == "" || body.URL == "" {
		http.Error(w, "title and url are required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") {
		http.Error(w, "url must start with http:// or https://", http.StatusBadRequest)
		return
	}

	res, err := db.Exec(
		`INSERT INTO comms_resources (title, url, description, created_by) VALUES (?, ?, ?, ?)`,
		body.Title, body.URL, body.Description, user.ID,
	)
	if err != nil {
		slog.Error("handleCommsResourceCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	logActivity(user.ID, user.Username, "created", "comms_resource", body.Title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handleCommsResourceUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.URL = strings.TrimSpace(body.URL)
	if body.Title == "" || body.URL == "" {
		http.Error(w, "title and url are required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(body.URL, "http://") && !strings.HasPrefix(body.URL, "https://") {
		http.Error(w, "url must start with http:// or https://", http.StatusBadRequest)
		return
	}

	var old CommsResource
	err = db.QueryRow(`SELECT title, url, description FROM comms_resources WHERE id = ?`, id).
		Scan(&old.Title, &old.URL, &old.Description)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleCommsResourceUpdate: fetch old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(
		`UPDATE comms_resources SET title=?, url=?, description=? WHERE id=?`,
		body.Title, body.URL, body.Description, id,
	); err != nil {
		slog.Error("handleCommsResourceUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Title != body.Title {
		changes = append(changes, "title: "+old.Title+" → "+body.Title)
	}
	if old.URL != body.URL {
		changes = append(changes, "url: "+old.URL+" → "+body.URL)
	}
	if old.Description != body.Description {
		changes = append(changes, "description updated")
	}
	logActivity(user.ID, user.Username, "updated", "comms_resource", body.Title, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handleCommsResourceDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var title string
	err = db.QueryRow(`SELECT title FROM comms_resources WHERE id = ?`, id).Scan(&title)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handleCommsResourceDelete: fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(`DELETE FROM comms_resources WHERE id = ?`, id); err != nil {
		slog.Error("handleCommsResourceDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "deleted", "comms_resource", title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}
