package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// GET /api/officer-command/data
func getOfficerCommandData(w http.ResponseWriter, r *http.Request) {
	// Fetch all categories ordered by display_order
	catRows, err := db.Query(`SELECT id, name, display_order FROM oc_categories ORDER BY display_order, id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer catRows.Close()

	var categories []OCCategory
	catMap := map[int]int{} // category ID -> index in categories slice
	for catRows.Next() {
		var c OCCategory
		if err := catRows.Scan(&c.ID, &c.Name, &c.DisplayOrder); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.Responsibilities = []OCResponsibility{}
		catMap[c.ID] = len(categories)
		categories = append(categories, c)
	}

	// Fetch all responsibilities
	respRows, err := db.Query(`SELECT id, category_id, name, description, frequency, display_order FROM oc_responsibilities ORDER BY display_order, id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer respRows.Close()

	type respLocation struct{ catIdx, respIdx int }
	respMap := map[int]respLocation{}
	for respRows.Next() {
		var rp OCResponsibility
		if err := respRows.Scan(&rp.ID, &rp.CategoryID, &rp.Name, &rp.Description, &rp.Frequency, &rp.DisplayOrder); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rp.Assignees = []OCAssignee{}
		catIdx, ok := catMap[rp.CategoryID]
		if !ok {
			continue
		}
		respIdx := len(categories[catIdx].Responsibilities)
		categories[catIdx].Responsibilities = append(categories[catIdx].Responsibilities, rp)
		respMap[rp.ID] = respLocation{catIdx, respIdx}
	}

	// Fetch all assignees
	asgRows, err := db.Query(`
		SELECT oa.responsibility_id, m.id, m.name, m.rank
		FROM oc_assignees oa
		JOIN members m ON m.id = oa.member_id
		ORDER BY m.rank DESC, m.name ASC
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer asgRows.Close()

	for asgRows.Next() {
		var respID int
		var a OCAssignee
		if err := asgRows.Scan(&respID, &a.MemberID, &a.Name, &a.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if loc, ok := respMap[respID]; ok {
			categories[loc.catIdx].Responsibilities[loc.respIdx].Assignees = append(
				categories[loc.catIdx].Responsibilities[loc.respIdx].Assignees, a,
			)
		}
	}

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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	var maxOrder int
	db.QueryRow(`SELECT COALESCE(MAX(display_order), -1) FROM oc_categories`).Scan(&maxOrder)

	res, err := db.Exec(`INSERT INTO oc_categories (name, display_order) VALUES (?, ?)`, body.Name, maxOrder+1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "created", "oc_category", body.Name, false)

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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	var oldName string
	db.QueryRow(`SELECT name FROM oc_categories WHERE id = ?`, id).Scan(&oldName)

	_, err := db.Exec(`UPDATE oc_categories SET name = ? WHERE id = ?`, body.Name, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	details := ""
	if oldName != body.Name {
		details = "name: " + oldName + " → " + body.Name
	}
	logActivity(actorID, actorName, "updated", "oc_category", body.Name, false, details)

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/officer-command/categories/{id}
func deleteOCCategory(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var catName string
	db.QueryRow(`SELECT name FROM oc_categories WHERE id = ?`, id).Scan(&catName)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM oc_categories WHERE id = ?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "oc_category", catName, false)

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/officer-command/responsibilities
func createOCResponsibility(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CategoryID  int    `json:"category_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Frequency   string `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.CategoryID == 0 {
		http.Error(w, "category_id and name are required", http.StatusBadRequest)
		return
	}
	if body.Frequency != "Daily" && body.Frequency != "Weekly" && body.Frequency != "Seasonal" {
		body.Frequency = "Weekly"
	}

	var maxOrder int
	db.QueryRow(`SELECT COALESCE(MAX(display_order), -1) FROM oc_responsibilities WHERE category_id = ?`, body.CategoryID).Scan(&maxOrder)

	res, err := db.Exec(
		`INSERT INTO oc_responsibilities (category_id, name, description, frequency, display_order) VALUES (?, ?, ?, ?, ?)`,
		body.CategoryID, body.Name, body.Description, body.Frequency, maxOrder+1,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "created", "oc_responsibility", body.Name, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(OCResponsibility{
		ID:           int(id),
		CategoryID:   body.CategoryID,
		Name:         body.Name,
		Description:  body.Description,
		Frequency:    body.Frequency,
		DisplayOrder: maxOrder + 1,
		Assignees:    []OCAssignee{},
	})
}

// PUT /api/officer-command/responsibilities/{id}
func updateOCResponsibility(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Frequency   string `json:"frequency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if body.Frequency != "Daily" && body.Frequency != "Weekly" && body.Frequency != "Seasonal" {
		body.Frequency = "Weekly"
	}

	var oldName, oldFreq string
	db.QueryRow(`SELECT name, frequency FROM oc_responsibilities WHERE id = ?`, id).Scan(&oldName, &oldFreq)

	_, err := db.Exec(
		`UPDATE oc_responsibilities SET name = ?, description = ?, frequency = ? WHERE id = ?`,
		body.Name, body.Description, body.Frequency, id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	var changes []string
	if oldName != body.Name {
		changes = append(changes, "name: "+oldName+" → "+body.Name)
	}
	if oldFreq != body.Frequency {
		changes = append(changes, "frequency: "+oldFreq+" → "+body.Frequency)
	}
	details := strings.Join(changes, "; ")
	logActivity(actorID, actorName, "updated", "oc_responsibility", body.Name, false, details)

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/officer-command/responsibilities/{id}
func deleteOCResponsibility(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(mux.Vars(r)["id"])

	var respName string
	db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, id).Scan(&respName)

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM oc_responsibilities WHERE id = ?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "oc_responsibility", respName, false)

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/officer-command/responsibilities/{id}/assignees
func addOCAssignee(w http.ResponseWriter, r *http.Request) {
	respID, _ := strconv.Atoi(mux.Vars(r)["id"])
	var body struct {
		MemberID int `json:"member_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MemberID == 0 {
		http.Error(w, "member_id is required", http.StatusBadRequest)
		return
	}

	_, err := db.Exec(
		`INSERT OR IGNORE INTO oc_assignees (responsibility_id, member_id) VALUES (?, ?)`,
		respID, body.MemberID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var a OCAssignee
	a.MemberID = body.MemberID
	db.QueryRow(`SELECT name, rank FROM members WHERE id = ?`, body.MemberID).Scan(&a.Name, &a.Rank)

	var respName string
	db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, respID).Scan(&respName)

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "created", "oc_assignee", a.Name, false, respName)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a)
}

// DELETE /api/officer-command/responsibilities/{id}/assignees/{member_id}
func removeOCAssignee(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	respID, _ := strconv.Atoi(vars["id"])
	memberID, _ := strconv.Atoi(vars["member_id"])

	var memberName, respName string
	db.QueryRow(`SELECT name FROM members WHERE id = ?`, memberID).Scan(&memberName)
	db.QueryRow(`SELECT name FROM oc_responsibilities WHERE id = ?`, respID).Scan(&respName)

	_, err := db.Exec(
		`DELETE FROM oc_assignees WHERE responsibility_id = ? AND member_id = ?`,
		respID, memberID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	session, _ := store.Get(r, "session")
	actorID, _ := session.Values["user_id"].(int)
	actorName, _ := session.Values["username"].(string)
	logActivity(actorID, actorName, "deleted", "oc_assignee", memberName, false, respName)

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/officer-command/categories/reorder
func reorderOCCategories(w http.ResponseWriter, r *http.Request) {
	var items []struct {
		ID           int `json:"id"`
		DisplayOrder int `json:"display_order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range items {
		if _, err := tx.Exec(`UPDATE oc_categories SET display_order = ? WHERE id = ?`, item.DisplayOrder, item.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, item := range items {
		if _, err := tx.Exec(`UPDATE oc_responsibilities SET display_order = ? WHERE id = ?`, item.DisplayOrder, item.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
