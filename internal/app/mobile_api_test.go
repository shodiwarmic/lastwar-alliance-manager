package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// The mobile API is a contract with a separately released client (the Android scanner), so
// these tests pin each response's key set and the rows a request writes. A later change to
// either belongs in the same diff as the assertion it changes.

const mobileTestPassword = "correct horse battery"

type mobileFixture struct {
	r4Member, r3Member, other, former int
}

// setupMobileTestDB: an unlinked admin (1), a non-admin R4 officer (2), a non-admin R3 (3)
// and a deactivated user (4). R4 and R3 permissions are pinned so the assertions don't
// depend on the migration defaults.
func setupMobileTestDB(t *testing.T) mobileFixture {
	t.Helper()
	setupNameMatchTestDB(t)
	if store == nil {
		initSessionStore()
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(mobileTestPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := mobileFixture{
		r4Member: seedMember(t, "Officer Four", "R4"),
		r3Member: seedMember(t, "Member Three", "R3"),
		other:    seedMember(t, "Zed Player", "R2"),
		former:   seedMember(t, "Gone Player", "EX"),
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', ?, 1, 1)`, []any{hash}},
		{`INSERT INTO users (id, username, password, is_admin, is_active, member_id) VALUES (2, 'officer', ?, 0, 1, ?)`, []any{hash, f.r4Member}},
		{`INSERT INTO users (id, username, password, is_admin, is_active, member_id) VALUES (3, 'member', ?, 0, 1, ?)`, []any{hash, f.r3Member}},
		{`INSERT INTO users (id, username, password, is_admin, is_active) VALUES (4, 'gone', ?, 0, 0)`, []any{hash}},
		// A token is revoked when it predates the password change, so the fixture's
		// passwords are a day old rather than stamped in the second the tokens are signed.
		{`UPDATE users SET password_changed_at = datetime('now', '-1 day'), force_password_change = 0`, nil},
		{`UPDATE rank_permissions SET permissions = json_set(permissions,
			'$.manage_vs_points', json('true'), '$.manage_members', json('true'), '$.view_vs_points', json('true'))
		 WHERE rank = 'R4'`, nil},
		{`UPDATE rank_permissions SET permissions = json_set(permissions,
			'$.manage_vs_points', json('false'), '$.manage_members', json('false'))
		 WHERE rank = 'R3'`, nil},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	return f
}

// mobileToken signs a token for userID exactly as mobileLogin does, with the permission
// claims mobileLogin would mint for that user at this moment.
func mobileToken(t *testing.T, userID int) string {
	t.Helper()
	var username string
	var isAdmin bool
	var memberID *int
	var mid int
	if err := db.QueryRow(`SELECT username, is_admin, COALESCE(member_id, 0) FROM users WHERE id = ?`, userID).
		Scan(&username, &isAdmin, &mid); err != nil {
		t.Fatalf("mobileToken: user %d: %v", userID, err)
	}
	manageVS, manageMembers := isAdmin, isAdmin
	if mid > 0 {
		memberID = &mid
		if !isAdmin {
			var rank string
			db.QueryRow(`SELECT rank FROM members WHERE id = ?`, mid).Scan(&rank)
			p := getRankPermissions(rank)
			manageVS, manageMembers = p.ManageVSPoints, p.ManageMembers
		}
	}
	now := time.Now()
	claims := MobileTokenClaims{
		UserID: userID, Username: username, MemberID: memberID, IsAdmin: isAdmin,
		ManageVS: manageVS, ManageMembers: manageMembers,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(mobileTokenExpiry)),
			Subject:   username,
			Issuer:    "lastwar-alliance-manager",
		},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(tokenSecret())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// serveMobile sends one request through the real router with an optional bearer token.
func serveMobile(t *testing.T, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	buildRouter().ServeHTTP(w, req)
	return w
}

func decodeObject(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return out
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// assertKeys checks that obj carries every key in want (a contract may grow, never shrink).
func assertKeys(t *testing.T, what string, obj map[string]any, want ...string) {
	t.Helper()
	for _, k := range want {
		if _, ok := obj[k]; !ok {
			t.Errorf("%s: missing key %q (have %v)", what, k, keysOf(obj))
		}
	}
}

func countRows(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestMobileLoginContract(t *testing.T) {
	setupMobileTestDB(t)
	login := func(user, pass, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/mobile/login",
			strings.NewReader(`{"username":"`+user+`","password":"`+pass+`"}`))
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		buildRouter().ServeHTTP(w, req)
		return w
	}

	w := login("officer", mobileTestPassword, "10.150.0.1")
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	assertKeys(t, "login", out, "token", "expires_at", "user_id", "username", "member_id", "is_admin", "permissions")
	perms, _ := out["permissions"].(map[string]any)
	assertKeys(t, "login.permissions", perms, "manage_vs_points", "manage_members")
	if perms["manage_vs_points"] != true || perms["manage_members"] != true {
		t.Errorf("R4 permissions = %v", perms)
	}

	if w := login("officer", "wrong", "10.150.0.2"); w.Code != http.StatusUnauthorized {
		t.Errorf("bad password: %d", w.Code)
	}
	if w := login("gone", mobileTestPassword, "10.150.0.3"); w.Code != http.StatusForbidden {
		t.Errorf("deactivated: %d", w.Code)
	}
	db.Exec(`UPDATE users SET force_password_change = 1 WHERE id = 3`)
	if w := login("member", mobileTestPassword, "10.150.0.4"); w.Code != http.StatusForbidden {
		t.Errorf("forced password change: %d", w.Code)
	}

	// The limiter's burst, then a refusal, from one address.
	var last int
	for i := 0; i <= loginLimiterBurst; i++ {
		last = login("officer", "wrong", "10.150.0.9").Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("after the burst: %d, want 429", last)
	}
}

func TestMobileMembersContract(t *testing.T) {
	f := setupMobileTestDB(t)
	officer := 2
	member := 3
	seedAlias(t, f.other, "ZedMine", "personal", &officer)
	seedAlias(t, f.other, "ZedTheirs", "personal", &member)
	seedAlias(t, f.other, "ZedG", "global", nil)

	if w := serveMobile(t, "GET", "/api/mobile/members", "", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", w.Code)
	}
	w := serveMobile(t, "GET", "/api/mobile/members", "", mobileToken(t, officer))
	if w.Code != http.StatusOK {
		t.Fatalf("members: %d %s", w.Code, w.Body.String())
	}
	var roster []map[string]any
	json.Unmarshal(w.Body.Bytes(), &roster)
	byName := map[string]map[string]any{}
	for _, m := range roster {
		assertKeys(t, "member", m, "id", "name", "rank", "aliases")
		byName[m["name"].(string)] = m
	}
	if _, ok := byName["Gone Player"]; ok {
		t.Error("an EX member is in the roster")
	}
	zed := byName["Zed Player"]
	if zed == nil {
		t.Fatal("Zed Player missing")
	}
	var aliases []string
	for _, a := range zed["aliases"].([]any) {
		aliases = append(aliases, a.(map[string]any)["alias"].(string))
	}
	sort.Strings(aliases)
	if strings.Join(aliases, ",") != "ZedG,ZedMine" {
		t.Errorf("aliases = %v, want ZedG and the caller's own personal only", aliases)
	}
}

func TestMobilePreviewContract(t *testing.T) {
	f := setupMobileTestDB(t)
	seedAlias(t, f.other, "Zeddy", "global", nil)
	seedMember(t, "Renée", "R1")
	before := countRows(t, `SELECT COUNT(*) FROM vs_points`) + countRows(t, `SELECT COUNT(*) FROM power_history`)

	body := `{"week_date":"2026-10-05","entries":[
		{"name":"Zed Player","score":100,"category":"monday"},
		{"name":"Zeddy","score":200,"category":"monday"},
		{"name":"Renee","score":300,"category":"monday"},
		{"name":"Nobody Here","score":400,"category":"monday"}]}`
	w := serveMobile(t, "POST", "/api/mobile/preview", body, mobileToken(t, 2))
	if w.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	assertKeys(t, "preview", out, "week_date", "matched", "unresolved", "all_members",
		"total_submitted", "total_matched", "total_unresolved")
	types := map[string]string{}
	for _, m := range out["matched"].([]any) {
		mm := m.(map[string]any)
		assertKeys(t, "match", mm, "original_name", "matched_member", "match_type", "category", "score")
		types[mm["original_name"].(string)] = mm["match_type"].(string)
	}
	if types["Zed Player"] != "exact" || types["Zeddy"] != "global_alias" || types["Renee"] != "folded" {
		t.Errorf("match types = %v", types)
	}
	if out["total_unresolved"].(float64) != 1 {
		t.Errorf("unresolved = %v", out["unresolved"])
	}
	after := countRows(t, `SELECT COUNT(*) FROM vs_points`) + countRows(t, `SELECT COUNT(*) FROM power_history`)
	if after != before {
		t.Error("preview wrote rows")
	}

	// R3 holds neither permission.
	if w := serveMobile(t, "POST", "/api/mobile/preview", body, mobileToken(t, 3)); w.Code != http.StatusForbidden {
		t.Errorf("R3 preview: %d", w.Code)
	}
}

func TestMobileCommitContract(t *testing.T) {
	f := setupMobileTestDB(t)
	member := 3
	seedAlias(t, f.other, "zedtext", "personal", &member) // another user's personal alias
	if _, err := db.Exec(`INSERT INTO vs_points (member_id, week_date, monday) VALUES (?, '2026-10-05', 11)`, f.other); err != nil {
		t.Fatal(err)
	}

	body := `{"week_date":"2026-10-06","records":[
		{"member_id":` + strconv.Itoa(f.other) + `,"original_name":"Zed Player","category":"tuesday","score":22},
		{"member_id":` + strconv.Itoa(f.other) + `,"original_name":"Zed Player","category":"power","score":5000000},
		{"member_id":` + strconv.Itoa(f.other) + `,"original_name":"Zed Player","category":"kills","score":123},
		{"member_id":` + strconv.Itoa(f.other) + `,"original_name":"Zed Player","category":"bogus","score":1},
		{"member_id":` + strconv.Itoa(f.former) + `,"original_name":"Gone Player","category":"power","score":1}],
		"save_aliases":[{"failed_alias":"ZEDTEXT","member_id":` + strconv.Itoa(f.r3Member) + `,"category":"ocr"}]}`
	w := serveMobile(t, "POST", "/api/mobile/commit", body, mobileToken(t, 2))
	if w.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	assertKeys(t, "commit", out, "message", "vs_records_saved", "power_records_saved",
		"kill_records_saved", "aliases_saved", "errors")
	if out["vs_records_saved"].(float64) != 1 || out["power_records_saved"].(float64) != 1 ||
		out["kill_records_saved"].(float64) != 1 || out["aliases_saved"].(float64) != 1 {
		t.Errorf("counts = %v", out)
	}
	if errs := out["errors"].([]any); len(errs) != 2 {
		t.Errorf("errors = %v, want the bogus category and the archived member", errs)
	}

	// Days merge into the existing week (the 6th snaps to Monday the 5th).
	var mon, tue int
	db.QueryRow(`SELECT monday, tuesday FROM vs_points WHERE member_id = ? AND week_date = '2026-10-05'`, f.other).Scan(&mon, &tue)
	if mon != 11 || tue != 22 {
		t.Errorf("week = mon %d tue %d, want 11 and 22", mon, tue)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM power_history WHERE member_id = ? AND source = 'mobile'`, f.other); n != 1 {
		t.Errorf("power rows stamped mobile = %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM kill_history WHERE member_id = ? AND source = 'mobile'`, f.other); n != 1 {
		t.Errorf("kill rows stamped mobile = %d", n)
	}
	// Pinned: today a global/OCR save deletes every alias with that text, another user's
	// personal one included.
	if n := countRows(t, `SELECT COUNT(*) FROM member_aliases WHERE LOWER(alias) = 'zedtext' AND category = 'personal'`); n != 0 {
		t.Errorf("another user's personal alias survived (%d) — the pinned behaviour changed", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM member_aliases WHERE alias = 'ZEDTEXT' AND category = 'ocr' AND member_id = ?`, f.r3Member); n != 1 {
		t.Errorf("ocr alias rows = %d", n)
	}
	for _, et := range []string{"vs_points", "power_records", "kill_count"} {
		if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = ? AND action = 'imported'`, et); n != 1 {
			t.Errorf("activity rows for %s = %d", et, n)
		}
	}

	// A global alias without manage_members lands in errors[]. R4 loses it for this check.
	db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_members', json('false')) WHERE rank = 'R4'`)
	tok := mobileToken(t, 2)
	w = serveMobile(t, "POST", "/api/mobile/commit", `{"week_date":"2026-10-05","records":[],
		"save_aliases":[{"failed_alias":"Zz","member_id":`+strconv.Itoa(f.other)+`,"category":"global"}]}`, tok)
	out = decodeObject(t, w)
	if out["aliases_saved"].(float64) != 0 || len(out["errors"].([]any)) != 1 {
		t.Errorf("global alias without manage_members: %v", out)
	}
}
