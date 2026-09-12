package app

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"
)

// Bounds on a responsibility's task list. Arbitrary but bounded, and enforced
// here rather than as a schema constraint: the point is a written error the
// officer can act on, not a CHECK that fails as a 500. Counted in runes, not
// bytes -- accented and non-Latin names are in scope for this app, and a byte
// count would reject multilingual text well before 300 characters.
const (
	maxOCTasks       = 25
	maxOCTaskRunes   = 300
	ocTaskLimitError = "A responsibility can have at most 25 tasks, each at most 300 characters."
)

// normaliseTasks trims each line, drops the blanks, and enforces the limits. The
// textarea in the responsibility modal is "one per line", so blank lines are how
// a user separates things rather than an attempt to store an empty task.
func normaliseTasks(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > maxOCTaskRunes {
			return nil, false
		}
		out = append(out, t)
	}
	return out, len(out) <= maxOCTasks
}

// replaceOCTasks rewrites a responsibility's whole task list inside the caller's
// transaction. Delete-then-insert, because the list has no stable identity to
// diff against -- line order is the only order there is.
func replaceOCTasks(tx *sql.Tx, respID int, tasks []string) error {
	if _, err := tx.Exec(`DELETE FROM oc_tasks WHERE responsibility_id = ?`, respID); err != nil {
		return err
	}
	for i, t := range tasks {
		if _, err := tx.Exec(
			`INSERT INTO oc_tasks (responsibility_id, text, display_order) VALUES (?, ?, ?)`,
			respID, t, i,
		); err != nil {
			return err
		}
	}
	return nil
}

// readOCTasks loads one responsibility's tasks. Used by the update handler to
// decide whether the list actually changed, before the transaction opens.
func readOCTasks(respID int) ([]string, error) {
	rows, err := db.Query(
		`SELECT text FROM oc_tasks WHERE responsibility_id = ? ORDER BY display_order, id`, respID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return tasks, nil
}

func sameTasks(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// GET /api/officer-command/data
//
// Four flat reads stitched in memory, deliberately not wrapped in a transaction:
// db.SetMaxOpenConns(1) means a read transaction would hold the only connection
// across all four statements, which is the exact shape the cursor-deadlock rule
// exists to stop. The cost is that a responsibility committed between two of the
// reads can come back with an empty task list for one request -- the correct
// answer at that instant. Each cursor is closed explicitly before the next
// statement is issued, rather than relying on Rows.Next closing it by exhaustion.
func getOfficerCommandData(w http.ResponseWriter, r *http.Request) {
	dbErr := func(what string, err error) {
		slog.Error("officer command: "+what, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
	}

	catRows, err := db.Query(`SELECT id, name, display_order FROM oc_categories ORDER BY display_order, id`)
	if err != nil {
		dbErr("load categories", err)
		return
	}
	defer catRows.Close()

	var categories []OCCategory
	catMap := map[int]int{} // category ID -> index in categories slice
	for catRows.Next() {
		var c OCCategory
		if err := catRows.Scan(&c.ID, &c.Name, &c.DisplayOrder); err != nil {
			dbErr("scan category", err)
			return
		}
		c.Responsibilities = []OCResponsibility{}
		catMap[c.ID] = len(categories)
		categories = append(categories, c)
	}
	if err := catRows.Err(); err != nil {
		dbErr("iterate categories", err)
		return
	}
	catRows.Close()

	respRows, err := db.Query(`SELECT id, category_id, name, description, frequency, display_order FROM oc_responsibilities ORDER BY display_order, id`)
	if err != nil {
		dbErr("load responsibilities", err)
		return
	}
	defer respRows.Close()

	type respLocation struct{ catIdx, respIdx int }
	respMap := map[int]respLocation{}
	for respRows.Next() {
		var rp OCResponsibility
		if err := respRows.Scan(&rp.ID, &rp.CategoryID, &rp.Name, &rp.Description, &rp.Frequency, &rp.DisplayOrder); err != nil {
			dbErr("scan responsibility", err)
			return
		}
		rp.Assignees = []OCAssignee{}
		rp.Tasks = []string{} // so the JSON is [], never null
		catIdx, ok := catMap[rp.CategoryID]
		if !ok {
			continue
		}
		respIdx := len(categories[catIdx].Responsibilities)
		categories[catIdx].Responsibilities = append(categories[catIdx].Responsibilities, rp)
		respMap[rp.ID] = respLocation{catIdx, respIdx}
	}
	if err := respRows.Err(); err != nil {
		dbErr("iterate responsibilities", err)
		return
	}
	respRows.Close()

	asgRows, err := db.Query(`
		SELECT oa.responsibility_id, m.id, m.name, m.rank
		FROM oc_assignees oa
		JOIN members m ON m.id = oa.member_id
		ORDER BY m.rank DESC, m.name ASC
	`)
	if err != nil {
		dbErr("load assignees", err)
		return
	}
	defer asgRows.Close()

	for asgRows.Next() {
		var respID int
		var a OCAssignee
		if err := asgRows.Scan(&respID, &a.MemberID, &a.Name, &a.Rank); err != nil {
			dbErr("scan assignee", err)
			return
		}
		if loc, ok := respMap[respID]; ok {
			categories[loc.catIdx].Responsibilities[loc.respIdx].Assignees = append(
				categories[loc.catIdx].Responsibilities[loc.respIdx].Assignees, a,
			)
		}
	}
	if err := asgRows.Err(); err != nil {
		dbErr("iterate assignees", err)
		return
	}
	asgRows.Close()

	taskRows, err := db.Query(`SELECT responsibility_id, text FROM oc_tasks ORDER BY responsibility_id, display_order, id`)
	if err != nil {
		dbErr("load tasks", err)
		return
	}
	defer taskRows.Close()

	for taskRows.Next() {
		var respID int
		var text string
		if err := taskRows.Scan(&respID, &text); err != nil {
			dbErr("scan task", err)
			return
		}
		// Same guard as the assignee loop: a task whose responsibility is not in
		// the map is skipped, never dereferenced.
		if loc, ok := respMap[respID]; ok {
			categories[loc.catIdx].Responsibilities[loc.respIdx].Tasks = append(
				categories[loc.catIdx].Responsibilities[loc.respIdx].Tasks, text,
			)
		}
	}
	if err := taskRows.Err(); err != nil {
		dbErr("iterate tasks", err)
		return
	}
	taskRows.Close()

	if categories == nil {
		categories = []OCCategory{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(categories)
}

// POST /api/officer-command/categories
func createOCCategory(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	var maxOrder int
	if err := db.QueryRow(`SELECT COALESCE(MAX(display_order), -1) FROM oc_categories`).Scan(&maxOrder); err != nil {
		slog.Error("officer command: read max category order", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	res, err := db.Exec(`INSERT INTO oc_categories (name, display_order) VALUES (?, ?)`, body.Name, maxOrder+1)
	if err != nil {
		slog.Error("officer command: create category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "oc_category", body.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OCCategory{
		ID:               int(id),
		Name:             body.Name,
		DisplayOrder:     maxOrder + 1,
		Responsibilities: []OCResponsibility{},
	})
}

// PUT /api/officer-command/categories/{id}
func updateOCCategory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	var oldName string
	switch err := db.QueryRow(`SELECT name FROM oc_categories WHERE id = ?`, id).Scan(&oldName); {
	case err == sql.ErrNoRows:
		http.Error(w, "Category not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(`UPDATE oc_categories SET name = ? WHERE id = ?`, body.Name, id); err != nil {
		slog.Error("officer command: update category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	details := ""
	if oldName != body.Name {
		details = "name: " + oldName + " → " + body.Name
	}
	logActivity(user.ID, user.Username, "updated", "oc_category", body.Name, false, details)

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/officer-command/categories/{id}
//
// Deletes two levels of children explicitly. foreign_keys is off app-wide, so
// the ON DELETE CASCADE in the schema has never fired and every earlier delete
// through this handler left its children stranded.
func deleteOCCategory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var catName string
	switch err := db.QueryRow(`SELECT name FROM oc_categories WHERE id = ?`, id).Scan(&catName); {
	case err == sql.ErrNoRows:
		http.Error(w, "Category not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin delete category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	children := []string{
		`DELETE FROM oc_tasks WHERE responsibility_id IN (SELECT id FROM oc_responsibilities WHERE category_id = ?)`,
		`DELETE FROM oc_assignees WHERE responsibility_id IN (SELECT id FROM oc_responsibilities WHERE category_id = ?)`,
		`DELETE FROM oc_responsibilities WHERE category_id = ?`,
		`DELETE FROM oc_categories WHERE id = ?`,
	}
	for _, q := range children {
		if _, err := tx.Exec(q, id); err != nil {
			slog.Error("officer command: delete category", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit delete category", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "oc_category", catName, false)

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/officer-command/responsibilities
func createOCResponsibility(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CategoryID  int      `json:"category_id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Frequency   string   `json:"frequency"`
		Tasks       []string `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.CategoryID == 0 {
		http.Error(w, "category_id and name are required", http.StatusBadRequest)
		return
	}
	if body.Frequency != "Daily" && body.Frequency != "Weekly" && body.Frequency != "Seasonal" {
		body.Frequency = "Weekly"
	}
	tasks, ok := normaliseTasks(body.Tasks)
	if !ok {
		http.Error(w, ocTaskLimitError, http.StatusBadRequest)
		return
	}

	// Fail closed: continuing on a default max order would stack the new row on
	// top of an existing one.
	var maxOrder int
	if err := db.QueryRow(
		`SELECT COALESCE(MAX(display_order), -1) FROM oc_responsibilities WHERE category_id = ?`,
		body.CategoryID,
	).Scan(&maxOrder); err != nil {
		slog.Error("officer command: read max responsibility order", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin create responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO oc_responsibilities (category_id, name, description, frequency, display_order) VALUES (?, ?, ?, ?, ?)`,
		body.CategoryID, body.Name, body.Description, body.Frequency, maxOrder+1,
	)
	if err != nil {
		slog.Error("officer command: create responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	if err := replaceOCTasks(tx, int(id), tasks); err != nil {
		slog.Error("officer command: write tasks", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit create responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	details := ""
	if len(tasks) > 0 {
		details = "tasks: " + strconv.Itoa(len(tasks))
	}
	logActivity(user.ID, user.Username, "created", "oc_responsibility", body.Name, false, details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OCResponsibility{
		ID:           int(id),
		CategoryID:   body.CategoryID,
		Name:         body.Name,
		Description:  body.Description,
		Frequency:    body.Frequency,
		DisplayOrder: maxOrder + 1,
		Assignees:    []OCAssignee{},
		Tasks:        tasks,
	})
}

// PUT /api/officer-command/responsibilities/{id}
func updateOCResponsibility(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Frequency   string   `json:"frequency"`
		Tasks       []string `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if body.Frequency != "Daily" && body.Frequency != "Weekly" && body.Frequency != "Seasonal" {
		body.Frequency = "Weekly"
	}
	tasks, ok := normaliseTasks(body.Tasks)
	if !ok {
		http.Error(w, ocTaskLimitError, http.StatusBadRequest)
		return
	}

	// Read everything the activity detail needs before the transaction opens, so
	// the single connection is never held across a read and a write. An
	// unchecked Scan here would silently wipe the task list against a zero value.
	var oldName, oldFreq, oldDesc string
	switch err := db.QueryRow(
		`SELECT name, description, frequency FROM oc_responsibilities WHERE id = ?`, id,
	).Scan(&oldName, &oldDesc, &oldFreq); {
	case err == sql.ErrNoRows:
		http.Error(w, "Responsibility not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	oldTasks, err := readOCTasks(id)
	if err != nil {
		slog.Error("officer command: read tasks", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin update responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE oc_responsibilities SET name = ?, description = ?, frequency = ? WHERE id = ?`,
		body.Name, body.Description, body.Frequency, id,
	); err != nil {
		slog.Error("officer command: update responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := replaceOCTasks(tx, id, tasks); err != nil {
		slog.Error("officer command: write tasks", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit update responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	var changes []string
	if oldName != body.Name {
		changes = append(changes, "name: "+oldName+" → "+body.Name)
	}
	if oldFreq != body.Frequency {
		changes = append(changes, "frequency: "+oldFreq+" → "+body.Frequency)
	}
	if oldDesc != body.Description {
		changes = append(changes, "description updated")
	}
	// Whenever the list differs, not only when the count does: a same-length
	// rewording is still a change to an officer's instructions.
	if !sameTasks(oldTasks, tasks) {
		changes = append(changes, "tasks updated ("+strconv.Itoa(len(oldTasks))+" → "+strconv.Itoa(len(tasks))+")")
	}
	logActivity(user.ID, user.Username, "updated", "oc_responsibility", body.Name, false, strings.Join(changes, "; "))

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/officer-command/responsibilities/{id}
func deleteOCResponsibility(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var respName string
	switch err := db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, id).Scan(&respName); {
	case err == sql.ErrNoRows:
		http.Error(w, "Responsibility not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin delete responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Children first, explicitly -- the schema's cascade never runs.
	children := []string{
		`DELETE FROM oc_tasks WHERE responsibility_id = ?`,
		`DELETE FROM oc_assignees WHERE responsibility_id = ?`,
		`DELETE FROM oc_responsibilities WHERE id = ?`,
	}
	for _, q := range children {
		if _, err := tx.Exec(q, id); err != nil {
			slog.Error("officer command: delete responsibility", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit delete responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "oc_responsibility", respName, false)

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/officer-command/responsibilities/{id}/assignees
func addOCAssignee(w http.ResponseWriter, r *http.Request) {
	respID, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		MemberID int `json:"member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.MemberID == 0 {
		http.Error(w, "member_id is required", http.StatusBadRequest)
		return
	}

	var a OCAssignee
	a.MemberID = body.MemberID
	switch err := db.QueryRow(`SELECT name, rank FROM members WHERE id = ?`, body.MemberID).Scan(&a.Name, &a.Rank); {
	case err == sql.ErrNoRows:
		http.Error(w, "Member not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read member", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var respName string
	switch err := db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, respID).Scan(&respName); {
	case err == sql.ErrNoRows:
		http.Error(w, "Responsibility not found", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("officer command: read responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(
		`INSERT OR IGNORE INTO oc_assignees (responsibility_id, member_id) VALUES (?, ?)`,
		respID, body.MemberID,
	); err != nil {
		slog.Error("officer command: add assignee", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "created", "oc_assignee", a.Name, false, respName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

// DELETE /api/officer-command/responsibilities/{id}/assignees/{member_id}
func removeOCAssignee(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	respID, _ := strconv.Atoi(vars["id"])
	memberID, _ := strconv.Atoi(vars["member_id"])

	// Names are for the activity log only, so a missing one is not fatal here --
	// the delete below is still the right thing to do.
	var memberName, respName string
	if err := db.QueryRow(`SELECT name FROM members WHERE id = ?`, memberID).Scan(&memberName); err != nil && err != sql.ErrNoRows {
		slog.Error("officer command: read member", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, respID).Scan(&respName); err != nil && err != sql.ErrNoRows {
		slog.Error("officer command: read responsibility", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(
		`DELETE FROM oc_assignees WHERE responsibility_id = ? AND member_id = ?`,
		respID, memberID,
	); err != nil {
		slog.Error("officer command: remove assignee", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "oc_assignee", memberName, false, respName)

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/officer-command/categories/reorder
func reorderOCCategories(w http.ResponseWriter, r *http.Request) {
	var items []struct {
		ID           int `json:"id"`
		DisplayOrder int `json:"display_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin reorder categories", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range items {
		if _, err := tx.Exec(`UPDATE oc_categories SET display_order = ? WHERE id = ?`, item.DisplayOrder, item.ID); err != nil {
			slog.Error("officer command: reorder categories", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit reorder categories", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/officer-command/responsibilities/reorder
func reorderOCResponsibilities(w http.ResponseWriter, r *http.Request) {
	var items []struct {
		ID           int `json:"id"`
		DisplayOrder int `json:"display_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("officer command: begin reorder responsibilities", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range items {
		if _, err := tx.Exec(`UPDATE oc_responsibilities SET display_order = ? WHERE id = ?`, item.DisplayOrder, item.ID); err != nil {
			slog.Error("officer command: reorder responsibilities", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("officer command: commit reorder responsibilities", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
