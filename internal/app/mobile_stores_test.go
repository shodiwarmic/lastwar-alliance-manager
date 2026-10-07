package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// The standing rule, enforced (private-docs 201, Project 15 decision 5): every table of a
// freshly migrated database is a mobile store's or excluded with a reason, and every
// /api/mobile/ route is declared, demo-blocked, and gated on real permission keys.

func TestMobileTableCoverage(t *testing.T) {
	setupNameMatchTestDB(t)
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()

	owner := map[string]string{}
	for _, st := range mobileStores() {
		for _, tb := range st.Tables {
			if prev, dup := owner[tb]; dup {
				t.Errorf("table %s is in two stores (%s, %s)", tb, prev, st.Key)
			}
			owner[tb] = st.Key
			if _, ex := mobileExcludedTables[tb]; ex {
				t.Errorf("table %s is both store %s's and excluded", tb, st.Key)
			}
		}
	}
	exists := map[string]bool{}
	for _, tb := range tables {
		exists[tb] = true
		_, stored := owner[tb]
		_, excluded := mobileExcludedTables[tb]
		if !stored && !excluded {
			t.Errorf("table %s is unclassified: CLAUDE.md checklist step 9 — a table holding game data ships "+
				"with a /api/mobile/* endpoint (declare it in a store in mobile_stores.go); anything else goes in "+
				"mobileExcludedTables with the reason", tb)
		}
	}
	for tb := range owner {
		if !exists[tb] {
			t.Errorf("store table %s does not exist", tb)
		}
	}
	for tb := range mobileExcludedTables {
		if !exists[tb] {
			t.Errorf("excluded table %s does not exist; remove its entry", tb)
		}
	}
}

func rankPermissionKeys() map[string]bool {
	keys := map[string]bool{}
	rt := reflect.TypeOf(RankPermissions{})
	for i := 0; i < rt.NumField(); i++ {
		if tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]; tag != "" && tag != "rank" {
			keys[tag] = true
		}
	}
	return keys
}

func TestMobileRoutesDeclaredGatedAndDemoBlocked(t *testing.T) {
	setupNameMatchTestDB(t)
	valid := rankPermissionKeys()
	declared := map[string]bool{}
	check := func(rt mobileRoute, perms ...[]string) {
		declared[rt.Method+" "+rt.Path] = true
		for _, set := range append(perms, rt.Perms) {
			for _, p := range set {
				if !valid[p] {
					t.Errorf("%s %s: %q is not a RankPermissions key", rt.Method, rt.Path, p)
				}
			}
		}
	}
	for _, rt := range mobileBaseRoutes() {
		check(rt)
	}
	for _, st := range mobileStores() {
		if len(st.Routes) == 0 {
			t.Errorf("store %s declares no route", st.Key)
		}
		for _, rt := range st.Routes {
			check(rt, st.ReadPerms, st.WritePerms)
		}
	}

	router := buildRouter()
	seen := 0
	router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil || !strings.HasPrefix(tpl, "/api/mobile/") {
			return nil
		}
		methods, _ := route.GetMethods()
		for _, m := range methods {
			seen++
			if !declared[m+" "+tpl] {
				t.Errorf("%s %s is registered but declared in no store (mobile_stores.go)", m, tpl)
			}
			blocked := false
			for _, d := range demoBlocked {
				var match mux.RouteMatch
				if d.method == m && route.Match(httptest.NewRequest(d.method, d.path, nil), &match) {
					blocked = true
				}
			}
			if !blocked {
				t.Errorf("%s %s has no demoBlocked row (demo_mode_test.go)", m, tpl)
			}
		}
		return nil
	})
	if seen != len(declared) {
		t.Errorf("router has %d mobile routes, registry declares %d", seen, len(declared))
	}
}

func capabilities(t *testing.T, token string) map[string]any {
	t.Helper()
	w := serveMobile(t, "GET", "/api/mobile/capabilities", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("capabilities: %d %s", w.Code, w.Body.String())
	}
	return decodeObject(t, w)
}

func setRankPerm(t *testing.T, rank, key string, on bool) {
	t.Helper()
	if _, err := db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.'||?, json(?)) WHERE rank = ?`,
		key, strconv.FormatBool(on), rank); err != nil {
		t.Fatal(err)
	}
}

// Permissions are resolved live: a matrix edit takes effect on the next request without
// signing in again.
func TestMobilePermissionsAreLive(t *testing.T) {
	f := setupMobileTestDB(t)
	tok := mobileToken(t, 2) // R4, minted while it holds manage_vs_points
	commit := `{"week_date":"2026-10-05","records":[{"member_id":` + strconv.Itoa(f.other) +
		`,"original_name":"Zed Player","category":"monday","score":5}],"save_aliases":[]}`

	caps := capabilities(t, tok)
	assertKeys(t, "capabilities", caps, "api_version", "app_version", "stores", "commit_categories")
	if caps["api_version"].(float64) != mobileAPIVersion {
		t.Errorf("api_version = %v", caps["api_version"])
	}
	sc := caps["stores"].(map[string]any)["scan_commit"].(map[string]any)
	if sc["read"] != true || sc["write"] != true || caps["commit_categories"].(map[string]any)["monday"] != true {
		t.Errorf("R4 capabilities = %v", caps)
	}

	setRankPerm(t, "R4", "manage_vs_points", false)
	if w := serveMobile(t, "POST", "/api/mobile/commit", commit, tok); w.Code != http.StatusForbidden {
		t.Errorf("after revoking: %d, want 403", w.Code)
	}
	caps = capabilities(t, tok)
	if caps["stores"].(map[string]any)["scan_commit"].(map[string]any)["write"] != false ||
		caps["commit_categories"].(map[string]any)["monday"] != false {
		t.Errorf("capabilities after revoking = %v", caps)
	}
	setRankPerm(t, "R4", "manage_vs_points", true)
	if w := serveMobile(t, "POST", "/api/mobile/commit", commit, tok); w.Code != http.StatusOK {
		t.Errorf("after restoring: %d %s", w.Code, w.Body.String())
	}

	// An admin passes everything; an R3 token is refused the write routes.
	for _, st := range mobileStores() {
		for _, rt := range st.Routes {
			if w := serveMobile(t, rt.Method, rt.Path, "{}", mobileToken(t, 1)); w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
				t.Errorf("admin %s %s: %d", rt.Method, rt.Path, w.Code)
			}
		}
	}
	if w := serveMobile(t, "POST", "/api/mobile/commit", commit, mobileToken(t, 3)); w.Code != http.StatusForbidden {
		t.Errorf("R3 commit: %d", w.Code)
	}

	// A deactivated user's token stops at once.
	db.Exec(`UPDATE users SET is_active = 0 WHERE id = 3`)
	if w := serveMobile(t, "GET", "/api/mobile/capabilities", "", mobileToken(t, 3)); w.Code != http.StatusUnauthorized {
		t.Errorf("deactivated: %d", w.Code)
	}
}

// A non-admin officer holding manage_members commits a global alias at once. The check
// used to read a token claim; resolving it with userHasPermission inside the open
// transaction would wait out the statement ceiling (Project 15, F1).
func TestMobileCommitGlobalAliasNoDeadlock(t *testing.T) {
	f := setupMobileTestDB(t)
	withCeiling(t, 2*time.Second)
	start := time.Now()
	w := serveMobile(t, "POST", "/api/mobile/commit", `{"week_date":"2026-10-05","records":[],
		"save_aliases":[{"failed_alias":"Zg","member_id":`+strconv.Itoa(f.other)+`,"category":"global"}]}`, mobileToken(t, 2))
	if w.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", w.Code, w.Body.String())
	}
	var out MobileCommitResponse
	json.Unmarshal(w.Body.Bytes(), &out)
	if out.AliasesSaved != 1 || len(out.Errors) != 0 {
		t.Errorf("response = %+v", out)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("commit took %v — something waited on the connection", d)
	}
}

func TestMobileLoginReportsAPIVersion(t *testing.T) {
	setupMobileTestDB(t)
	req := httptest.NewRequest("POST", "/api/mobile/login", strings.NewReader(`{"username":"officer","password":"`+mobileTestPassword+`"}`))
	req.RemoteAddr = "10.151.0.1:1"
	w := httptest.NewRecorder()
	buildRouter().ServeHTTP(w, req)
	out := decodeObject(t, w)
	if out["api_version"] != float64(mobileAPIVersion) {
		t.Errorf("login api_version = %v (keys %v)", out["api_version"], keysOf(out))
	}
}
