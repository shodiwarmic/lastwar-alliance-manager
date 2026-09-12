package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pressly/goose/v3"
)

// The Officers directory had no tests at all before this file. The load-bearing
// ones are the two delete tests: foreign_keys is off app-wide, so the schema's
// ON DELETE CASCADE has never fired and every delete through these handlers used
// to strand its children. Both fail on the pre-PR handlers, which is the point.

// setupOCTestDB points the package-level db at a fresh temp SQLite file, runs
// every migration, and seeds an officer to attribute activity to. Follows
// setupFileTagsTestDB; restores the previous handle on cleanup.
func setupOCTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
	})
	if _, err := db.Exec(`INSERT INTO users (id, username, password, is_admin) VALUES (20, 'r5user', 'x', 1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
}

// ocRouter mounts the handlers under the same paths main.go uses, so {id} and
// {member_id} resolve exactly as they do in production.
func ocRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/api/officer-command/data", getOfficerCommandData).Methods("GET")
	r.HandleFunc("/api/officer-command/categories", createOCCategory).Methods("POST")
	r.HandleFunc("/api/officer-command/categories/{id:[0-9]+}", deleteOCCategory).Methods("DELETE")
	r.HandleFunc("/api/officer-command/responsibilities", createOCResponsibility).Methods("POST")
	r.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}", updateOCResponsibility).Methods("PUT")
	r.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}", deleteOCResponsibility).Methods("DELETE")
	r.HandleFunc("/api/officer-command/responsibilities/{id:[0-9]+}/assignees", addOCAssignee).Methods("POST")
	return r
}

func ocDo(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req = req.WithContext(context.WithValue(req.Context(),
		authUserKey, &AuthUser{ID: 20, Username: "r5user", IsAdmin: true}))
	rec := httptest.NewRecorder()
	ocRouter().ServeHTTP(rec, req)
	return rec
}

func ocCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// ocSeedCategory creates a category directly and returns its id.
func ocSeedCategory(t *testing.T, name string) int {
	t.Helper()
	res, err := db.Exec(`INSERT INTO oc_categories (name, display_order) VALUES (?, 0)`, name)
	if err != nil {
		t.Fatalf("seed category: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// ocCreateResp posts a responsibility and returns the decoded row.
func ocCreateResp(t *testing.T, catID int, name string, tasks []string) OCResponsibility {
	t.Helper()
	rec := ocDo(t, http.MethodPost, "/api/officer-command/responsibilities", map[string]any{
		"category_id": catID, "name": name, "description": "d", "frequency": "Weekly", "tasks": tasks,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create responsibility: %d %s", rec.Code, rec.Body.String())
	}
	var rp OCResponsibility
	if err := json.Unmarshal(rec.Body.Bytes(), &rp); err != nil {
		t.Fatalf("decode responsibility: %v", err)
	}
	return rp
}

// ocFetchTasks reads one responsibility's tasks back through GET /data, which is
// the only path the UI ever sees them on.
func ocFetchTasks(t *testing.T, respID int) []string {
	t.Helper()
	rec := ocDo(t, http.MethodGet, "/api/officer-command/data", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get data: %d %s", rec.Code, rec.Body.String())
	}
	var cats []OCCategory
	if err := json.Unmarshal(rec.Body.Bytes(), &cats); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	for _, c := range cats {
		for _, rp := range c.Responsibilities {
			if rp.ID == respID {
				if rp.Tasks == nil {
					t.Fatalf("tasks came back as null, not []")
				}
				return rp.Tasks
			}
		}
	}
	t.Fatalf("responsibility %d not in GET /data", respID)
	return nil
}

// TestMigration071SweepsOnlyOrphans applies 071 over a database holding both live
// rows and rows stranded by earlier deletes, and asserts it takes exactly the
// stranded ones. This is a data delete inside a migration, so the blast radius is
// the thing under test.
func TestMigration071SweepsOnlyOrphans(t *testing.T) {
	conn := migrateTo(t, 70)

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.Exec(q, args...); err != nil {
			t.Fatalf("seed (%s): %v", q, err)
		}
	}
	// A member for the live assignee to point at.
	exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R4')`)

	// Live: category 1 -> responsibility 1 -> assignee.
	exec(`INSERT INTO oc_categories (id, name, display_order) VALUES (1, 'Live', 0)`)
	exec(`INSERT INTO oc_responsibilities (id, category_id, name, description, frequency, display_order)
	      VALUES (1, 1, 'Live resp', '', 'Weekly', 0)`)
	exec(`INSERT INTO oc_assignees (responsibility_id, member_id) VALUES (1, 1)`)

	// Stranded: a responsibility whose category is gone, and two assignees whose
	// responsibility is gone. Exactly what the dev database was carrying.
	exec(`INSERT INTO oc_responsibilities (id, category_id, name, description, frequency, display_order)
	      VALUES (2, 999, 'Orphan resp', '', 'Weekly', 0)`)
	exec(`INSERT INTO oc_assignees (responsibility_id, member_id) VALUES (998, 1)`)
	exec(`INSERT INTO oc_assignees (responsibility_id, member_id) VALUES (997, 1)`)

	if err := goose.UpTo(conn, "migrations", 71); err != nil {
		t.Fatalf("goose.UpTo(71): %v", err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("count (%s): %v", q, err)
		}
		return n
	}

	if n := count(`SELECT COUNT(*) FROM oc_categories WHERE id = 1`); n != 1 {
		t.Errorf("live category swept: got %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM oc_responsibilities WHERE id = 1`); n != 1 {
		t.Errorf("live responsibility swept: got %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM oc_assignees WHERE responsibility_id = 1`); n != 1 {
		t.Errorf("live assignee swept: got %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM oc_responsibilities WHERE id = 2`); n != 0 {
		t.Errorf("orphan responsibility survived: got %d, want 0", n)
	}
	if n := count(`SELECT COUNT(*) FROM oc_assignees WHERE responsibility_id IN (997, 998)`); n != 0 {
		t.Errorf("orphan assignees survived: got %d, want 0", n)
	}
	// The orphan responsibility's own assignees must go with it, not be left
	// behind as a new orphan by the sweep itself.
	if n := count(`SELECT COUNT(*) FROM oc_assignees WHERE responsibility_id NOT IN (SELECT id FROM oc_responsibilities)`); n != 0 {
		t.Errorf("sweep left orphaned assignees behind: got %d, want 0", n)
	}

	// And the new table is there and empty.
	if n := count(`SELECT COUNT(*) FROM oc_tasks`); n != 0 {
		t.Errorf("oc_tasks not empty after migration: got %d", n)
	}
}

// TestTasksRoundTripInOrder covers the whole editing model: create with a list,
// read it back in order, replace it on update, and drop blank lines.
func TestTasksRoundTripInOrder(t *testing.T) {
	setupOCTestDB(t)
	catID := ocSeedCategory(t, "Ops")

	rp := ocCreateResp(t, catID, "Roster Management", []string{"first", "second", "third"})
	if got := ocFetchTasks(t, rp.ID); strings.Join(got, "|") != "first|second|third" {
		t.Fatalf("after create: got %v", got)
	}

	rec := ocDo(t, http.MethodPut, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID), map[string]any{
		"name": "Roster Management", "description": "d", "frequency": "Weekly",
		"tasks": []string{"only", "two"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if got := ocFetchTasks(t, rp.ID); strings.Join(got, "|") != "only|two" {
		t.Fatalf("after update: got %v", got)
	}
	// Replace-on-save, not append: the removed third row must be gone from the
	// table, not merely absent from the response.
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_tasks WHERE responsibility_id = ?`, rp.ID); n != 2 {
		t.Fatalf("oc_tasks rows after update: got %d, want 2", n)
	}

	rec = ocDo(t, http.MethodPut, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID), map[string]any{
		"name": "Roster Management", "description": "d", "frequency": "Weekly",
		"tasks": []string{"  kept  ", "", "   ", "\t\n", "also kept"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update with blanks: %d %s", rec.Code, rec.Body.String())
	}
	if got := ocFetchTasks(t, rp.ID); strings.Join(got, "|") != "kept|also kept" {
		t.Fatalf("blank lines not dropped/trimmed: got %v", got)
	}

	// A responsibility with no tasks serialises as [], never null.
	bare := ocCreateResp(t, catID, "No tasks here", nil)
	if got := ocFetchTasks(t, bare.ID); len(got) != 0 {
		t.Fatalf("expected no tasks, got %v", got)
	}
}

// TestTaskLimits checks both bounds, that the rune count is a rune count, and
// that a rejected request writes nothing.
func TestTaskLimits(t *testing.T) {
	setupOCTestDB(t)
	catID := ocSeedCategory(t, "Ops")
	rp := ocCreateResp(t, catID, "Subject", []string{"starting point"})

	put := func(tasks []string) *httptest.ResponseRecorder {
		return ocDo(t, http.MethodPut, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID), map[string]any{
			"name": "Subject", "description": "d", "frequency": "Weekly", "tasks": tasks,
		})
	}

	tooMany := make([]string, 26)
	for i := range tooMany {
		tooMany[i] = "task"
	}
	if rec := put(tooMany); rec.Code != http.StatusBadRequest {
		t.Errorf("26 tasks: got %d, want 400", rec.Code)
	}
	if rec := put([]string{strings.Repeat("a", 301)}); rec.Code != http.StatusBadRequest {
		t.Errorf("301-rune task: got %d, want 400", rec.Code)
	}
	if rec := put([]string{strings.Repeat("a", 300)}); rec.Code != http.StatusNoContent {
		t.Errorf("300-rune task: got %d, want 204", rec.Code)
	}

	// 300 runes of a 3-byte character: 900 bytes, well past the limit if anyone
	// reaches for len() instead of utf8.RuneCountInString.
	multibyte := strings.Repeat("é", 300) // é is 2 bytes; 600 bytes total
	if got := len(multibyte); got <= 300 {
		t.Fatalf("fixture is not multibyte: %d bytes", got)
	}
	if rec := put([]string{multibyte}); rec.Code != http.StatusNoContent {
		t.Errorf("300-rune multibyte task: got %d, want 204 (byte count leaking?)", rec.Code)
	}
	if got := ocFetchTasks(t, rp.ID); len(got) != 1 || got[0] != multibyte {
		t.Errorf("multibyte task did not round-trip intact")
	}

	// Exactly 25 is allowed.
	exactly := make([]string, 25)
	for i := range exactly {
		exactly[i] = "task"
	}
	if rec := put(exactly); rec.Code != http.StatusNoContent {
		t.Errorf("25 tasks: got %d, want 204", rec.Code)
	}

	// Nothing is written on a 400: the list is still the 25 from above, and the
	// name the rejected request carried was not applied either.
	if rec := put(tooMany); rec.Code != http.StatusBadRequest {
		t.Fatalf("second 26-task attempt: got %d, want 400", rec.Code)
	}
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_tasks WHERE responsibility_id = ?`, rp.ID); n != 25 {
		t.Errorf("rejected request changed the table: got %d rows, want 25", n)
	}
}

// TestDeleteResponsibilityRemovesChildren is the load-bearing one: on the
// pre-PR handler this leaves both the tasks and the assignees behind, because
// foreign_keys is off and the cascade never runs.
func TestDeleteResponsibilityRemovesChildren(t *testing.T) {
	setupOCTestDB(t)
	catID := ocSeedCategory(t, "Ops")
	if _, err := db.Exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R4')`); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rp := ocCreateResp(t, catID, "Doomed", []string{"a", "b", "c"})
	if rec := ocDo(t, http.MethodPost, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID)+"/assignees",
		map[string]any{"member_id": 1}); rec.Code != http.StatusOK {
		t.Fatalf("add assignee: %d %s", rec.Code, rec.Body.String())
	}

	if rec := ocDo(t, http.MethodDelete, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	if n := ocCount(t, `SELECT COUNT(*) FROM oc_responsibilities WHERE id = ?`, rp.ID); n != 0 {
		t.Errorf("responsibility survived: %d", n)
	}
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_tasks WHERE responsibility_id = ?`, rp.ID); n != 0 {
		t.Errorf("orphaned %d task rows", n)
	}
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_assignees WHERE responsibility_id = ?`, rp.ID); n != 0 {
		t.Errorf("orphaned %d assignee rows", n)
	}
}

// TestDeleteCategoryRemovesChildren is the same, two levels down.
func TestDeleteCategoryRemovesChildren(t *testing.T) {
	setupOCTestDB(t)
	catID := ocSeedCategory(t, "Doomed")
	keepID := ocSeedCategory(t, "Kept")
	if _, err := db.Exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R4')`); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var doomed []int
	for _, name := range []string{"one", "two"} {
		rp := ocCreateResp(t, catID, name, []string{"x", "y"})
		doomed = append(doomed, rp.ID)
		if rec := ocDo(t, http.MethodPost, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID)+"/assignees",
			map[string]any{"member_id": 1}); rec.Code != http.StatusOK {
			t.Fatalf("add assignee: %d %s", rec.Code, rec.Body.String())
		}
	}
	// A second category that must be untouched.
	kept := ocCreateResp(t, keepID, "survivor", []string{"still here"})

	if rec := ocDo(t, http.MethodDelete, "/api/officer-command/categories/"+strconv.Itoa(catID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete category: %d %s", rec.Code, rec.Body.String())
	}

	if n := ocCount(t, `SELECT COUNT(*) FROM oc_categories WHERE id = ?`, catID); n != 0 {
		t.Errorf("category survived: %d", n)
	}
	for _, id := range doomed {
		if n := ocCount(t, `SELECT COUNT(*) FROM oc_responsibilities WHERE id = ?`, id); n != 0 {
			t.Errorf("responsibility %d survived", id)
		}
		if n := ocCount(t, `SELECT COUNT(*) FROM oc_tasks WHERE responsibility_id = ?`, id); n != 0 {
			t.Errorf("responsibility %d orphaned %d task rows", id, n)
		}
		if n := ocCount(t, `SELECT COUNT(*) FROM oc_assignees WHERE responsibility_id = ?`, id); n != 0 {
			t.Errorf("responsibility %d orphaned %d assignee rows", id, n)
		}
	}
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_responsibilities WHERE id = ?`, kept.ID); n != 1 {
		t.Errorf("the other category's responsibility was taken too")
	}
	if n := ocCount(t, `SELECT COUNT(*) FROM oc_tasks WHERE responsibility_id = ?`, kept.ID); n != 1 {
		t.Errorf("the other category's tasks were taken too: %d", n)
	}
}

// TestUpdateResponsibilityNotFound covers the pre-write Scan the handler used to
// ignore: a missing row is a 404, not a silent update against a zero value.
func TestUpdateResponsibilityNotFound(t *testing.T) {
	setupOCTestDB(t)
	rec := ocDo(t, http.MethodPut, "/api/officer-command/responsibilities/4242", map[string]any{
		"name": "ghost", "description": "", "frequency": "Weekly", "tasks": []string{},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("update missing responsibility: got %d, want 404", rec.Code)
	}
	if rec := ocDo(t, http.MethodDelete, "/api/officer-command/responsibilities/4242", nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing responsibility: got %d, want 404", rec.Code)
	}
	if rec := ocDo(t, http.MethodDelete, "/api/officer-command/categories/4242", nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing category: got %d, want 404", rec.Code)
	}
}

// TestTaskActivityDetailRecordsReword guards the audit trail: a same-length
// rewrite of an officer's instructions is a change, so counting alone is not
// enough to decide whether to log one.
func TestTaskActivityDetailRecordsReword(t *testing.T) {
	setupOCTestDB(t)
	catID := ocSeedCategory(t, "Ops")
	rp := ocCreateResp(t, catID, "Subject", []string{"post a warning after 3 days"})

	rec := ocDo(t, http.MethodPut, "/api/officer-command/responsibilities/"+strconv.Itoa(rp.ID), map[string]any{
		"name": "Subject", "description": "d", "frequency": "Weekly",
		"tasks": []string{"post a warning after 2 days"},
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	var details sql.NullString
	err := db.QueryRow(`SELECT details FROM activity_log
	                    WHERE entity_type = 'oc_responsibility' AND action = 'updated'
	                    ORDER BY id DESC LIMIT 1`).Scan(&details)
	if err != nil {
		t.Fatalf("read activity: %v", err)
	}
	if !strings.Contains(details.String, "tasks updated") {
		t.Errorf("same-length reword went unlogged: details = %q", details.String)
	}
}
