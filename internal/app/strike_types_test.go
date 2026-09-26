package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pressly/goose/v3"
)

// Migration 079 turns free-text strike categories into a managed list. The copy
// of existing values is exercised on a database migrated to 078 with real
// free-text rows, then taken to 079 — the path another install's upgrade takes.
func TestStrikeTypesMigrationCopiesExistingValues(t *testing.T) {
	conn := migrateTo(t, 78)
	for _, q := range []string{
		`INSERT INTO members (id, name, rank) VALUES (1, 'A', 'R3')`,
		`INSERT INTO accountability_strikes (member_id, strike_type, reason) VALUES
		    (1, 'manual', 'x'), (1, 'vs_below_threshold', 'x'),
		    (1, 'Diplomacy Violation', 'x'), (1, 'Diplomacy Violation', 'y'),
		    (1, 'diplomacy violation', 'x'), (1, '', 'x')`,
	} {
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	if err := goose.UpTo(conn, "migrations", 79); err != nil {
		t.Fatalf("goose.UpTo(79): %v", err)
	}

	rows, err := conn.Query(`SELECT key, label, is_system FROM strike_types ORDER BY sort_order, key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var key, label string
		var sys int
		rows.Scan(&key, &label, &sys)
		got = append(got, key+"|"+label+"|"+strconv.Itoa(sys))
	}
	want := []string{
		"vs_below_threshold|VS Below Threshold|1",
		"train_no_show|Train No-Show|1",
		"storm_no_show|Storm No-Show|1",
		"manual|Manual|1",
		// Verbatim and unmerged: look-alike spellings are the operator's to reconcile.
		"Diplomacy Violation|Diplomacy Violation|0",
		"diplomacy violation|diplomacy violation|0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("strike_types after 079:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var strikes int
	conn.QueryRow(`SELECT COUNT(*) FROM accountability_strikes`).Scan(&strikes)
	if strikes != 6 {
		t.Errorf("strikes = %d, want all 6 untouched", strikes)
	}
}

func strikeTypeRequest(method, path string, body any, id int) *http.Request {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, strings.NewReader(string(b)))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r = scheduleTestActor(r)
	if id != 0 {
		r = mux.SetURLVars(r, map[string]string{"id": strconv.Itoa(id)})
	}
	return r
}

func strikeTypeID(t *testing.T, key string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM strike_types WHERE key = ?`, key).Scan(&id); err != nil {
		t.Fatalf("strike type %q: %v", key, err)
	}
	return id
}

func createStrike(t *testing.T, key string) int {
	t.Helper()
	rr := httptest.NewRecorder()
	handleStrikeCreate(rr, strikeTypeRequest(http.MethodPost, "/", map[string]any{
		"member_id": 1, "strike_type": key, "reason": "test",
	}, 0))
	return rr.Code
}

func TestStrikeTypesAPI(t *testing.T) {
	setupSettingsTestDB(t)
	if _, err := db.Exec(`INSERT INTO members (id, name, rank) VALUES (1, 'Alpha', 'R3')`); err != nil {
		t.Fatal(err)
	}

	// Create: key slugged from the label, accent-folded.
	rr := httptest.NewRecorder()
	handleStrikeTypeCreate(rr, strikeTypeRequest(http.MethodPost, "/", map[string]any{"label": "Réunion No-Show"}, 0))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", rr.Code, rr.Body.String())
	}
	var created StrikeType
	json.Unmarshal(rr.Body.Bytes(), &created)
	if created.Key != "reunion_no_show" || created.IsSystem || !created.Active {
		t.Errorf("created = %+v, want key reunion_no_show, custom, active", created)
	}

	// Duplicate key → 409.
	rr = httptest.NewRecorder()
	handleStrikeTypeCreate(rr, strikeTypeRequest(http.MethodPost, "/", map[string]any{"label": "Reunion no show"}, 0))
	if rr.Code != http.StatusConflict {
		t.Errorf("duplicate create = %d, want 409", rr.Code)
	}

	// Strike creation validates against the list.
	if code := createStrike(t, "no_such_category"); code != http.StatusBadRequest {
		t.Errorf("strike with unknown category = %d, want 400", code)
	}
	if code := createStrike(t, "reunion_no_show"); code != http.StatusOK {
		t.Errorf("strike with active custom category = %d, want 200", code)
	}

	// Deactivate the custom row: allowed, and the strike form then refuses it.
	rr = httptest.NewRecorder()
	handleStrikeTypeUpdate(rr, strikeTypeRequest(http.MethodPut, "/", map[string]any{"label": "Reunion No-Show", "active": false, "sort_order": 9}, created.ID))
	if rr.Code != http.StatusOK {
		t.Fatalf("deactivate = %d (%s)", rr.Code, rr.Body.String())
	}
	if code := createStrike(t, "reunion_no_show"); code != http.StatusBadRequest {
		t.Errorf("strike with inactive category = %d, want 400", code)
	}

	// The default GET omits it; ?all=1 includes it, with its use count.
	list := func(q string) []StrikeType {
		rr := httptest.NewRecorder()
		handleStrikeTypes(rr, strikeTypeRequest(http.MethodGet, "/api/accountability/strike-types"+q, nil, 0))
		var out []StrikeType
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out
	}
	find := func(ts []StrikeType, key string) *StrikeType {
		for i := range ts {
			if ts[i].Key == key {
				return &ts[i]
			}
		}
		return nil
	}
	if find(list(""), "reunion_no_show") != nil {
		t.Error("inactive category offered by the default list")
	}
	if got := find(list("?all=1"), "reunion_no_show"); got == nil || got.InUse != 1 || got.Label != "Reunion No-Show" {
		t.Errorf("?all=1 row = %+v, want inactive, relabelled, in_use 1", got)
	}

	// In-use custom row cannot be deleted.
	rr = httptest.NewRecorder()
	handleStrikeTypeDelete(rr, strikeTypeRequest(http.MethodDelete, "/", nil, created.ID))
	if rr.Code != http.StatusConflict {
		t.Errorf("delete in-use custom = %d, want 409", rr.Code)
	}

	// System rows: neither deleted nor deactivated; relabelling is fine.
	train := strikeTypeID(t, "train_no_show")
	rr = httptest.NewRecorder()
	handleStrikeTypeDelete(rr, strikeTypeRequest(http.MethodDelete, "/", nil, train))
	if rr.Code != http.StatusConflict {
		t.Errorf("delete system = %d, want 409", rr.Code)
	}
	rr = httptest.NewRecorder()
	handleStrikeTypeUpdate(rr, strikeTypeRequest(http.MethodPut, "/", map[string]any{"label": "Train No-Show", "active": false, "sort_order": 1}, train))
	if rr.Code != http.StatusConflict {
		t.Errorf("deactivate system = %d, want 409", rr.Code)
	}
	rr = httptest.NewRecorder()
	handleStrikeTypeUpdate(rr, strikeTypeRequest(http.MethodPut, "/", map[string]any{"label": "Conductor No-Show", "active": true, "sort_order": 1}, train))
	if rr.Code != http.StatusOK {
		t.Errorf("relabel system = %d, want 200", rr.Code)
	}

	// An unused custom row can be deleted.
	rr = httptest.NewRecorder()
	handleStrikeTypeCreate(rr, strikeTypeRequest(http.MethodPost, "/", map[string]any{"label": "Typo Categry"}, 0))
	var typo StrikeType
	json.Unmarshal(rr.Body.Bytes(), &typo)
	rr = httptest.NewRecorder()
	handleStrikeTypeDelete(rr, strikeTypeRequest(http.MethodDelete, "/", nil, typo.ID))
	if rr.Code != http.StatusNoContent {
		t.Errorf("delete unused custom = %d, want 204", rr.Code)
	}
}
