package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// setReportKeys pins R3's three report keys.
func setReportKeys(t *testing.T, weekly, misses, participation bool) {
	t.Helper()
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions,
		'$.view_report_weekly', json('`+b(weekly)+`'), '$.view_report_vs_misses', json('`+b(misses)+`'),
		'$.view_report_participation', json('`+b(participation)+`')) WHERE rank = 'R3'`)
}

func TestReportsPageNeedsAReportKey(t *testing.T) {
	setupPreviewTestDB(t)
	setReportKeys(t, false, false, false)
	officer := sessionCookie(t, 3, "")
	if w := serveAs(officer, http.MethodGet, "/reports", "", handleReportsPage); w.Code != http.StatusForbidden {
		t.Errorf("no keys: status %d, want 403", w.Code)
	}

	setReportKeys(t, false, true, false)
	w := serveAs(officer, http.MethodGet, "/reports", "", handleReportsPage)
	if w.Code != http.StatusOK {
		t.Fatalf("one key: status %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-tab="vs-misses"`) || strings.Contains(body, `data-tab="weekly"`) {
		t.Errorf("one key should render exactly its tab")
	}

	if w := serveAs(sessionCookie(t, 1, ""), http.MethodGet, "/reports", "", handleReportsPage); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `data-tab="participation"`) {
		t.Errorf("admin: status %d", w.Code)
	}
}

func TestReportsListShowsOnlyHeldReports(t *testing.T) {
	setupPreviewTestDB(t)
	setReportKeys(t, true, false, true)
	w := serveAs(sessionCookie(t, 3, ""), http.MethodGet, "/api/reports", "", handleReportsList)
	var defs []reportDef
	json.Unmarshal(w.Body.Bytes(), &defs)
	if len(defs) != 2 || defs[0].Key != "weekly" || defs[1].Key != "participation" {
		t.Errorf("got %+v", defs)
	}

	setReportKeys(t, false, false, false)
	w = serveAs(sessionCookie(t, 3, ""), http.MethodGet, "/api/reports", "", handleReportsList)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("no keys: %s", w.Body)
	}
}
