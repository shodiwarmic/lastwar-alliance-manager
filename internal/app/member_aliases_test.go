package app

import (
	"net/http"
	"strconv"
	"testing"
)

// The member modal's alias routes gate global and OCR aliases on manage_members, resolved
// like every other permission. They read a `permissions` table that never existed, so
// only admins passed (private-docs 210).
func TestMemberAliasGlobalPermission(t *testing.T) {
	f := setupMobileTestDB(t)
	officer := sessionCookie(t, 2, "") // non-admin R4 holding manage_members
	member := sessionCookie(t, 3, "")  // R3 without it
	preview := sessionCookie(t, 1, "R3")
	path := "/api/members/" + strconv.Itoa(f.other) + "/aliases"

	aliasID := func(text string) string {
		var id int
		db.QueryRow(`SELECT id FROM member_aliases WHERE alias = ?`, text).Scan(&id)
		return strconv.Itoa(id)
	}

	// R3 and an admin previewing R3: refused on all three.
	seedAlias(t, f.other, "GlobalOne", "global", nil)
	seedAlias(t, f.other, "OcrOne", "ocr", nil)
	for name, c := range map[string]*http.Cookie{"R3": member, "admin previewing R3": preview} {
		if w := serveRouter(t, "POST", path, `{"alias":"NewGlobal","is_global":true}`, c); w.Code != http.StatusForbidden {
			t.Errorf("%s add global: %d", name, w.Code)
		}
		if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("GlobalOne"), "", c); w.Code != http.StatusForbidden {
			t.Errorf("%s delete global: %d", name, w.Code)
		}
		if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("OcrOne"), "", c); w.Code != http.StatusForbidden {
			t.Errorf("%s delete ocr: %d", name, w.Code)
		}
	}

	// The non-admin R4: all three succeed.
	if w := serveRouter(t, "POST", path, `{"alias":"NewGlobal","is_global":true}`, officer); w.Code != http.StatusCreated {
		t.Errorf("R4 add global: %d %s", w.Code, w.Body.String())
	}
	if n := countRows(t, `SELECT COUNT(*) FROM member_aliases WHERE alias = 'NewGlobal' AND category = 'global'`); n != 1 {
		t.Errorf("global rows = %d", n)
	}
	if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("GlobalOne"), "", officer); w.Code != http.StatusOK {
		t.Errorf("R4 delete global: %d", w.Code)
	}
	if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("OcrOne"), "", officer); w.Code != http.StatusOK {
		t.Errorf("R4 delete ocr: %d", w.Code)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM member_aliases WHERE alias IN ('GlobalOne', 'OcrOne')`); n != 0 {
		t.Errorf("%d deleted aliases remain", n)
	}

	// Personal aliases are unchanged: R3 adds and deletes its own, and can't delete R4's.
	if w := serveRouter(t, "POST", path, `{"alias":"MineR3","is_global":false}`, member); w.Code != http.StatusCreated {
		t.Errorf("R3 add personal: %d", w.Code)
	}
	if w := serveRouter(t, "POST", path, `{"alias":"MineR4","is_global":false}`, officer); w.Code != http.StatusCreated {
		t.Errorf("R4 add personal: %d", w.Code)
	}
	if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("MineR4"), "", member); w.Code != http.StatusForbidden {
		t.Errorf("R3 deleting R4's personal: %d", w.Code)
	}
	if w := serveRouter(t, "DELETE", "/api/aliases/"+aliasID("MineR3"), "", member); w.Code != http.StatusOK {
		t.Errorf("R3 delete own personal: %d", w.Code)
	}
}
