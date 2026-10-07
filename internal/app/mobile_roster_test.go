package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The mobile roster store (Project 15, C7) and the roster primitives it shares with the
// web and LastRank.

func memberRow(t *testing.T, id int) (name, rank, reason string) {
	t.Helper()
	db.QueryRow(`SELECT name, rank, leave_reason FROM members WHERE id = ?`, id).Scan(&name, &rank, &reason)
	return
}

func rosterChanges(t *testing.T, tok, changes string) (results []mobileRosterResult, applied int) {
	t.Helper()
	w := serveMobile(t, "POST", "/api/mobile/roster/changes", `{"changes":[`+changes+`]}`, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("roster changes: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Results []mobileRosterResult `json:"results"`
		Applied int                  `json:"applied"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Results, out.Applied
}

func TestMobileRosterChanges(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	zed := strconv.Itoa(f.other) // Zed Player, R2
	three := strconv.Itoa(f.r3Member)
	gone := strconv.Itoa(f.former)

	// A batch is validated against itself: promote Zed, rename Zed, then promote again
	// from the rank the first change left.
	res, applied := rosterChanges(t, tok,
		`{"kind":"rank","member_id":`+zed+`,"from":"R2","to":"R3"},
		 {"kind":"rename","member_id":`+zed+`,"from":"Zed Player","to":"Zed Prime"},
		 {"kind":"rank","member_id":`+zed+`,"from":"R3","to":"R4"},
		 {"kind":"rank","member_id":`+three+`,"from":"R2","to":"R4"},
		 {"kind":"rename","member_id":`+three+`,"from":"Member Three","to":"zed prime"},
		 {"kind":"rename","member_id":`+three+`,"from":"Member Three","to":"Gone Player"},
		 {"kind":"join","name":"New Player","rank":"R1"},
		 {"kind":"join","name":"Gone Player","rank":"R1"},
		 {"kind":"join","name":"zed prime","rank":"R1"},
		 {"kind":"join","name":"Nobody","rank":"R9"},
		 {"kind":"rejoin","member_id":`+gone+`,"rank":"R2"},
		 {"kind":"rejoin","member_id":`+three+`,"rank":"R2"},
		 {"kind":"leave","member_id":`+three+`,"from":"R2"},
		 {"kind":"leave","member_id":`+three+`,"from":"R3"},
		 {"kind":"bogus"}`)
	wantApplied := []bool{true, true, true, false, false, false, true, false, false, false, true, false, false, true, false}
	if applied != 6 {
		t.Errorf("applied = %d, want 6", applied)
	}
	for i, r := range res {
		if r.Applied != wantApplied[i] {
			t.Errorf("change %d: applied %v (%q), want %v", i, r.Applied, r.Error, wantApplied[i])
		}
	}
	for i, want := range map[int]string{3: "rank is now R3", 4: "already another member", 5: "former member", 7: "former member Gone Player",
		8: "matches member Zed Prime", 9: "R1–R5", 12: "rank is now R3", 14: "unknown kind"} {
		if !strings.Contains(res[i].Error, want) {
			t.Errorf("change %d error = %q, want %q", i, res[i].Error, want)
		}
	}

	if name, rank, _ := memberRow(t, f.other); name != "Zed Prime" || rank != "R4" {
		t.Errorf("Zed = %s %s", name, rank)
	}
	if got := strings.Join(aliasRowsFor(t, "Zed Player"), ","); got != "global:Zed Prime" {
		t.Errorf("rename alias = %s", got)
	}
	if _, rank, _ := memberRow(t, f.former); rank != "R2" {
		t.Errorf("rejoined rank = %s", rank)
	}
	if _, rank, reason := memberRow(t, f.r3Member); rank != "EX" || reason != "Left alliance (seen in game)" {
		t.Errorf("left = %s %q", rank, reason)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM members WHERE name = 'New Player' AND rank = 'R1'`); n != 1 {
		t.Errorf("joined rows = %d", n)
	}
	for _, a := range []string{"updated", "created", "unarchived", "archived"} {
		if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'member' AND action = ? AND details LIKE '%via mobile'`, a); n == 0 {
			t.Errorf("no %s member row", a)
		}
	}

	// R3 can't reach the route.
	if w := serveMobile(t, "POST", "/api/mobile/roster/changes", `{"changes":[]}`, mobileToken(t, 3)); w.Code != http.StatusForbidden {
		t.Errorf("R3: %d", w.Code)
	}
}

func TestMobileMemberAttributes(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	db.Exec(`UPDATE members SET troop_level = 8, squad_type = 'Tank', profession = 'Engineer' WHERE id = ?`, f.other)
	id := strconv.Itoa(f.other)
	w := serveMobile(t, "POST", "/api/mobile/members/attributes", `{"records":[
		{"member_id":`+id+`,"troop_level":10,"squad_type":"Missile"},
		{"member_id":`+strconv.Itoa(f.r3Member)+`,"profession":"Diplomat"},
		{"member_id":`+strconv.Itoa(f.r4Member)+`},
		{"member_id":`+id+`,"troop_level":9},
		{"member_id":`+id+`,"troop_level":12},
		{"member_id":`+id+`,"squad_type":"Boat"},
		{"member_id":`+id+`,"profession":"Farmer"},
		{"member_id":`+strconv.Itoa(f.former)+`,"troop_level":3}]}`, tok)
	out := decodeObject(t, w)
	if out["updated"] != float64(2) || out["unchanged"] != float64(1) || len(out["errors"].([]any)) != 5 {
		t.Errorf("attributes = %v", out)
	}
	var troop int
	var squad, prof string
	db.QueryRow(`SELECT troop_level, squad_type, profession FROM members WHERE id = ?`, f.other).Scan(&troop, &squad, &prof)
	if troop != 10 || squad != "Missile" || prof != "Engineer" {
		t.Errorf("Zed = %d %s %s, want only the fields sent changed", troop, squad, prof)
	}
	var det string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'member' AND entity_name = 'Zed Player'`).Scan(&det)
	if det != "troop level: 8 → 10; squad: Tank → Missile · via mobile" {
		t.Errorf("details = %q", det)
	}

	// The roster carries the attributes; former members need manage_members.
	w = serveMobile(t, "GET", "/api/mobile/members?include_former=true", "", tok)
	var roster []map[string]any
	json.Unmarshal(w.Body.Bytes(), &roster)
	sawFormer := false
	for _, m := range roster {
		assertKeys(t, "member", m, "troop_level", "squad_type", "profession")
		if m["rank"] == "EX" {
			sawFormer = true
		}
	}
	if !sawFormer {
		t.Error("include_former returned no former member")
	}
	if w := serveMobile(t, "GET", "/api/mobile/members?include_former=true", "", mobileToken(t, 3)); w.Code != http.StatusForbidden {
		t.Errorf("R3 include_former: %d", w.Code)
	}
}

func TestProfessionListsAgree(t *testing.T) {
	for code, label := range CareerTypeLabels {
		if !slices.Contains(ValidProfessions, label) {
			t.Errorf("CareerTypeLabels[%d] = %q is not in ValidProfessions", code, label)
		}
	}
}

// LastRank keeps archiving with its own reason through the shared primitive, on both its
// commit and its review queue.
func TestLastRankArchiveThroughRosterPrimitive(t *testing.T) {
	f := setupMobileTestDB(t)
	admin := sessionCookie(t, 1, "")
	w := serveRouter(t, "POST", "/api/lastrank/commit", `{"archive":[`+strconv.Itoa(f.other)+`]}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", w.Code, w.Body.String())
	}
	if _, rank, reason := memberRow(t, f.other); rank != "EX" || reason != "Left alliance (via LastRank)" {
		t.Errorf("commit archive = %s %q", rank, reason)
	}
	db.Exec(`INSERT INTO lastrank_pending_changes (kind, subject_key, member_id, fingerprint) VALUES ('archive', 'a1', ?, 'fa')`, f.r3Member)
	var pid int
	db.QueryRow(`SELECT id FROM lastrank_pending_changes WHERE subject_key = 'a1'`).Scan(&pid)
	if w := serveRouter(t, "POST", "/api/lastrank/review/action", `{"ids":[`+strconv.Itoa(pid)+`],"action":"apply"}`, admin); w.Code != http.StatusOK {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	if _, rank, reason := memberRow(t, f.r3Member); rank != "EX" || reason != "Left alliance (via LastRank)" {
		t.Errorf("review archive = %s %q", rank, reason)
	}
}

// My Profile's troop clamp reads the moved table and behaves as before.
func TestMyProfileTroopClamp(t *testing.T) {
	f := setupMobileTestDB(t)
	db.Exec(`UPDATE settings SET max_hq_level = 35 WHERE id = 1`)
	officer := sessionCookie(t, 2, "")
	w := serveRouter(t, "PUT", "/api/profile/me", `{"name":"Officer Four","level":15,"troop_level":8}`, officer)
	if w.Code != http.StatusOK {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	var troop int
	db.QueryRow(`SELECT troop_level FROM members WHERE id = ?`, f.r4Member).Scan(&troop)
	if troop != 5 {
		t.Errorf("troop = %d, want 5 (HQ 15 unlocks T5)", troop)
	}
}
