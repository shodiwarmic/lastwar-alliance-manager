package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"lastwar-alliance/internal/demo"
	"lastwar-alliance/internal/gametime"
)

// setupDemoSeedTestDB migrates a fresh database the app's way and seeds the demo alliance
// into it through the app's own handle — the shape the server uses at boot in demo mode.
func setupDemoSeedTestDB(t *testing.T) {
	t.Helper()
	setupNameMatchTestDB(t)
	_, err := demo.Seed(db.DB, demo.Options{
		UploadsDir: filepath.Join(t.TempDir(), "up"),
		Today:      time.Date(2026, 10, 5, 0, 0, 0, 0, gametime.Loc),
		Password:   "test-pass",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// The generator inserts schedule_events directly, so this is what proves it obeys the game
// rules: every generated row must pass the same validator the Schedule page's create and
// edit go through, checked against all the others.
func TestDemoScheduleObeysTheValidator(t *testing.T) {
	setupDemoSeedTestDB(t)
	type ev struct {
		id, typeID int
		date, tm   string
		level      sql.NullInt64
		tf         sql.NullString
	}
	rows, err := db.Query(`SELECT id, event_type_id, event_date, event_time, level, task_force FROM schedule_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var evs []ev
	for rows.Next() {
		var e ev
		rows.Scan(&e.id, &e.typeID, &e.date, &e.tm, &e.level, &e.tf)
		evs = append(evs, e)
	}
	rows.Close()
	if len(evs) < 50 {
		t.Fatalf("only %d schedule events", len(evs))
	}
	for _, e := range evs {
		tr, err := loadScheduleTypeRules(db, e.typeID)
		if err != nil {
			t.Fatal(err)
		}
		c := eventCandidate{Date: e.date, Time: e.tm}
		if e.tf.Valid {
			c.TaskForce = &e.tf.String
		}
		msg, err := validateEventRules(db, tr, c, e.id)
		if err != nil {
			t.Fatal(err)
		}
		if msg != "" {
			t.Errorf("%s on %s %s: %s", tr.Short, e.date, e.tm, msg)
		}
		tl, err := loadTypeLevels(db, e.typeID)
		if err != nil {
			t.Fatal(err)
		}
		var level *int
		if e.level.Valid {
			l := int(e.level.Int64)
			level = &l
		}
		if tl.HasLevel && level == nil {
			t.Errorf("%s on %s has no level", tr.Short, e.date)
		}
		if msg := validateEventLevel(tr.Name, tl, level); msg != "" {
			t.Errorf("%s on %s: %s", tr.Short, e.date, msg)
		}
	}
}

// The generator dates rows through internal/gametime; the app reads them through its own
// wrappers. Re-normalising every week key and parsing every timestamp the app's way proves
// the two agree.
func TestDemoDatesReadBackTheAppsWay(t *testing.T) {
	setupDemoSeedTestDB(t)
	for _, q := range []string{`SELECT DISTINCT week_date FROM vs_points`, `SELECT week_date FROM vs_league_weeks`} {
		rows, _ := db.Query(q)
		var weeks []string
		for rows.Next() {
			var w string
			rows.Scan(&w)
			weeks = append(weeks, w)
		}
		rows.Close()
		for _, w := range weeks {
			if n, err := normalizeToGameWeekMonday(w); err != nil || n != w {
				t.Errorf("%s: week %s normalises to %s (%v)", q, w, n, err)
			}
		}
	}
	for _, q := range []string{
		`SELECT recorded_at FROM power_history`, `SELECT recorded_at FROM hq_level_history`,
		`SELECT created_at FROM activity_log`, `SELECT password_changed_at FROM users`,
		`SELECT created_at FROM accountability_strikes`, `SELECT updated_at FROM participation_boards`,
	} {
		rows, _ := db.Query(q)
		var vals []string
		for rows.Next() {
			var v string
			rows.Scan(&v)
			vals = append(vals, v)
		}
		rows.Close()
		if len(vals) == 0 {
			t.Errorf("%s: no rows", q)
		}
		for _, v := range vals {
			if _, ok := lastRankParseTime(v); !ok {
				t.Errorf("%s: %q does not parse", q, v)
				break
			}
		}
	}
}

func TestDemoRosterServesThroughTheAPI(t *testing.T) {
	setupDemoSeedTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/members", nil)
	w := httptest.NewRecorder()
	getMembers(w, req.WithContext(context.WithValue(req.Context(), authUserKey, &AuthUser{ID: 1, Username: "demo-admin", IsAdmin: true, RealIsAdmin: true})))
	if w.Code != http.StatusOK {
		t.Fatalf("getMembers: %d %s", w.Code, w.Body.String())
	}
	var ms []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ms); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, m := range ms {
		if m["rank"] != "EX" {
			active++
		}
		if m["power"] == nil || m["power"].(float64) <= 0 {
			if m["rank"] != "EX" {
				t.Errorf("%v has no power", m["name"])
			}
		}
	}
	if active != 86 {
		t.Errorf("getMembers returned %d active members, want 86", active)
	}
	// Every seeded account can be resolved by the session path.
	for _, a := range demo.Manifest().Accounts {
		u := loadUserFromDB(a.UserID)
		if u == nil || u.Username != a.Username || (a.Rank != "admin" && u.Rank != a.Rank) {
			t.Errorf("account %s resolves to %+v", a.Username, u)
		}
	}
}
