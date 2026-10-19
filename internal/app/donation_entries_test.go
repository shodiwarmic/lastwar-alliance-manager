package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)


// An entered day is exact and replaces only its own day; the total stays the week reading,
// unless every day is entered (290).
func TestDeriveDonationWeekWithEnteredDays(t *testing.T) {
	over := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	days := workedDonationWeek()
	days[2].Entered = i64(1500) // Wednesday was blank
	days[1].Entered = i64(700)  // Tuesday was so far at 500
	w := deriveDonationWeek("2026-10-12", days, over)
	want := []string{"exact:2500", "exact:700", "exact:1500", "exact:900", "exact:1000", "exact:900", "so_far:400"}
	for d, wd := range want {
		if got := dayString(w.Days[d]); got != wd {
			t.Errorf("day %d = %s, want %s", d, got, wd)
		}
	}
	if !w.Days[2].Entered || w.Days[3].Entered || w.Total != (donationWeekTotal{6400, donationFinal}) {
		t.Errorf("entered flags / total: %+v %+v", w.Days, w.Total)
	}

	var all [7]donationDayReadings
	for d := range all {
		all[d].Entered = i64(int64(1000 * (d + 1)))
	}
	if w := deriveDonationWeek("2026-10-12", all, over); w.Total != (donationWeekTotal{28000, donationFinal}) || w.LastReadDay != 6 {
		t.Errorf("all entered: %+v last %d", w.Total, w.LastReadDay)
	}
	if w := deriveDonationWeek("2026-10-12", all, time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)); w.Total.Status != donationSoFar {
		t.Errorf("all entered, week running: %+v", w.Total)
	}
}

func TestSaveDonationEntries(t *testing.T) {
	setupSettingsTestDB(t)
	a := seedMember(t, "Alder", "R3")
	gone := seedMember(t, "Gone", "EX")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	save := func(cells []donationEntryCell, source string, check bool) donationEntryResult {
		t.Helper()
		tx, _ := db.Begin()
		defer tx.Rollback()
		res, err := saveDonationEntriesTx(tx, cells, source, check, 1, now)
		if err != nil {
			t.Fatal(err)
		}
		tx.Commit()
		return res
	}
	stored := func() (int64, string, bool) {
		var v int64
		var s string
		err := db.QueryRow(`SELECT amount, source FROM donation_day_entries WHERE member_id = ? AND game_date = '2026-10-13'`, a).Scan(&v, &s)
		return v, s, err == nil
	}

	r := save([]donationEntryCell{
		{MemberID: a, GameDate: "2026-10-13", Amount: i64(900)},
		{MemberID: a, GameDate: "2026-10-15", Amount: i64(1)}, // after today
		{MemberID: a, GameDate: "2026-10-12", Amount: i64(-5)},
		{MemberID: gone, GameDate: "2026-10-12", Amount: i64(5)}, // a former member's day is still history
		{MemberID: 999999, GameDate: "2026-10-12", Amount: i64(5)},
		{MemberID: a, GameDate: "13/10/2026", Amount: i64(5)},
	}, "manual", true)
	if r.Saved != 2 || len(r.Errors) != 4 {
		t.Errorf("first save %+v", r)
	}
	// The same value again: unchanged. A stale `was`: a conflict, nothing written.
	if r := save([]donationEntryCell{{MemberID: a, GameDate: "2026-10-13", Amount: i64(900), Was: nil}}, "manual", true); r.Unchanged != 1 {
		t.Errorf("repeat %+v", r)
	}
	// A stale `was` beside a good cell: a conflict, and the save writes nothing at all (the VS
	// page's rule, 209) — the good cell waits for the officer's choice too.
	r = save([]donationEntryCell{
		{MemberID: a, GameDate: "2026-10-13", Amount: i64(950), Was: nil},
		{MemberID: a, GameDate: "2026-10-12", Amount: i64(10), Was: nil},
	}, "manual", true)
	if len(r.Conflicts) != 1 || r.Saved != 0 || r.Conflicts[0].Name != "Alder" || *r.Conflicts[0].Current != 900 || r.Conflicts[0].Loaded != nil || *r.Conflicts[0].Typed != 950 {
		t.Errorf("stale was %+v", r)
	}
	if v, _, _ := stored(); v != 900 {
		t.Errorf("a conflict overwrote: %d", v)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM donation_day_entries WHERE member_id = ? AND game_date = '2026-10-12'`, a); n != 0 {
		t.Errorf("the good cell of a conflicting save was written")
	}
	// "Keep mine": resubmitted against the value the review showed — saved and counted.
	if r := save([]donationEntryCell{{MemberID: a, GameDate: "2026-10-13", Amount: i64(950), Was: i64(900), Overridden: true}}, "manual", true); r.Saved != 1 || r.Overridden != 1 {
		t.Errorf("keep mine %+v", r)
	}
	// A CSV replaces whatever the page loaded.
	if r := save([]donationEntryCell{{MemberID: a, GameDate: "2026-10-13", Amount: i64(1200)}}, "csv", false); r.Saved != 1 {
		t.Errorf("csv %+v", r)
	}
	if v, s, _ := stored(); v != 1200 || s != "csv" {
		t.Errorf("after csv: %d %s", v, s)
	}
	// Clearing deletes the row.
	if r := save([]donationEntryCell{{MemberID: a, GameDate: "2026-10-13", Amount: nil, Was: i64(1200)}}, "manual", true); r.Cleared != 1 {
		t.Errorf("clear %+v", r)
	}
	if _, _, ok := stored(); ok {
		t.Error("cleared entry still stored")
	}

	// Entries reach every reader: a member with entries only, and the weeks list.
	save([]donationEntryCell{{MemberID: a, GameDate: "2026-09-30", Amount: i64(400)}}, "manual", true)
	weeks, err := loadDonationWeeks(db, "2026-09-28", "2026-09-28", now)
	if err != nil || !weeks[a]["2026-09-28"].Days[2].Entered {
		t.Errorf("loaded %+v %v", weeks[a], err)
	}
	if avail, _ := donationWeeksAvailable(db); len(avail) == 0 || avail[len(avail)-1] != "2026-09-28" {
		t.Errorf("weeks available %v", avail)
	}

	// Member delete removes them.
	tx, _ := db.Begin()
	if err := deleteMemberTx(tx, a); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if n := countRows(t, `SELECT COUNT(*) FROM donation_day_entries WHERE member_id = ?`, a); n != 0 {
		t.Errorf("%d entries left after member delete", n)
	}
}

func csvRecords(t *testing.T, s string) [][]string {
	t.Helper()
	r, err := csv.NewReader(strings.NewReader(s)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseDonationCSV(t *testing.T) {
	rows, errs, err := parseDonationCSV(csvRecords(t, "Member,Date,Amount\nAlder,2026-10-13,\"1,250\"\nAlder,2026-10-12,900\nBirch,2026-10-13,\nBirch,bad,5\nCedar,2026-10-14,lots\n"), "")
	if err != nil || len(rows) != 1 || len(rows[0].Cells) != 2 || rows[0].Cells[0].GameDate != "2026-10-12" || *rows[0].Cells[1].Amount != 1250 || len(errs) != 2 {
		t.Errorf("long: %+v %v %v", rows, errs, err)
	}
	// The Donations tab's export: Commander, Mon…Sun, Week, Week status; blanks skipped.
	export := "Commander,Mon,Tue,Wed,Thu,Fri,Sat,Sun,Week,Week status\nAlder,2500,500,,900,1000,900,400,6400,final\n"
	rows, _, err = parseDonationCSV(csvRecords(t, export), "2026-10-15")
	if err != nil || len(rows) != 1 || len(rows[0].Cells) != 6 || rows[0].Cells[0].GameDate != "2026-10-12" || rows[0].Cells[5].GameDate != "2026-10-18" {
		t.Errorf("wide: %+v %v", rows, err)
	}
	if _, _, err := parseDonationCSV(csvRecords(t, export), ""); err == nil {
		t.Error("wide without a week was accepted")
	}
	if _, _, err := parseDonationCSV(csvRecords(t, "Who,When\nA,B\n"), ""); err == nil {
		t.Error("no member column was accepted")
	}
}

// The two routes: manage_members, an activity row, and a preview that resolves names and
// saves nothing.
func TestDonationEntryRoutes(t *testing.T) {
	setupPreviewTestDB(t) // admin 1; officer 3 is R3
	a := seedMember(t, "Alder", "R3")
	gone := seedMember(t, "Gone Player", "EX")
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_members', json('false')) WHERE rank = 'R3'`)
	today := time.Now().UTC().Add(-2 * time.Hour).Format("2006-01-02")
	body := `{"source":"manual","cells":[{"member_id":` + itoaTest(a) + `,"game_date":"` + today + `","amount":800}]}`
	if w := serveRouter(t, "PUT", "/api/donations/entries", body, sessionCookie(t, 3, "")); w.Code != http.StatusForbidden {
		t.Errorf("without manage_members: %d", w.Code)
	}
	w := serveRouter(t, "PUT", "/api/donations/entries", body, sessionCookie(t, 1, ""))
	var res donationEntryResult
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Saved != 1 {
		t.Fatalf("admin save: %d %s", w.Code, w.Body)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'donations' AND action = 'updated' AND details = '1 day entered, 0 cleared · by hand'`); n != 1 {
		t.Errorf("activity rows %d", n)
	}
	// Someone else changes it; a save from the stale page is 409 with the conflict.
	stale := `{"source":"manual","cells":[{"member_id":` + itoaTest(a) + `,"game_date":"` + today + `","amount":500,"was":null}]}`
	w = serveRouter(t, "PUT", "/api/donations/entries", stale, sessionCookie(t, 1, ""))
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != http.StatusConflict || len(res.Conflicts) != 1 || *res.Conflicts[0].Current != 800 {
		t.Errorf("stale save: %d %s", w.Code, w.Body)
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("csv_file", "d.csv")
	part.Write([]byte("Member,Date,Amount\nAlder,2026-10-13,500\nNobody Here,2026-10-13,5\nGone Player,2026-10-13,7\n"))
	mw.Close()
	req := httptest.NewRequest("POST", "/api/donations/import/preview", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(sessionCookie(t, 1, ""))
	rec := httptest.NewRecorder()
	buildRouter().ServeHTTP(rec, req)
	var prev struct {
		Rows []donationImportPreviewRow `json:"rows"`
	}
	json.Unmarshal(rec.Body.Bytes(), &prev)
	if rec.Code != 200 || len(prev.Rows) != 3 || prev.Rows[0].MatchedMember == nil || prev.Rows[0].Cells[0].MemberID != a || prev.Rows[1].MatchedMember != nil ||
		prev.Rows[2].MatchedMember == nil || prev.Rows[2].Cells[0].MemberID != gone {
		t.Errorf("preview: %d %s", rec.Code, rec.Body)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM donation_day_entries WHERE game_date = '2026-10-13'`); n != 0 {
		t.Errorf("the preview saved %d rows", n)
	}
}
