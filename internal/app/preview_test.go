package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// setupPreviewTestDB: an unlinked admin (1), a linked admin (2, member R5), and an R3
// officer (3). R3's permissions are pinned so the assertions do not depend on the
// migration defaults: view_schedule and view_activity on, manage_settings off.
func setupPreviewTestDB(t *testing.T) {
	t.Helper()
	setupNameMatchTestDB(t)
	if store == nil {
		initSessionStore()
	}
	r5 := seedMember(t, "Leader", "R5")
	r3 := seedMember(t, "Officer", "R3")
	for _, q := range []string{
		`INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`,
		`INSERT INTO users (id, username, password, is_admin, is_active, member_id) VALUES (2, 'linkedadmin', 'x', 1, 1, ` + strconv.Itoa(r5) + `)`,
		`INSERT INTO users (id, username, password, is_admin, is_active, member_id) VALUES (3, 'officer', 'x', 0, 1, ` + strconv.Itoa(r3) + `)`,
		`UPDATE rank_permissions SET permissions = json_set(permissions,
			'$.view_schedule', json('true'), '$.view_activity', json('true'), '$.manage_settings', json('false'))
		 WHERE rank = 'R3'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// sessionCookie returns a session cookie naming userID, with an optional preview flag.
func sessionCookie(t *testing.T, userID int, preview string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	session, _ := store.Get(req, "session")
	session.Values["user_id"] = userID
	if preview != "" {
		session.Values[previewRankKey] = preview
	}
	if err := session.Save(req, w); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func sessionUser(t *testing.T, c *http.Cookie) *AuthUser {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	session, _ := store.Get(req, "session")
	return loadSessionUser(session)
}

// serveAs runs h behind authMiddleware with the given cookie and returns the recorder.
func serveAs(c *http.Cookie, method, path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if c != nil {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	authMiddleware(h)(w, req)
	return w
}

// lastCookie returns the last session cookie a response set (authMiddleware's rolling
// save comes first, the handler's own save after it — a browser keeps the last), or the
// one passed in.
func lastCookie(w *httptest.ResponseRecorder, prev *http.Cookie) *http.Cookie {
	got := prev
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			got = c
		}
	}
	return got
}

func TestLoadSessionUserAppliesAPreview(t *testing.T) {
	setupPreviewTestDB(t)
	u := sessionUser(t, sessionCookie(t, 1, "R3"))
	if u == nil || u.IsAdmin || u.Rank != "R3" || !u.RealIsAdmin || u.PreviewRank != "R3" {
		t.Fatalf("admin previewing R3 resolved to %+v", u)
	}
	if u.MemberID != nil {
		t.Errorf("an unlinked admin gained a member: %v", *u.MemberID)
	}

	// A linked admin keeps their own member: "own" data is unchanged by a preview.
	u = sessionUser(t, sessionCookie(t, 2, "R1"))
	if u == nil || u.MemberID == nil || u.Rank != "R1" || u.IsAdmin {
		t.Fatalf("linked admin previewing R1 resolved to %+v", u)
	}

	// No flag: an ordinary admin.
	u = sessionUser(t, sessionCookie(t, 1, ""))
	if !u.IsAdmin || !u.RealIsAdmin || u.PreviewRank != "" {
		t.Errorf("admin without a preview resolved to %+v", u)
	}
}

func TestPreviewFlagOnANonAdminIsIgnored(t *testing.T) {
	setupPreviewTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(sessionCookie(t, 3, "R5"))
	session, _ := store.Get(req, "session")
	u := loadSessionUser(session)
	if u.IsAdmin || u.RealIsAdmin || u.PreviewRank != "" || u.Rank != "R3" {
		t.Errorf("officer with a planted R5 flag resolved to %+v", u)
	}
	if _, still := session.Values[previewRankKey]; still {
		t.Error("the stale flag was not deleted")
	}

	// An unknown rank on an admin's session is ignored too.
	u = sessionUser(t, sessionCookie(t, 1, "R9"))
	if !u.IsAdmin || u.PreviewRank != "" {
		t.Errorf("admin with flag R9 resolved to %+v", u)
	}
}

func TestPreviewFollowsTheRanksPermissions(t *testing.T) {
	setupPreviewTestDB(t)
	c := sessionCookie(t, 1, "R3")
	u := sessionUser(t, c)
	if !userHasPermission(u, "view_schedule") {
		t.Error("unlinked admin previewing R3 denied view_schedule, which R3 holds")
	}
	if userHasPermission(u, "manage_settings") {
		t.Error("unlinked admin previewing R3 granted manage_settings, which R3 lacks")
	}
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }
	if w := serveAs(c, http.MethodGet, "/x", "", requirePermission("manage_settings", ok)); w.Code != http.StatusForbidden {
		t.Errorf("R3-denied route under preview: %d, want 403", w.Code)
	}
	if w := serveAs(c, http.MethodGet, "/x", "", adminMiddleware(ok)); w.Code != http.StatusForbidden {
		t.Errorf("admin-only route under preview: %d, want 403", w.Code)
	}
	if w := serveAs(c, http.MethodGet, "/api/activity", "", getActivityLog); w.Code != http.StatusOK {
		t.Errorf("activity under an R3 preview (R3 has view_activity): %d", w.Code)
	}
	if _, err := db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_activity', json('false')) WHERE rank = 'R3'`); err != nil {
		t.Fatal(err)
	}
	if w := serveAs(c, http.MethodGet, "/api/activity", "", getActivityLog); w.Code != http.StatusForbidden {
		t.Errorf("activity under an R3 preview without view_activity: %d, want 403", w.Code)
	}

	// The dashboard's leadership card follows the previewed rank too.
	data := getPageData(func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(c)
		return r
	}(), "Dashboard", "dashboard")
	if data.IsAdmin || data.Rank != "R3" || data.PreviewRank != "R3" || !data.CanExitPreview {
		t.Fatalf("page data under preview: IsAdmin=%v Rank=%q PreviewRank=%q CanExit=%v",
			data.IsAdmin, data.Rank, data.PreviewRank, data.CanExitPreview)
	}
	for _, card := range allowedCards(data) {
		if card.ID == "leader-flags" {
			t.Error("allowedCards for a previewed R3 includes leader-flags")
		}
	}
}

func TestPreviewStartAndExit(t *testing.T) {
	setupPreviewTestDB(t)
	start := requireRealAdmin(startRankPreview)
	end := requireRealAdmin(endRankPreview)

	// A non-admin can do neither.
	officer := sessionCookie(t, 3, "")
	if w := serveAs(officer, http.MethodPost, "/api/preview-rank", `{"rank":"R5"}`, start); w.Code != http.StatusForbidden {
		t.Errorf("officer start: %d, want 403", w.Code)
	}
	if w := serveAs(officer, http.MethodDelete, "/api/preview-rank", "", end); w.Code != http.StatusForbidden {
		t.Errorf("officer exit: %d, want 403", w.Code)
	}

	admin := sessionCookie(t, 1, "")
	if w := serveAs(admin, http.MethodPost, "/api/preview-rank", `{"rank":"R7"}`, start); w.Code != http.StatusBadRequest {
		t.Errorf("unknown rank: %d, want 400", w.Code)
	}
	w := serveAs(admin, http.MethodPost, "/api/preview-rank", `{"rank":"R3"}`, start)
	if w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	c := lastCookie(w, admin)
	if u := sessionUser(t, c); u.PreviewRank != "R3" {
		t.Fatalf("after start: %+v", u)
	}

	// Switching while previewing goes through the RealIsAdmin path (IsAdmin is false).
	w = serveAs(c, http.MethodPost, "/api/preview-rank", `{"rank":"R1"}`, start)
	if w.Code != http.StatusOK {
		t.Fatalf("switch while previewing: %d", w.Code)
	}
	c = lastCookie(w, c)

	// The started rows are sensitive, so the previewed rank's own activity view does not
	// show them: a preview looks like the rank, reads included. They appear after Exit.
	if _, err := db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_activity', json('true')) WHERE rank = 'R1'`); err != nil {
		t.Fatal(err)
	}
	w = serveAs(c, http.MethodGet, "/api/activity", "", getActivityLog)
	if strings.Contains(w.Body.String(), "rank_preview") {
		t.Error("a previewing admin sees the sensitive rank_preview rows")
	}

	w = serveAs(c, http.MethodDelete, "/api/preview-rank", "", end)
	if w.Code != http.StatusNoContent {
		t.Fatalf("exit while previewing: %d", w.Code)
	}
	c = lastCookie(w, c)
	if u := sessionUser(t, c); u.PreviewRank != "" || !u.IsAdmin {
		t.Fatalf("after exit: %+v", u)
	}
	w = serveAs(c, http.MethodGet, "/api/activity", "", getActivityLog)
	if n := strings.Count(w.Body.String(), `"rank_preview"`); n != 3 {
		t.Errorf("after exit the admin sees %d rank_preview rows, want 3 (start, switch, end)\n%s", n, w.Body.String())
	}
}
