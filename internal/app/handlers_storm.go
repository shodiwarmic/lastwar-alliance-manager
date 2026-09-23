package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func getStormAssignments(w http.ResponseWriter, r *http.Request) {
	taskForce := r.URL.Query().Get("task_force")
	if taskForce == "" {
		taskForce = "A"
	}

	rows, err := db.Query(`
		SELECT id, task_force, building_id, member_id, position
		FROM storm_assignments
		WHERE task_force = ?
		ORDER BY building_id, position
	`, taskForce)
	if err != nil {
		slog.Error("getStormAssignments: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	assignments := []StormAssignment{}
	for rows.Next() {
		var a StormAssignment
		if err := rows.Scan(&a.ID, &a.TaskForce, &a.BuildingID, &a.MemberID, &a.Position); err != nil {
			slog.Error("getStormAssignments: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		assignments = append(assignments, a)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assignments)
}

func saveStormAssignments(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TaskForce   string `json:"task_force"`
		Assignments []struct {
			BuildingID string `json:"building_id"`
			MemberID   int    `json:"member_id"`
			Position   int    `json:"position"`
		} `json:"assignments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if request.TaskForce != "A" && request.TaskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("saveStormAssignments: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM storm_assignments WHERE task_force = ?", request.TaskForce)
	if err != nil {
		slog.Error("saveStormAssignments: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	for _, assignment := range request.Assignments {
		_, err = tx.Exec(`
			INSERT INTO storm_assignments (task_force, building_id, member_id, position)
			VALUES (?, ?, ?, ?)
		`, request.TaskForce, assignment.BuildingID, assignment.MemberID, assignment.Position)
		if err != nil {
			slog.Error("saveStormAssignments: insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("saveStormAssignments: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "updated", "storm_assignments", "TF "+request.TaskForce, false, strconv.Itoa(len(request.Assignments))+" assignments")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Assignments saved successfully",
	})
}

func deleteStormAssignments(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskForce := vars["taskForce"]

	if taskForce != "A" && taskForce != "B" {
		http.Error(w, "Invalid task force - must be A or B", http.StatusBadRequest)
		return
	}

	_, err := db.Exec("DELETE FROM storm_assignments WHERE task_force = ?", taskForce)
	if err != nil {
		slog.Error("deleteStormAssignments: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Storm TF Config ---

func getStormConfig(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT task_force, time_slot, participating FROM storm_tf_config ORDER BY task_force`)
	if err != nil {
		slog.Error("getStormConfig: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var configs []StormTFConfig
	for rows.Next() {
		var c StormTFConfig
		if err := rows.Scan(&c.TaskForce, &c.TimeSlot, &c.Participating); err != nil {
			slog.Error("getStormConfig: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		configs = append(configs, c)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configs)
}

func saveStormConfig(w http.ResponseWriter, r *http.Request) {
	var configs []StormTFConfig
	if err := json.NewDecoder(r.Body).Decode(&configs); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("saveStormConfig: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	for _, c := range configs {
		if c.TaskForce != "A" && c.TaskForce != "B" {
			http.Error(w, "invalid task_force", 400)
			return
		}
		if c.TimeSlot != nil && (*c.TimeSlot < 1 || *c.TimeSlot > 3) {
			http.Error(w, "invalid time_slot", 400)
			return
		}
		if c.Participating != 0 && c.Participating != 1 {
			http.Error(w, "invalid participating value", 400)
			return
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO storm_tf_config (task_force, time_slot, participating) VALUES (?,?,?)`, c.TaskForce, c.TimeSlot, c.Participating); err != nil {
			slog.Error("saveStormConfig: insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("saveStormConfig: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	var tfParts []string
	for _, c := range configs {
		slot := "unset"
		if c.TimeSlot != nil {
			slot = "slot " + strconv.Itoa(*c.TimeSlot)
		}
		tfParts = append(tfParts, "TF "+c.TaskForce+": "+slot)
	}
	logActivity(actorID, actorName, "updated", "storm_config", "storm TF config", false, strings.Join(tfParts, "; "))

	w.WriteHeader(http.StatusNoContent)
}

// --- Storm Registrations ---

func getStormRegistrations(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`
		WITH latest_power AS (
			SELECT member_id, power FROM (
				SELECT member_id, power,
				       ROW_NUMBER() OVER (PARTITION BY member_id ORDER BY recorded_at DESC) AS rn
				FROM power_history
			) WHERE rn = 1
		)
		SELECT m.id, m.name, m.rank, lp.power,
		       COALESCE(r.id, 0), COALESCE(r.slot_1,0), COALESCE(r.slot_2,0), COALESCE(r.slot_3,0),
		       COALESCE(r.updated_at,'')
		FROM members m
		LEFT JOIN latest_power lp ON lp.member_id = m.id
		LEFT JOIN storm_registrations r ON r.member_id = m.id
		ORDER BY
		    CASE m.rank WHEN 'R5' THEN 1 WHEN 'R4' THEN 2 WHEN 'R3' THEN 3 WHEN 'R2' THEN 4 WHEN 'R1' THEN 5 ELSE 6 END,
		    lp.power DESC`)
	if err != nil {
		slog.Error("getStormRegistrations: query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var regs []StormRegistration
	for rows.Next() {
		var reg StormRegistration
		if err := rows.Scan(&reg.MemberID, &reg.MemberName, &reg.MemberRank, &reg.MemberPower,
			&reg.ID, &reg.Slot1, &reg.Slot2, &reg.Slot3, &reg.UpdatedAt); err != nil {
			slog.Error("getStormRegistrations: scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		regs = append(regs, reg)
	}
	if regs == nil {
		regs = []StormRegistration{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(regs)
}

func upsertMyRegistration(w http.ResponseWriter, r *http.Request) {
	authUser := getAuthUser(r)
	if authUser.MemberID == nil {
		http.Error(w, "no linked member", 403)
		return
	}
	memberID := *authUser.MemberID
	var body struct {
		Slot1 int `json:"slot_1"`
		Slot2 int `json:"slot_2"`
		Slot3 int `json:"slot_3"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`INSERT INTO storm_registrations (member_id, slot_1, slot_2, slot_3, updated_at)
		VALUES (?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(member_id) DO UPDATE SET slot_1=excluded.slot_1, slot_2=excluded.slot_2,
		slot_3=excluded.slot_3, updated_at=CURRENT_TIMESTAMP`,
		memberID, body.Slot1, body.Slot2, body.Slot3)
	if err != nil {
		slog.Error("upsertMyRegistration: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getMyRegistration(w http.ResponseWriter, r *http.Request) {
	authUser := getAuthUser(r)
	if authUser.MemberID == nil {
		http.Error(w, "no linked member", 403)
		return
	}
	memberID := *authUser.MemberID
	var reg StormRegistration
	err := db.QueryRow(`SELECT id, member_id, slot_1, slot_2, slot_3, updated_at FROM storm_registrations WHERE member_id=?`, memberID).
		Scan(&reg.ID, &reg.MemberID, &reg.Slot1, &reg.Slot2, &reg.Slot3, &reg.UpdatedAt)
	if err != nil {
		// Return empty registration if none exists
		reg = StormRegistration{MemberID: memberID}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reg)
}

func upsertMemberRegistration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	memberID := vars["member_id"]
	var body struct {
		Slot1 int `json:"slot_1"`
		Slot2 int `json:"slot_2"`
		Slot3 int `json:"slot_3"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	_, err := db.Exec(`INSERT INTO storm_registrations (member_id, slot_1, slot_2, slot_3, updated_at)
		VALUES (?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT(member_id) DO UPDATE SET slot_1=excluded.slot_1, slot_2=excluded.slot_2,
		slot_3=excluded.slot_3, updated_at=CURRENT_TIMESTAMP`,
		memberID, body.Slot1, body.Slot2, body.Slot3)
	if err != nil {
		slog.Error("upsertMemberRegistration: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func deleteMemberRegistration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if _, err := db.Exec(`DELETE FROM storm_registrations WHERE member_id = ?`, vars["member_id"]); err != nil {
		slog.Error("deleteMemberRegistration: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Storm Groups ---

func getStormAssignedMemberIDs(taskForce string) (map[int]bool, error) {
	assigned := map[int]bool{}
	rows, err := db.Query(`
		SELECT sgbm.member_id FROM storm_group_building_members sgbm
		JOIN storm_group_buildings sgb ON sgb.id = sgbm.group_building_id
		JOIN storm_groups sg ON sg.id = sgb.group_id
		WHERE sg.task_force = ?`, taskForce)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		rows.Scan(&id)
		assigned[id] = true
	}

	rows2, err := db.Query(`
		SELECT sgm.member_id FROM storm_group_members sgm
		JOIN storm_groups sg ON sg.id = sgm.group_id
		WHERE sg.task_force = ?`, taskForce)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var id int
		rows2.Scan(&id)
		assigned[id] = true
	}
	return assigned, nil
}

func getStormGroups(w http.ResponseWriter, r *http.Request) {
	tf := r.URL.Query().Get("task_force")
	if tf != "A" && tf != "B" {
		tf = "A"
	}

	groupRows, err := db.Query(`SELECT id, task_force, name, COALESCE(instructions,''), sort_order FROM storm_groups WHERE task_force=? ORDER BY sort_order`, tf)
	if err != nil {
		slog.Error("getStormGroups: groups query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer groupRows.Close()
	groupMap := map[int]*StormGroup{}
	var groups []*StormGroup
	for groupRows.Next() {
		var g StormGroup
		if err := groupRows.Scan(&g.ID, &g.TaskForce, &g.Name, &g.Instructions, &g.SortOrder); err != nil {
			slog.Error("getStormGroups: groups scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		g.Buildings = []StormGroupBuilding{}
		g.DirectMembers = []StormGroupMember{}
		groupMap[g.ID] = &g
		groups = append(groups, &g)
	}

	bldgRows, err := db.Query(`SELECT sgb.id, sgb.group_id, sgb.building_id, sgb.sort_order FROM storm_group_buildings sgb JOIN storm_groups sg ON sg.id=sgb.group_id WHERE sg.task_force=? ORDER BY sgb.sort_order`, tf)
	if err != nil {
		slog.Error("getStormGroups: buildings query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer bldgRows.Close()
	// Index a building by its group and position, never by a pointer into
	// g.Buildings: a later append to the same group can move the backing array, and
	// members attached through a stale pointer were silently lost on read.
	type bldgRef struct {
		group *StormGroup
		index int
	}
	bldgMap := map[int]bldgRef{}
	for bldgRows.Next() {
		var b StormGroupBuilding
		var groupID int
		if err := bldgRows.Scan(&b.ID, &groupID, &b.BuildingID, &b.SortOrder); err != nil {
			slog.Error("getStormGroups: buildings scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		b.Members = []StormGroupMember{}
		if g, ok := groupMap[groupID]; ok {
			g.Buildings = append(g.Buildings, b)
			bldgMap[b.ID] = bldgRef{g, len(g.Buildings) - 1}
		}
	}

	memRows, err := db.Query(`SELECT sgbm.id, sgbm.group_building_id, sgbm.member_id, sgbm.is_sub, sgbm.position FROM storm_group_building_members sgbm JOIN storm_group_buildings sgb ON sgb.id=sgbm.group_building_id JOIN storm_groups sg ON sg.id=sgb.group_id WHERE sg.task_force=? ORDER BY sgbm.position`, tf)
	if err != nil {
		slog.Error("getStormGroups: building members query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer memRows.Close()
	for memRows.Next() {
		var m StormGroupMember
		var gbID, isSub int
		if err := memRows.Scan(&m.ID, &gbID, &m.MemberID, &isSub, &m.Position); err != nil {
			slog.Error("getStormGroups: building members scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.IsSub = isSub == 1
		if ref, ok := bldgMap[gbID]; ok {
			b := &ref.group.Buildings[ref.index]
			b.Members = append(b.Members, m)
		}
	}

	dmRows, err := db.Query(`SELECT sgm.id, sgm.group_id, sgm.member_id, sgm.is_sub, sgm.position FROM storm_group_members sgm JOIN storm_groups sg ON sg.id=sgm.group_id WHERE sg.task_force=? ORDER BY sgm.position`, tf)
	if err != nil {
		slog.Error("getStormGroups: direct members query failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer dmRows.Close()
	for dmRows.Next() {
		var m StormGroupMember
		var groupID, isSub int
		if err := dmRows.Scan(&m.ID, &groupID, &m.MemberID, &isSub, &m.Position); err != nil {
			slog.Error("getStormGroups: direct members scan failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		m.IsSub = isSub == 1
		if g, ok := groupMap[groupID]; ok {
			g.DirectMembers = append(g.DirectMembers, m)
		}
	}

	result := make([]StormGroup, 0, len(groups))
	for _, g := range groups {
		result = append(result, *g)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func createStormGroup(w http.ResponseWriter, r *http.Request) {
	var g StormGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if g.TaskForce != "A" && g.TaskForce != "B" {
		http.Error(w, "invalid task_force", 400)
		return
	}
	res, err := db.Exec(`INSERT INTO storm_groups (task_force,name,instructions,sort_order) VALUES (?,?,?,?)`, g.TaskForce, g.Name, g.Instructions, g.SortOrder)
	if err != nil {
		slog.Error("createStormGroup: insert failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	g.ID = int(id)
	g.Buildings = []StormGroupBuilding{}
	g.DirectMembers = []StormGroupMember{}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "created", "storm_group", g.Name, false, "TF "+g.TaskForce)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(g)
}

func updateStormGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var g StormGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var oldName, oldTF string
	db.QueryRow(`SELECT name, task_force FROM storm_groups WHERE id=?`, id).Scan(&oldName, &oldTF)

	_, err := db.Exec(`UPDATE storm_groups SET name=?,instructions=?,sort_order=? WHERE id=?`, g.Name, g.Instructions, g.SortOrder, id)
	if err != nil {
		slog.Error("updateStormGroup: update failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	details := "TF " + oldTF
	if oldName != g.Name {
		details += "; name: " + oldName + " → " + g.Name
	}
	logActivity(actorID, actorName, "updated", "storm_group", g.Name, false, details)

	w.WriteHeader(http.StatusNoContent)
}

func deleteStormGroup(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var groupName, tf string
	db.QueryRow(`SELECT name, task_force FROM storm_groups WHERE id=?`, id).Scan(&groupName, &tf)

	_, err := db.Exec(`DELETE FROM storm_groups WHERE id=?`, id)
	if err != nil {
		slog.Error("deleteStormGroup: delete failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "deleted", "storm_group", groupName, false, "TF "+tf)

	w.WriteHeader(http.StatusNoContent)
}

func saveGroupBuildings(w http.ResponseWriter, r *http.Request) {
	groupID := mux.Vars(r)["id"]
	var buildings []StormGroupBuilding
	if err := json.NewDecoder(r.Body).Decode(&buildings); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Determine task force for this group
	var tf string
	if err := db.QueryRow(`SELECT task_force FROM storm_groups WHERE id=?`, groupID).Scan(&tf); err != nil {
		http.Error(w, "group not found", 404)
		return
	}

	// Check for duplicates against other groups
	assigned, err := getStormAssignedMemberIDs(tf)
	if err != nil {
		slog.Error("saveGroupBuildings: read assigned members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// Remove current group's own members from the conflict check
	curRows, err := db.Query(`SELECT sgbm.member_id FROM storm_group_building_members sgbm JOIN storm_group_buildings sgb ON sgb.id=sgbm.group_building_id WHERE sgb.group_id=?`, groupID)
	if err != nil {
		slog.Error("saveGroupBuildings: read building members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer curRows.Close()
	ownIDs := map[int]bool{}
	for curRows.Next() {
		var id int
		curRows.Scan(&id)
		ownIDs[id] = true
	}
	curRows2, err := db.Query(`SELECT member_id FROM storm_group_members WHERE group_id=?`, groupID)
	if err != nil {
		slog.Error("saveGroupBuildings: read direct members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer curRows2.Close()
	for curRows2.Next() {
		var id int
		curRows2.Scan(&id)
		ownIDs[id] = true
	}
	for id := range ownIDs {
		delete(assigned, id)
	}

	for _, b := range buildings {
		for _, m := range b.Members {
			if assigned[m.MemberID] {
				http.Error(w, fmt.Sprintf("member %d already assigned elsewhere", m.MemberID), 409)
				return
			}
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("saveGroupBuildings: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM storm_group_building_members WHERE group_building_id IN (SELECT id FROM storm_group_buildings WHERE group_id=?)`, groupID); err != nil {
		slog.Error("saveGroupBuildings: clear building members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`DELETE FROM storm_group_buildings WHERE group_id=?`, groupID); err != nil {
		slog.Error("saveGroupBuildings: clear buildings failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for _, b := range buildings {
		res, err := tx.Exec(`INSERT INTO storm_group_buildings (group_id,building_id,sort_order) VALUES (?,?,?)`, groupID, b.BuildingID, b.SortOrder)
		if err != nil {
			slog.Error("saveGroupBuildings: insert failed", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		gbID, _ := res.LastInsertId()
		for _, m := range b.Members {
			isSub := 0
			if m.IsSub {
				isSub = 1
			}
			if _, err := tx.Exec(`INSERT INTO storm_group_building_members (group_building_id,member_id,is_sub,position) VALUES (?,?,?,?)`, gbID, m.MemberID, isSub, m.Position); err != nil {
				slog.Error("saveGroupBuildings: insert building member failed", "member_id", m.MemberID, "error", err)
				http.Error(w, "Database error", http.StatusInternalServerError)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("saveGroupBuildings: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	// Collect all member IDs across all buildings for the log detail
	var memberIDs []int
	for _, b := range buildings {
		for _, m := range b.Members {
			memberIDs = append(memberIDs, m.MemberID)
		}
	}
	var groupName string
	db.QueryRow(`SELECT name FROM storm_groups WHERE id=?`, groupID).Scan(&groupName)

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "updated", "storm_group", groupName, false,
		"TF "+tf+"; "+strconv.Itoa(len(memberIDs))+" members across "+strconv.Itoa(len(buildings))+" buildings")

	w.WriteHeader(http.StatusNoContent)
}

func saveGroupDirectMembers(w http.ResponseWriter, r *http.Request) {
	groupID := mux.Vars(r)["id"]
	var members []StormGroupMember
	if err := json.NewDecoder(r.Body).Decode(&members); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var tf string
	if err := db.QueryRow(`SELECT task_force FROM storm_groups WHERE id=?`, groupID).Scan(&tf); err != nil {
		http.Error(w, "group not found", 404)
		return
	}

	assigned, err := getStormAssignedMemberIDs(tf)
	if err != nil {
		slog.Error("saveGroupDirectMembers: read assigned members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	curRows, err := db.Query(`SELECT member_id FROM storm_group_members WHERE group_id=?`, groupID)
	if err != nil {
		slog.Error("saveGroupDirectMembers: read direct members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer curRows.Close()
	for curRows.Next() {
		var id int
		curRows.Scan(&id)
		delete(assigned, id)
	}
	bldgRows, err := db.Query(`SELECT sgbm.member_id FROM storm_group_building_members sgbm JOIN storm_group_buildings sgb ON sgb.id=sgbm.group_building_id WHERE sgb.group_id=?`, groupID)
	if err != nil {
		slog.Error("saveGroupDirectMembers: read building members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer bldgRows.Close()
	for bldgRows.Next() {
		var id int
		bldgRows.Scan(&id)
		delete(assigned, id)
	}

	for _, m := range members {
		if assigned[m.MemberID] {
			http.Error(w, fmt.Sprintf("member %d already assigned elsewhere", m.MemberID), 409)
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("saveGroupDirectMembers: begin failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM storm_group_members WHERE group_id=?`, groupID); err != nil {
		slog.Error("saveGroupDirectMembers: clear direct members failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	for _, m := range members {
		isSub := 0
		if m.IsSub {
			isSub = 1
		}
		if _, err := tx.Exec(`INSERT INTO storm_group_members (group_id,member_id,is_sub,position) VALUES (?,?,?,?)`, groupID, m.MemberID, isSub, m.Position); err != nil {
			slog.Error("saveGroupDirectMembers: insert direct member failed", "member_id", m.MemberID, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Error("saveGroupDirectMembers: commit failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	var groupName string
	db.QueryRow(`SELECT name FROM storm_groups WHERE id=?`, groupID).Scan(&groupName)

	actor := getAuthUser(r)
	actorID, actorName := actor.ID, actor.Username
	logActivity(actorID, actorName, "updated", "storm_group", groupName, false,
		"TF "+tf+"; "+strconv.Itoa(len(members))+" direct members")

	w.WriteHeader(http.StatusNoContent)
}
