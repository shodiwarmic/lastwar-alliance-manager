package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The VS page's Save sends only the cells it changed, each with the value it
// loaded (209). These pin the four pairings of loaded (L), database now (D) and
// typed (T), and that nothing outside the sent cells is touched.

const vsSaveWeek = "2026-10-12" // a Monday

func seedVSSave(t *testing.T) {
	t.Helper()
	setupSettingsTestDB(t)
	mustExec(t, `INSERT INTO members (id, name, rank) VALUES (1,'Alpha','R3'),(2,'Bravo','R3'),(3,'Charlie','R2')`)
	mustExec(t, `INSERT INTO vs_points (member_id, week_date, monday, tuesday, wednesday, thursday, friday, saturday)
		VALUES (1, ?, 10, 20, 30, 40, 50, 60), (2, ?, 11, 21, 31, 41, 51, 61)`, vsSaveWeek, vsSaveWeek)
}

func mustExec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func postVSSave(t *testing.T, week string, cells []vsSaveCell) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"week_date": week, "cells": cells})
	req := httptest.NewRequest(http.MethodPost, "/api/vs-points", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), authUserKey, &AuthUser{ID: 1, Username: "officer", IsAdmin: true}))
	w := httptest.NewRecorder()
	saveVSPoints(w, req)
	return w
}

func vsRow(t *testing.T, memberID int) [6]int {
	t.Helper()
	var v [6]int
	if err := db.QueryRow(`SELECT monday, tuesday, wednesday, thursday, friday, saturday FROM vs_points
		WHERE member_id = ? AND week_date = ?`, memberID, vsSaveWeek).Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5]); err != nil {
		t.Fatalf("read vs row %d: %v", memberID, err)
	}
	return v
}

func lastVSDetails(t *testing.T) string {
	t.Helper()
	var d string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'vs_points' ORDER BY id DESC LIMIT 1`).Scan(&d)
	return d
}

func TestVSSaveWritesOnlyTheSentCell(t *testing.T) {
	seedVSSave(t)
	w := postVSSave(t, vsSaveWeek, []vsSaveCell{{MemberID: 1, Day: "wednesday", Loaded: 30, Value: 99}})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if got := vsRow(t, 1); got != [6]int{10, 20, 99, 40, 50, 60} {
		t.Errorf("member 1 = %v", got)
	}
	if got := vsRow(t, 2); got != [6]int{11, 21, 31, 41, 51, 61} {
		t.Errorf("member 2 touched: %v", got)
	}
	if d := lastVSDetails(t); d != "1 cells, 1 members" {
		t.Errorf("details = %q", d)
	}
}

func TestVSSaveCreatesARowWithOnlyTheEditedDay(t *testing.T) {
	seedVSSave(t)
	if w := postVSSave(t, vsSaveWeek, []vsSaveCell{{MemberID: 3, Day: "friday", Loaded: 0, Value: 7}}); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if got := vsRow(t, 3); got != [6]int{0, 0, 0, 0, 7, 0} {
		t.Errorf("member 3 = %v", got)
	}
}

func TestVSSaveRefusesBadInput(t *testing.T) {
	seedVSSave(t)
	cases := map[string][]vsSaveCell{
		"unknown day":    {{MemberID: 1, Day: "sunday", Loaded: 0, Value: 1}},
		"injected day":   {{MemberID: 1, Day: "monday = 0, tuesday", Loaded: 0, Value: 1}},
		"negative":       {{MemberID: 1, Day: "monday", Loaded: 10, Value: -1}},
		"empty":          {},
		"unknown member": {{MemberID: 999, Day: "monday", Loaded: 0, Value: 1}},
	}
	for name, cells := range cases {
		if w := postVSSave(t, vsSaveWeek, cells); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
	}
	if w := postVSSave(t, "not-a-date", []vsSaveCell{{MemberID: 1, Day: "monday", Loaded: 10, Value: 1}}); w.Code != http.StatusBadRequest {
		t.Errorf("bad week: status %d", w.Code)
	}
	if got := vsRow(t, 1); got != [6]int{10, 20, 30, 40, 50, 60} {
		t.Errorf("a refused save wrote: %v", got)
	}
}

// D ≠ L and D = T: the database already holds what was typed — not written, not counted.
func TestVSSaveSkipsACellTheDatabaseAlreadyHolds(t *testing.T) {
	seedVSSave(t)
	w := postVSSave(t, vsSaveWeek, []vsSaveCell{
		{MemberID: 1, Day: "monday", Loaded: 0, Value: 10}, // D = T = 10
		{MemberID: 2, Day: "monday", Loaded: 11, Value: 12},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if d := lastVSDetails(t); d != "1 cells, 1 members" {
		t.Errorf("details = %q", d)
	}
}

// All three differ: 409 with the list, and nothing written — not even a clean cell.
func TestVSSaveConflictWritesNothing(t *testing.T) {
	seedVSSave(t)
	w := postVSSave(t, vsSaveWeek, []vsSaveCell{
		{MemberID: 1, Day: "tuesday", Loaded: 0, Value: 25}, // D=20, L=0, T=25
		{MemberID: 2, Day: "monday", Loaded: 11, Value: 12}, // clean
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409: %s", w.Code, w.Body)
	}
	var body struct{ Conflicts []vsSaveConflict }
	json.Unmarshal(w.Body.Bytes(), &body)
	want := vsSaveConflict{MemberID: 1, Name: "Alpha", Day: "tuesday", Loaded: 0, Current: 20, Typed: 25}
	if len(body.Conflicts) != 1 || body.Conflicts[0] != want {
		t.Errorf("conflicts = %+v", body.Conflicts)
	}
	if got := vsRow(t, 2); got[0] != 11 {
		t.Errorf("clean cell written despite the conflict: %v", got)
	}
	if got := vsRow(t, 1); got[1] != 20 {
		t.Errorf("conflicting cell written: %v", got)
	}

	// The page resubmits "keep mine" against the value it was shown.
	w = postVSSave(t, vsSaveWeek, []vsSaveCell{
		{MemberID: 1, Day: "tuesday", Loaded: 20, Value: 25, Overridden: true},
		{MemberID: 2, Day: "monday", Loaded: 11, Value: 12},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("resubmit status %d: %s", w.Code, w.Body)
	}
	if got := vsRow(t, 1); got[1] != 25 {
		t.Errorf("resubmitted cell = %d", got[1])
	}
	if d := lastVSDetails(t); d != "2 cells, 2 members, 1 conflicts overridden" {
		t.Errorf("details = %q", d)
	}
}

// A cell sent with value == loaded is a no-op, whatever the database holds now.
func TestVSSaveIgnoresAnUnchangedCell(t *testing.T) {
	seedVSSave(t)
	w := postVSSave(t, vsSaveWeek, []vsSaveCell{
		{MemberID: 1, Day: "monday", Loaded: 0, Value: 0}, // D = 10: would zero an import
		{MemberID: 2, Day: "friday", Loaded: 51, Value: 52},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if got := vsRow(t, 1); got[0] != 10 {
		t.Errorf("no-op cell overwrote the import: %v", got)
	}
}

func TestVSSaveSnapsTheWeekToItsMonday(t *testing.T) {
	seedVSSave(t)
	if w := postVSSave(t, "2026-10-14", []vsSaveCell{{MemberID: 1, Day: "saturday", Loaded: 60, Value: 61}}); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if got := vsRow(t, 1); got[5] != 61 {
		t.Errorf("saturday = %d (week not snapped?)", got[5])
	}
}
