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
// Helpers
// ---------------------------------------------------------------------------

var validRanks = map[string]bool{"R1": true, "R2": true, "R3": true, "R4": true, "R5": true}

func parsePollOptions(raw string) ([]string, error) {
	var opts []string
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return nil, err
	}
	return opts, nil
}

func validateOptions(opts []string) string {
	if len(opts) < 2 {
		return "at least 2 options required"
	}
	seen := map[string]bool{}
	for _, o := range opts {
		t := strings.TrimSpace(o)
		if t == "" {
			return "options must not be empty"
		}
		if seen[t] {
			return "duplicate option: " + t
		}
		seen[t] = true
	}
	return ""
}

func loadPollInstance(id int) (*PollInstance, error) {
	var pi PollInstance
	err := db.QueryRow(`
		SELECT i.id, i.template_id, i.label, i.question, i.options, i.poll_type,
		       i.multi_select, i.rank_filter, i.total_eligible,
		       COALESCE(u.username,'(deleted)'), i.created_at
		FROM poll_instances i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.id = ?`, id).
		Scan(&pi.ID, &pi.TemplateID, &pi.Label, &pi.Question, &pi.Options,
			&pi.PollType, &pi.MultiSelect, &pi.RankFilter, &pi.TotalEligible,
			&pi.CreatedBy, &pi.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &pi, nil
}

// ---------------------------------------------------------------------------
// Poll Templates
// ---------------------------------------------------------------------------

func handlePollTemplateList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT t.id, t.title, t.question, t.options, t.poll_type, t.multi_select,
		       COALESCE(u.username,'(deleted)'), t.created_at
		FROM poll_templates t
		LEFT JOIN users u ON u.id = t.created_by
		ORDER BY t.created_at ASC`)
	if err != nil {
		slog.Error("handlePollTemplateList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []PollTemplate{}
	for rows.Next() {
		var t PollTemplate
		if err := rows.Scan(&t.ID, &t.Title, &t.Question, &t.Options, &t.PollType,
			&t.MultiSelect, &t.CreatedBy, &t.CreatedAt); err != nil {
			continue
		}
		items = append(items, t)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func handlePollTemplateCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		Title       string   `json:"title"`
		Question    string   `json:"question"`
		Options     []string `json:"options"`
		PollType    string   `json:"poll_type"`
		MultiSelect bool     `json:"multi_select"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Question = strings.TrimSpace(body.Question)
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if body.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}
	if body.PollType != "named" && body.PollType != "anonymous" {
		http.Error(w, "poll_type must be named or anonymous", http.StatusBadRequest)
		return
	}
	if msg := validateOptions(body.Options); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	optsJSON, _ := json.Marshal(body.Options)
	multiInt := 0
	if body.MultiSelect {
		multiInt = 1
	}

	res, err := db.Exec(
		`INSERT INTO poll_templates (title, question, options, poll_type, multi_select, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		body.Title, body.Question, string(optsJSON), body.PollType, multiInt, user.ID,
	)
	if err != nil {
		slog.Error("handlePollTemplateCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "poll_template", body.Title, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handlePollTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Title       string   `json:"title"`
		Question    string   `json:"question"`
		Options     []string `json:"options"`
		PollType    string   `json:"poll_type"`
		MultiSelect bool     `json:"multi_select"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Question = strings.TrimSpace(body.Question)
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if body.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}
	if body.PollType != "named" && body.PollType != "anonymous" {
		http.Error(w, "poll_type must be named or anonymous", http.StatusBadRequest)
		return
	}
	if msg := validateOptions(body.Options); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	optsJSON, _ := json.Marshal(body.Options)
	multiInt := 0
	if body.MultiSelect {
		multiInt = 1
	}

	var old PollTemplate
	err = db.QueryRow(`SELECT title, question, options, poll_type, multi_select FROM poll_templates WHERE id = ?`, id).
		Scan(&old.Title, &old.Question, &old.Options, &old.PollType, &old.MultiSelect)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollTemplateUpdate: fetch old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if _, err := db.Exec(
		`UPDATE poll_templates SET title=?, question=?, options=?, poll_type=?, multi_select=? WHERE id=?`,
		body.Title, body.Question, string(optsJSON), body.PollType, multiInt, id,
	); err != nil {
		slog.Error("handlePollTemplateUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if old.Title != body.Title {
		changes = append(changes, "title: "+old.Title+" → "+body.Title)
	}
	if old.Question != body.Question {
		changes = append(changes, "question updated")
	}
	if old.Options != string(optsJSON) {
		changes = append(changes, "options updated")
	}
	if old.PollType != body.PollType {
		changes = append(changes, "poll_type: "+old.PollType+" → "+body.PollType)
	}
	logActivity(user.ID, user.Username, "updated", "poll_template", body.Title, false, strings.Join(changes, "; "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handlePollTemplateDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var title string
	err = db.QueryRow(`SELECT title FROM poll_templates WHERE id = ?`, id).Scan(&title)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollTemplateDelete: fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handlePollTemplateDelete: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE poll_instances SET template_id = NULL WHERE template_id = ?`, id); err != nil {
		slog.Error("handlePollTemplateDelete: null refs", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM poll_templates WHERE id = ?`, id); err != nil {
		slog.Error("handlePollTemplateDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handlePollTemplateDelete: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "deleted", "poll_template", title, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

// ---------------------------------------------------------------------------
// Poll Instances
// ---------------------------------------------------------------------------

func handlePollInstanceList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		SELECT i.id, i.template_id, i.label, i.question, i.options, i.poll_type,
		       i.multi_select, i.rank_filter, i.total_eligible,
		       COALESCE(u.username,'(deleted)'), i.created_at
		FROM poll_instances i
		LEFT JOIN users u ON u.id = i.created_by
		ORDER BY i.created_at DESC`)
	if err != nil {
		slog.Error("handlePollInstanceList: query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	items := []PollInstance{}
	for rows.Next() {
		var pi PollInstance
		if err := rows.Scan(&pi.ID, &pi.TemplateID, &pi.Label, &pi.Question, &pi.Options,
			&pi.PollType, &pi.MultiSelect, &pi.RankFilter, &pi.TotalEligible,
			&pi.CreatedBy, &pi.CreatedAt); err != nil {
			continue
		}
		items = append(items, pi)
	}

	// Compute responded_count for each instance in one query per type batch
	// to avoid N+1. Use a map for quick lookup.
	ids := make([]any, len(items))
	for i, pi := range items {
		ids[i] = pi.ID
	}

	if len(ids) > 0 {
		// Named polls: COUNT(DISTINCT member_id)
		namedCounts := map[int]int{}
		ph := strings.Repeat("?,", len(ids))
		ph = ph[:len(ph)-1]
		namedRows, err := db.Query(
			`SELECT instance_id, COUNT(DISTINCT member_id) FROM poll_responses WHERE instance_id IN (`+ph+`) GROUP BY instance_id`,
			ids...,
		)
		if err == nil {
			defer namedRows.Close()
			for namedRows.Next() {
				var iid, cnt int
				namedRows.Scan(&iid, &cnt)
				namedCounts[iid] = cnt
			}
		}

		// Anonymous polls: SUM(response_count)
		anonCounts := map[int]int{}
		anonRows, err := db.Query(
			`SELECT instance_id, SUM(response_count) FROM poll_anonymous_counts WHERE instance_id IN (`+ph+`) GROUP BY instance_id`,
			ids...,
		)
		if err == nil {
			defer anonRows.Close()
			for anonRows.Next() {
				var iid, cnt int
				anonRows.Scan(&iid, &cnt)
				anonCounts[iid] = cnt
			}
		}

		for i := range items {
			if items[i].PollType == "named" {
				items[i].RespondedCount = namedCounts[items[i].ID]
			} else {
				items[i].RespondedCount = anonCounts[items[i].ID]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": items})
}

func handlePollInstanceCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)

	var body struct {
		TemplateID int      `json:"template_id"`
		Label      string   `json:"label"`
		RankFilter []string `json:"rank_filter"` // nil/empty = all active
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	if body.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}

	// Validate rank filter values
	for _, rank := range body.RankFilter {
		if !validRanks[rank] {
			http.Error(w, "invalid rank in rank_filter: "+rank, http.StatusBadRequest)
			return
		}
	}

	// Load template
	var tmpl PollTemplate
	err := db.QueryRow(`SELECT id, title, question, options, poll_type, multi_select FROM poll_templates WHERE id = ?`, body.TemplateID).
		Scan(&tmpl.ID, &tmpl.Title, &tmpl.Question, &tmpl.Options, &tmpl.PollType, &tmpl.MultiSelect)
	if err == sql.ErrNoRows {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollInstanceCreate: load template", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	multiInt := 0
	if tmpl.MultiSelect {
		multiInt = 1
	}

	// Snapshot rank_filter
	var rankFilterStr *string
	if len(body.RankFilter) > 0 {
		rf, _ := json.Marshal(body.RankFilter)
		s := string(rf)
		rankFilterStr = &s
	}

	// Compute total_eligible
	var totalEligible int
	if rankFilterStr != nil {
		// Use json_each to avoid dynamic SQL
		db.QueryRow(
			`SELECT COUNT(*) FROM members WHERE rank != 'EX' AND rank IN (SELECT value FROM json_each(?))`,
			*rankFilterStr,
		).Scan(&totalEligible)
	} else {
		db.QueryRow(`SELECT COUNT(*) FROM members WHERE rank != 'EX'`).Scan(&totalEligible)
	}

	res, err := db.Exec(
		`INSERT INTO poll_instances (template_id, label, question, options, poll_type, multi_select, rank_filter, total_eligible, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		body.TemplateID, body.Label, tmpl.Question, tmpl.Options, tmpl.PollType, multiInt, rankFilterStr, totalEligible, user.ID,
	)
	if err != nil {
		slog.Error("handlePollInstanceCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "poll_instance", body.Label, false)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func handlePollInstanceUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Label    string   `json:"label"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	body.Question = strings.TrimSpace(body.Question)
	if body.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	if body.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}
	for i, o := range body.Options {
		body.Options[i] = strings.TrimSpace(o)
		if body.Options[i] == "" {
			http.Error(w, "options must not be empty", http.StatusBadRequest)
			return
		}
	}

	var oldLabel, oldQuestion, oldOptsJSON string
	err = db.QueryRow(`SELECT label, question, options FROM poll_instances WHERE id = ?`, id).
		Scan(&oldLabel, &oldQuestion, &oldOptsJSON)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollInstanceUpdate: fetch old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var oldOpts []string
	json.Unmarshal([]byte(oldOptsJSON), &oldOpts)

	// Options are optional in the body; if provided, count must match
	if len(body.Options) > 0 && len(body.Options) != len(oldOpts) {
		http.Error(w, "cannot change the number of options", http.StatusBadRequest)
		return
	}
	newOpts := oldOpts
	if len(body.Options) > 0 {
		newOpts = body.Options
	}
	newOptsJSON, _ := json.Marshal(newOpts)

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handlePollInstanceUpdate: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Remap response/count records for any renamed options (by position)
	for i, newOpt := range newOpts {
		if i < len(oldOpts) && oldOpts[i] != newOpt {
			if _, err := tx.Exec(`UPDATE poll_responses SET option_key = ? WHERE instance_id = ? AND option_key = ?`, newOpt, id, oldOpts[i]); err != nil {
				slog.Error("handlePollInstanceUpdate: remap responses", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
			if _, err := tx.Exec(`UPDATE poll_anonymous_counts SET option_key = ? WHERE instance_id = ? AND option_key = ?`, newOpt, id, oldOpts[i]); err != nil {
				slog.Error("handlePollInstanceUpdate: remap counts", "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
		}
	}

	if _, err := tx.Exec(`UPDATE poll_instances SET label = ?, question = ?, options = ? WHERE id = ?`,
		body.Label, body.Question, string(newOptsJSON), id); err != nil {
		slog.Error("handlePollInstanceUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handlePollInstanceUpdate: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var changes []string
	if oldLabel != body.Label {
		changes = append(changes, "label: "+oldLabel+" → "+body.Label)
	}
	if oldQuestion != body.Question {
		changes = append(changes, "question updated")
	}
	if string(newOptsJSON) != oldOptsJSON {
		changes = append(changes, "options updated")
	}
	if len(changes) > 0 {
		logActivity(user.ID, user.Username, "updated", "poll_instance", body.Label, false, strings.Join(changes, "; "))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}

func handlePollInstanceDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var label string
	err = db.QueryRow(`SELECT label FROM poll_instances WHERE id = ?`, id).Scan(&label)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollInstanceDelete: fetch", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handlePollInstanceDelete: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, tbl := range []string{"poll_responses", "poll_anonymous_counts"} {
		if _, err := tx.Exec(`DELETE FROM `+tbl+` WHERE instance_id = ?`, id); err != nil {
			slog.Error("handlePollInstanceDelete: delete "+tbl, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(`DELETE FROM poll_instances WHERE id = ?`, id); err != nil {
		slog.Error("handlePollInstanceDelete: delete instance", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handlePollInstanceDelete: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	logActivity(user.ID, user.Username, "deleted", "poll_instance", label, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Deleted"})
}

// ---------------------------------------------------------------------------
// Poll Instance Detail
// ---------------------------------------------------------------------------

func handlePollInstanceDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	pi, err := loadPollInstance(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollInstanceDetail: load instance", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if pi.PollType == "anonymous" {
		// Seed all options at 0, then overwrite with real counts
		opts, _ := parsePollOptions(pi.Options)
		counts := make([]PollAnonCount, len(opts))
		for i, o := range opts {
			counts[i] = PollAnonCount{OptionKey: o, ResponseCount: 0}
		}
		rows, err := db.Query(`SELECT option_key, response_count FROM poll_anonymous_counts WHERE instance_id = ?`, id)
		if err != nil {
			slog.Error("handlePollInstanceDetail: anon counts", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		dbCounts := map[string]int{}
		for rows.Next() {
			var key string
			var cnt int
			rows.Scan(&key, &cnt)
			dbCounts[key] = cnt
		}
		for i := range counts {
			if cnt, ok := dbCounts[counts[i].OptionKey]; ok {
				counts[i].ResponseCount = cnt
			}
		}
		total := 0
		for _, c := range counts {
			total += c.ResponseCount
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"instance":        pi,
			"counts":          counts,
			"responded_count": total,
		})
		return
	}

	// Named poll: two queries — members then responses
	memberQuery := `
		SELECT m.id, m.name, m.rank FROM members m
		WHERE m.rank != 'EX'`
	var memberArgs []any
	if pi.RankFilter != nil {
		memberQuery += ` AND m.rank IN (SELECT value FROM json_each(?))`
		memberArgs = append(memberArgs, *pi.RankFilter)
	}
	memberQuery += ` ORDER BY m.name ASC`

	memberRows, err := db.Query(memberQuery, memberArgs...)
	if err != nil {
		slog.Error("handlePollInstanceDetail: member query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer memberRows.Close()

	statusMap := map[int]*PollMemberStatus{}
	var orderedIDs []int
	for memberRows.Next() {
		var ms PollMemberStatus
		if err := memberRows.Scan(&ms.MemberID, &ms.MemberName, &ms.Rank); err != nil {
			continue
		}
		ms.Options = []string{}
		statusMap[ms.MemberID] = &ms
		orderedIDs = append(orderedIDs, ms.MemberID)
	}

	respRows, err := db.Query(
		`SELECT member_id, option_key FROM poll_responses WHERE instance_id = ?`, id)
	if err != nil {
		slog.Error("handlePollInstanceDetail: response query", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer respRows.Close()
	for respRows.Next() {
		var mid int
		var key string
		respRows.Scan(&mid, &key)
		if ms, ok := statusMap[mid]; ok {
			ms.Responded = true
			ms.Options = append(ms.Options, key)
		}
	}

	pending := []PollMemberStatus{}
	responded := []PollMemberStatus{}
	for _, mid := range orderedIDs {
		ms := statusMap[mid]
		if ms.Responded {
			responded = append(responded, *ms)
		} else {
			pending = append(pending, *ms)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"instance":  pi,
		"pending":   pending,
		"responded": responded,
	})
}

// ---------------------------------------------------------------------------
// Responses (named polls)
// ---------------------------------------------------------------------------

func handlePollResponseSet(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		MemberID int      `json:"member_id"`
		Options  []string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pi, err := loadPollInstance(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollResponseSet: load instance", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if pi.PollType != "named" {
		http.Error(w, "cannot log named responses on an anonymous poll", http.StatusBadRequest)
		return
	}

	// Validate member is active
	var memberExists int
	db.QueryRow(`SELECT COUNT(*) FROM members WHERE id = ? AND rank != 'EX'`, body.MemberID).Scan(&memberExists)
	if memberExists == 0 {
		http.Error(w, "Member not found or not active", http.StatusBadRequest)
		return
	}

	// Validate options exist in snapshot
	validOpts, _ := parsePollOptions(pi.Options)
	optSet := map[string]bool{}
	for _, o := range validOpts {
		optSet[o] = true
	}
	for _, o := range body.Options {
		if !optSet[o] {
			http.Error(w, "invalid option: "+o, http.StatusBadRequest)
			return
		}
	}

	// Single-select: reject more than one option
	if !pi.MultiSelect && len(body.Options) > 1 {
		http.Error(w, "this poll only allows one option", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handlePollResponseSet: begin tx", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM poll_responses WHERE instance_id = ? AND member_id = ?`, id, body.MemberID); err != nil {
		slog.Error("handlePollResponseSet: delete old", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for _, opt := range body.Options {
		if _, err := tx.Exec(
			`INSERT INTO poll_responses (instance_id, member_id, option_key, recorded_by) VALUES (?, ?, ?, ?)`,
			id, body.MemberID, opt, user.ID,
		); err != nil {
			slog.Error("handlePollResponseSet: insert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handlePollResponseSet: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Recorded"})
}

func handlePollResponseClear(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	memberID, err := strconv.Atoi(mux.Vars(r)["memberID"])
	if err != nil {
		http.Error(w, "Invalid member ID", http.StatusBadRequest)
		return
	}

	pi, err := loadPollInstance(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollResponseClear: load instance", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if pi.PollType != "named" {
		http.Error(w, "cannot clear responses on an anonymous poll", http.StatusBadRequest)
		return
	}

	if _, err := db.Exec(`DELETE FROM poll_responses WHERE instance_id = ? AND member_id = ?`, id, memberID); err != nil {
		slog.Error("handlePollResponseClear: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Cleared"})
}

// ---------------------------------------------------------------------------
// Anonymous counts
// ---------------------------------------------------------------------------

func handlePollAnonCountsUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var body struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	pi, err := loadPollInstance(id)
	if err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("handlePollAnonCountsUpdate: load instance", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if pi.PollType != "anonymous" {
		http.Error(w, "cannot set counts on a named poll", http.StatusBadRequest)
		return
	}

	validOpts, _ := parsePollOptions(pi.Options)
	optSet := map[string]bool{}
	for _, o := range validOpts {
		optSet[o] = true
	}
	for key := range body.Counts {
		if !optSet[key] {
			http.Error(w, "invalid option: "+key, http.StatusBadRequest)
			return
		}
	}

	for key, cnt := range body.Counts {
		if _, err := db.Exec(`
			INSERT INTO poll_anonymous_counts (instance_id, option_key, response_count)
			VALUES (?, ?, ?)
			ON CONFLICT(instance_id, option_key) DO UPDATE SET response_count = excluded.response_count`,
			id, key, cnt,
		); err != nil {
			slog.Error("handlePollAnonCountsUpdate: upsert", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Updated"})
}
