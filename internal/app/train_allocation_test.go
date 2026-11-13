package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func readTrainAllocation(t *testing.T) []TrainAllocation {
	t.Helper()
	w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", sessionCookie(t, 1, ""))
	if w.Code != http.StatusOK {
		t.Fatalf("allocation = %d %s", w.Code, w.Body.String())
	}
	var rows []TrainAllocation
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if rows == nil {
		t.Fatal("allocation must be an array, including when empty")
	}
	return rows
}

func TestTrainAllocationMetricsAndNoShows(t *testing.T) {
	f := setupMobileTestDB(t)
	alice, bob := f.r4Member, f.r3Member
	noShow := seedMember(t, "No Show Only", "R2")
	guestOnly := seedMember(t, "Guest Only", "R1")
	add := func(date, typ string, conductor int, vip any, vipType any, showed int) int {
		t.Helper()
		res, err := db.Exec(`INSERT INTO train_logs (date, train_type, conductor_id, vip_id, vip_type, showed_up, created_by)
            VALUES (?, ?, ?, ?, ?, ?, 1)`, date, typ, conductor, vip, vipType, showed)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return int(id)
	}
	add("2026-09-01", "FREE", alice, bob, "SPECIAL_GUEST", 1)
	add("2026-09-03", "FREE", alice, bob, "GUARDIAN_DEFENDER", 1)
	add("2026-09-05", "PURCHASED", alice, guestOnly, "SPECIAL_GUEST", 1)
	missedLog := add("2026-09-10", "FREE", alice, noShow, "GUARDIAN_DEFENDER", 1)
	add("2026-09-06", "PURCHASED", bob, alice, "SPECIAL_GUEST", 1)
	add("2026-09-07", "FREE", f.former, alice, "GUARDIAN_DEFENDER", 1)
	add("2026-09-08", "FREE", noShow, bob, "SPECIAL_GUEST", 0)
	// Future rows from older imports must not count as completed history.
	add("2099-01-01", "FREE", alice, bob, "SPECIAL_GUEST", 1)
	admin := sessionCookie(t, 1, "")
	w := serveRouter(t, http.MethodPut, "/api/train-logs/"+strconv.Itoa(missedLog)+"/showed-up", `{"showed_up":0}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("mark no-show = %d %s", w.Code, w.Body.String())
	}
	// The overview uses current roster details and includes ineligible members.
	if _, err := db.Exec(`UPDATE members SET name = 'Alice renamed', rank = 'R5', eligible = 0 WHERE id = ?`, alice); err != nil {
		t.Fatal(err)
	}
	logsBefore := serveRouter(t, http.MethodGet, "/api/train-logs", "", admin).Body.String()
	profilePath := "/api/accountability/members/" + strconv.Itoa(alice)
	profileBefore := serveRouter(t, http.MethodGet, profilePath, "", admin)
	if profileBefore.Code != http.StatusOK {
		t.Fatalf("profile = %d %s", profileBefore.Code, profileBefore.Body.String())
	}
	var profile MemberProfileData
	if err := json.Unmarshal(profileBefore.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range profile.TrainHistory {
		if row.ID == missedLog {
			found = true
			if row.ShowedUp {
				t.Fatal("no-show lost from accountability")
			}
		}
	}
	if !found || profile.ActiveStrikes != 1 {
		t.Fatalf("no-show accountability = %+v", profile)
	}
	activityBefore := countRows(t, `SELECT COUNT(*) FROM activity_log`)

	rows := readTrainAllocation(t)
	if len(rows) != 5 {
		t.Fatalf("active roster = %d rows, want 5: %+v", len(rows), rows)
	}
	byID := map[int]TrainAllocation{}
	for _, row := range rows {
		byID[row.MemberID] = row
	}
	if _, ok := byID[f.former]; ok {
		t.Fatal("former member included")
	}
	if len(byID) != len(rows) {
		t.Fatal("duplicated roster members")
	}
	a := byID[alice]
	if a.Name != "Alice renamed" || a.Rank != "R5" || a.Trains != 3 || a.FreeTrains != 2 || a.LastTrain == nil || *a.LastTrain != "2026-09-05" || a.GuestSpots != 2 || a.LastGuest == nil || *a.LastGuest != "2026-09-07" {
		t.Fatalf("Alice's three attended trains + one no-show = %+v", a)
	}
	b := byID[bob]
	if b.Trains != 1 || b.FreeTrains != 0 || b.LastTrain == nil || *b.LastTrain != "2026-09-06" || b.GuestSpots != 3 || b.LastGuest == nil || *b.LastGuest != "2026-09-08" {
		t.Fatalf("Bob = %+v", b)
	}
	n := byID[noShow]
	if n.Trains != 0 || n.FreeTrains != 0 || n.LastTrain != nil || n.GuestSpots != 1 || n.LastGuest == nil || *n.LastGuest != "2026-09-10" {
		t.Fatalf("no-show-only conductor and recorded guest = %+v", n)
	}
	g := byID[guestOnly]
	if g.Trains != 0 || g.LastTrain != nil || g.GuestSpots != 1 || g.LastGuest == nil {
		t.Fatalf("guest-only member = %+v", g)
	}
	never := byID[f.other]
	if never.MemberID != f.other || never.Trains != 0 || never.FreeTrains != 0 || never.GuestSpots != 0 || never.LastTrain != nil || never.LastGuest != nil {
		t.Fatalf("never allocated = %+v", never)
	}
	// Reading allocation must leave all historic records and accountability intact.
	if after := serveRouter(t, http.MethodGet, "/api/train-logs", "", admin).Body.String(); after != logsBefore {
		t.Fatal("overview changed normal train history")
	}
	if after := serveRouter(t, http.MethodGet, profilePath, "", admin).Body.String(); after != profileBefore.Body.String() {
		t.Fatal("overview changed accountability")
	}
	if countRows(t, `SELECT COUNT(*) FROM train_logs`) != 8 || countRows(t, `SELECT COUNT(*) FROM train_logs WHERE conductor_id = ?`, f.former) != 1 || countRows(t, `SELECT COUNT(*) FROM activity_log`) != activityBefore {
		t.Fatal("overview wrote history/activity")
	}
}

func TestTrainAllocationEmptyHistoryAndRoster(t *testing.T) {
	f := setupMobileTestDB(t)
	rows := readTrainAllocation(t)
	if len(rows) != 3 {
		t.Fatalf("unallocated roster = %+v", rows)
	}
	for _, a := range rows {
		if a.LastTrain != nil || a.LastGuest != nil || a.Trains != 0 || a.FreeTrains != 0 || a.GuestSpots != 0 {
			t.Fatalf("empty history = %+v", a)
		}
	}
	// An optional VIP is not required for conductor credit; today's log is history.
	if _, err := db.Exec(`INSERT INTO train_logs (date, train_type, conductor_id, created_by) VALUES (?, 'FREE', ?, 1)`, gameDate(), f.r4Member); err != nil {
		t.Fatal(err)
	}
	for _, a := range readTrainAllocation(t) {
		if a.MemberID == f.r4Member && (a.Trains != 1 || a.FreeTrains != 1 || a.LastTrain == nil || *a.LastTrain != gameDate() || a.GuestSpots != 0) {
			t.Fatalf("today's train without VIP = %+v", a)
		}
	}
	if _, err := db.Exec(`UPDATE members SET rank = 'EX'`); err != nil {
		t.Fatal(err)
	}
	if rows := readTrainAllocation(t); len(rows) != 0 {
		t.Fatalf("empty active roster = %+v", rows)
	}
}

func TestTrainAllocationPermissionsAndReadOnlyRoute(t *testing.T) {
	setupMobileTestDB(t)
	setRankPerm(t, "R3", "view_train", true)
	setRankPerm(t, "R3", "manage_train", false)
	setRankPerm(t, "R4", "view_train", true)
	setRankPerm(t, "R4", "manage_train", true)
	viewer, officer := sessionCookie(t, 3, ""), sessionCookie(t, 2, "")
	if w := serveRouter(t, http.MethodGet, "/api/train-logs", "", viewer); w.Code != http.StatusOK {
		t.Fatalf("viewer history = %d", w.Code)
	}
	if w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", viewer); w.Code != http.StatusForbidden {
		t.Fatalf("viewer allocation = %d", w.Code)
	}
	if w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated allocation = %d", w.Code)
	}
	if w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", officer); w.Code != http.StatusOK {
		t.Fatalf("officer allocation = %d", w.Code)
	}
	// Admin preview must respect the selected rank's permissions.
	if w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", sessionCookie(t, 1, "R3")); w.Code != http.StatusForbidden {
		t.Fatalf("admin preview = %d", w.Code)
	}
	// Management alone grants the API; view_train is a separate permission.
	setRankPerm(t, "R4", "view_train", false)
	if w := serveRouter(t, http.MethodGet, "/api/train-allocation", "", officer); w.Code != http.StatusOK {
		t.Fatalf("manage-only allocation = %d", w.Code)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		if w := serveRouter(t, method, "/api/train-allocation", `{}`, officer); w.Code != http.StatusMethodNotAllowed && w.Code != http.StatusNotFound {
			t.Errorf("%s = %d", method, w.Code)
		}
	}
}

func TestTrainAllocationTabPermission(t *testing.T) {
	for _, manage := range []bool{false, true} {
		tmpl, err := parseTemplates("templates/layout.html", "templates/train.html")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "layout.html", PageData{IsAuthenticated: true, Permissions: RankPermissions{ViewTrain: true, ManageTrain: manage}}); err != nil {
			t.Fatal(err)
		}
		html := buf.String()
		for _, marker := range []string{`data-tab="allocation"`, `id="tab-allocation"`, `id="allocation-table"`} {
			if strings.Contains(html, marker) != manage {
				t.Errorf("manage=%t: unexpected visibility for %s", manage, marker)
			}
		}
		if !strings.Contains(html, `data-tab="logs"`) {
			t.Fatal("normal history tab missing")
		}
	}
}
