package app

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The commit's category table (Project 15, C6): each category writes its table under its
// own web page's permission, weekly derives Saturday by the web's rule, and HQ never
// goes down.

func commitRecords(t *testing.T, tok string, recs ...string) map[string]any {
	t.Helper()
	return decodeObject(t, serveMobile(t, "POST", "/api/mobile/commit",
		`{"week_date":"2026-10-05","records":[`+strings.Join(recs, ",")+`],"save_aliases":[]}`, tok))
}

func rec(memberID int, category string, score int) string {
	return `{"member_id":` + strconv.Itoa(memberID) + `,"original_name":"M` + strconv.Itoa(memberID) +
		`","category":"` + category + `","score":` + strconv.Itoa(score) + `}`
}

func TestMobileCommitCategoriesWriteTheirTables(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	m := f.other
	out := commitRecords(t, tok, rec(m, "hero_power", 300), rec(m, "squad_power", 200), rec(m, "hq_level", 25),
		rec(m, "profession_level", 40), rec(m, "power", 900), rec(m, "kills", 5))
	if errs := out["errors"].([]any); len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}
	for table, col := range map[string]string{"hero_power_history": "power", "squad_power_history": "power",
		"hq_level_history": "hq_level", "profession_level_history": "profession_level"} {
		if n := countRows(t, `SELECT COUNT(*) FROM `+table+` WHERE member_id = ? AND source = 'mobile' AND `+col+` > 0`, m); n != 1 {
			t.Errorf("%s rows = %d", table, n)
		}
	}
	var det string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'power_records'`).Scan(&det)
	if det != "power 1, hero power 1, squad power 1, hq level 1, profession level 1 records · via mobile" {
		t.Errorf("power_records details = %q", det)
	}

	// HQ can't go down: refused, naming the stored value.
	out = commitRecords(t, tok, rec(m, "hq_level", 24))
	if errs := out["errors"].([]any); len(errs) != 1 || !strings.Contains(errs[0].(string), "stored 25") {
		t.Errorf("lower HQ = %v", out)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM hq_level_history WHERE member_id = ?`, m); n != 1 {
		t.Errorf("lower HQ wrote a row")
	}
}

func TestMobileWeeklyDerivesSaturday(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2)
	a, b, c := f.other, f.r3Member, f.r4Member
	// a: Mon–Wed stored, Thu–Fri uploaded with the weekly total.
	db.Exec(`INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday) VALUES (?, '2026-10-05', 10, 20, 30)`, a)
	// b: every day uploaded. c: Friday missing → refused.
	out := commitRecords(t, tok,
		rec(a, "thursday", 40), rec(a, "friday", 50), rec(a, "weekly", 1000),
		rec(b, "monday", 1), rec(b, "tuesday", 1), rec(b, "wednesday", 1), rec(b, "thursday", 1), rec(b, "friday", 1), rec(b, "weekly", 105),
		rec(c, "monday", 1), rec(c, "weekly", 50))
	if out["saturday_derived"] != float64(2) {
		t.Errorf("saturday_derived = %v (%v)", out["saturday_derived"], out["errors"])
	}
	errs := out["errors"].([]any)
	if len(errs) != 1 || !strings.Contains(errs[0].(string), "M"+strconv.Itoa(c)) || !strings.Contains(errs[0].(string), "incomplete") {
		t.Errorf("errors = %v, want member %d's incomplete week named", errs, c)
	}
	sat := func(id int) int {
		var s int
		db.QueryRow(`SELECT saturday FROM vs_points WHERE member_id = ? AND week_date = '2026-10-05'`, id).Scan(&s)
		return s
	}
	if sat(a) != 850 || sat(b) != 100 || sat(c) != 0 {
		t.Errorf("saturdays = %d %d %d, want 850 100 0", sat(a), sat(b), sat(c))
	}

	// A total below Mon–Fri is refused.
	out = commitRecords(t, tok, rec(a, "weekly", 10))
	if errs := out["errors"].([]any); len(errs) != 1 || !strings.Contains(errs[0].(string), "less than") {
		t.Errorf("small total = %v", out)
	}
}

// Days follow the VS page (manage_vs_points); member stats follow the roster
// (manage_members). A denied record is reported and the rest saves.
func TestMobilePerRecordPermissions(t *testing.T) {
	f := setupMobileTestDB(t)
	m := f.other

	setRankPerm(t, "R4", "manage_members", false) // VS only
	out := commitRecords(t, mobileToken(t, 2), rec(m, "monday", 7), rec(m, "power", 900))
	if out["vs_records_saved"] != float64(1) || out["power_records_saved"] != float64(0) || len(out["errors"].([]any)) != 1 {
		t.Errorf("VS-only = %v", out)
	}
	caps := capabilities(t, mobileToken(t, 2))["commit_categories"].(map[string]any)
	if caps["monday"] != true || caps["weekly"] != true || caps["power"] != false || caps["hq_level"] != false {
		t.Errorf("VS-only categories = %v", caps)
	}

	setRankPerm(t, "R4", "manage_members", true)
	setRankPerm(t, "R4", "manage_vs_points", false) // members only
	out = commitRecords(t, mobileToken(t, 2), rec(m, "tuesday", 7), rec(m, "power", 950))
	if out["power_records_saved"] != float64(1) || len(out["errors"].([]any)) != 1 {
		t.Errorf("members-only = %v", out)
	}
	var tue int
	db.QueryRow(`SELECT tuesday FROM vs_points WHERE member_id = ?`, m).Scan(&tue)
	if tue != 0 {
		t.Error("a members-only user saved a VS day")
	}

	// R3 holds neither: the route refuses.
	if w := serveMobile(t, "POST", "/api/mobile/commit", `{"week_date":"2026-10-05","records":[]}`, mobileToken(t, 3)); w.Code != 403 {
		t.Errorf("R3: %d", w.Code)
	}
}

// A mixed batch carrying a global alias, from a non-admin holding both permissions,
// finishes under a short statement ceiling: every permission is resolved before the
// transaction (F1).
func TestMobileMixedBatchNoDeadlock(t *testing.T) {
	f := setupMobileTestDB(t)
	withCeiling(t, 2*time.Second)
	start := time.Now()
	w := serveMobile(t, "POST", "/api/mobile/commit", `{"week_date":"2026-10-05","records":[`+
		rec(f.other, "monday", 1)+","+rec(f.other, "weekly", 9)+","+rec(f.other, "power", 5)+","+rec(f.other, "hq_level", 3)+
		`],"save_aliases":[{"failed_alias":"Mx","member_id":`+strconv.Itoa(f.other)+`,"category":"global"}]}`, mobileToken(t, 2))
	if w.Code != 200 || time.Since(start) > time.Second {
		t.Errorf("mixed batch: %d after %v: %s", w.Code, time.Since(start), w.Body.String())
	}
}
