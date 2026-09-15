package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// A comms template the app fetches BY NAME carries a slug. Deleting one leaves
// whatever fetches it with nothing to fetch and no way to put it back through the
// UI — `slug` is seed-only, so a re-created template cannot be given one.
//
// Reproduced before the fix (2026-09-15): DELETE on the seeded ds_battle_mail row
// returned 200, removed it, and the by-slug fetch then 404'd.

func commsDeleteReq(t *testing.T, id int) *http.Request {
	t.Helper()
	req := scheduleTestActor(httptest.NewRequest(http.MethodDelete, "/api/comms/templates/"+strconv.Itoa(id), nil))
	return mux.SetURLVars(req, map[string]string{"id": strconv.Itoa(id)})
}

func countSlugRows(t *testing.T, slug string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comms_templates WHERE slug = ?`, slug).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", slug, err)
	}
	return n
}

func TestSluggedTemplateCannotBeDeleted(t *testing.T) {
	setupSettingsTestDB(t)

	var id int
	if err := db.QueryRow(`SELECT id FROM comms_templates WHERE slug = 'ds_battle_mail'`).Scan(&id); err != nil {
		t.Fatalf("seeded ds_battle_mail row missing: %v", err)
	}

	rr := httptest.NewRecorder()
	handleCommsTemplateDelete(rr, commsDeleteReq(t, id))
	if rr.Code != http.StatusConflict {
		t.Fatalf("delete of a slugged template returned %d, want 409 (body %s)", rr.Code, rr.Body.String())
	}
	if countSlugRows(t, "ds_battle_mail") != 1 {
		t.Error("the slugged row was deleted anyway")
	}
	// The refusal names the slug: the officer's next move is to edit that template,
	// so the message has to say which one the app is protecting.
	if body := rr.Body.String(); !strings.Contains(body, "ds_battle_mail") {
		t.Errorf("refusal does not name the slug: %q", body)
	}

	var logged int
	db.QueryRow(`SELECT COUNT(*) FROM activity_log WHERE entity_type = 'comms_template' AND action = 'deleted'`).Scan(&logged)
	if logged != 0 {
		t.Errorf("a refused delete wrote %d activity rows, want 0", logged)
	}

	// The whole point of refusing: the by-name fetch still works afterwards.
	req := scheduleTestActor(httptest.NewRequest(http.MethodGet, "/api/comms/templates/slug/ds_battle_mail", nil))
	req = mux.SetURLVars(req, map[string]string{"slug": "ds_battle_mail"})
	rr2 := httptest.NewRecorder()
	handleCommsTemplateBySlug(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Errorf("by-slug fetch after the refused delete returned %d, want 200", rr2.Code)
	}
}

func TestUnsluggedTemplateStillDeletes(t *testing.T) {
	setupSettingsTestDB(t)
	res, err := db.Exec(`INSERT INTO comms_templates (type, title, category, content) VALUES ('mail','Scratch','General','x')`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()

	rr := httptest.NewRecorder()
	handleCommsTemplateDelete(rr, commsDeleteReq(t, int(id)))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete of an ordinary template returned %d, want 200 (body %s)", rr.Code, rr.Body.String())
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM comms_templates WHERE id = ?`, id).Scan(&n)
	if n != 0 {
		t.Error("the row survived a permitted delete")
	}
}

// goosePayload strips a migration's goose directives so the body can be replayed
// against an already-migrated test DB. Running the shipped file is the point:
// a test carrying its own copy of the INSERT would pass while the migration rots.
func goosePayload(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return regexp.MustCompile(`(?m)^--.*$`).ReplaceAllString(string(raw), "")
}

// The 409 guard stops the NEXT deletion; an install that already deleted the row
// has nothing to fetch, and once the Storm page's built-in fallback goes that
// button would be dead. The migration restores it, and must be safe to re-run.
func TestMissingSlugTemplateIsReseeded(t *testing.T) {
	setupSettingsTestDB(t)
	if countSlugRows(t, "ds_battle_mail") != 1 {
		t.Fatal("expected migration 041's seed before the test starts")
	}

	if _, err := db.Exec(`DELETE FROM comms_templates WHERE slug = 'ds_battle_mail'`); err != nil {
		t.Fatalf("simulate an install that deleted the row: %v", err)
	}
	if countSlugRows(t, "ds_battle_mail") != 0 {
		t.Fatal("setup: the row is still there")
	}

	sql := goosePayload(t, "migrations/072_reseed_slugged_templates.sql")
	for run := 1; run <= 2; run++ {
		if _, err := db.Exec(sql); err != nil {
			t.Fatalf("re-seed run %d: %v", run, err)
		}
		if got := countSlugRows(t, "ds_battle_mail"); got != 1 {
			t.Fatalf("after re-seed run %d there are %d rows, want exactly 1", run, got)
		}
	}

	var content, vars string
	db.QueryRow(`SELECT content, required_vars FROM comms_templates WHERE slug = 'ds_battle_mail'`).
		Scan(&content, &vars)
	for _, want := range []string{"{task_force}", "{battle_time}", "{group_assignments}"} {
		if !strings.Contains(content, want) {
			t.Errorf("re-seeded content is missing %s", want)
		}
	}
	var parsed []string
	if err := json.Unmarshal([]byte(vars), &parsed); err != nil || len(parsed) != 3 {
		t.Errorf("re-seeded required_vars = %q (err %v)", vars, err)
	}
}
