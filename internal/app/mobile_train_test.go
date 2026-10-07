package app

import (
	"net/http"
	"strconv"
	"testing"
)

// Train logs (Project 15, C12): one validated, transactional write for the web create,
// the web update and the mobile create.

func TestTrainLogWrites(t *testing.T) {
	f := setupMobileTestDB(t)
	setRankPerm(t, "R4", "manage_train", true)
	setRankPerm(t, "R4", "view_train", true)
	tok := mobileToken(t, 2)
	admin := sessionCookie(t, 1, "")
	today := gameDate()
	zed := strconv.Itoa(f.other)
	logs := func() int { return countRows(t, `SELECT COUNT(*) FROM train_logs`) }

	// A stale conductor id is refused with nothing written, on all three paths.
	stale := `{"date":"` + today + `","train_type":"FREE","conductor_id":99999}`
	if w := serveMobile(t, "POST", "/api/mobile/train-logs", stale, tok); w.Code != http.StatusBadRequest {
		t.Errorf("mobile stale: %d", w.Code)
	}
	if w := serveRouter(t, "POST", "/api/train-logs", stale, admin); w.Code != http.StatusBadRequest {
		t.Errorf("web stale: %d", w.Code)
	}
	if logs() != 0 {
		t.Fatal("a refused create wrote a row")
	}
	// A former member can't conduct; a future date is refused.
	if w := serveMobile(t, "POST", "/api/mobile/train-logs", `{"date":"`+today+`","train_type":"FREE","conductor_id":`+strconv.Itoa(f.former)+`}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("former conductor: %d", w.Code)
	}
	if w := serveMobile(t, "POST", "/api/mobile/train-logs", `{"date":"2099-01-01","train_type":"FREE","conductor_id":`+zed+`}`, tok); w.Code != http.StatusBadRequest {
		t.Errorf("future date: %d", w.Code)
	}

	// A retried mobile create within the window returns the first log.
	body := `{"date":"` + today + `","train_type":"PURCHASED","conductor_id":` + zed + `}`
	first := decodeObject(t, serveMobile(t, "POST", "/api/mobile/train-logs", body, tok))
	again := decodeObject(t, serveMobile(t, "POST", "/api/mobile/train-logs", body, tok))
	id := first["train_log"].(map[string]any)["id"]
	if first["duplicate"] != false || again["duplicate"] != true || again["train_log"].(map[string]any)["id"] != id || logs() != 1 {
		t.Errorf("retry: first %v, again %v, %d logs", first, again, logs())
	}
	// A different VIP is a different log; the conductor's second purchased train today
	// writes, and trips the daily-limit warning when it is due.
	w := serveMobile(t, "POST", "/api/mobile/train-logs", `{"date":"`+today+`","train_type":"PURCHASED","conductor_id":`+zed+`,
		"vip_id":`+strconv.Itoa(f.r3Member)+`,"vip_type":"SPECIAL_GUEST"}`, tok)
	if w.Code != http.StatusOK || logs() != 2 {
		t.Errorf("second purchased: %d %s (%d logs)", w.Code, w.Body.String(), logs())
	}
	db.Exec(`UPDATE settings SET train_purchased_daily_limit = 1 WHERE id = 1`)
	w = serveMobile(t, "POST", "/api/mobile/train-logs", `{"date":"`+today+`","train_type":"PURCHASED","conductor_id":`+strconv.Itoa(f.r4Member)+`}`, tok)
	if out := decodeObject(t, w); out["limit_warning"] != true {
		t.Errorf("limit warning = %v", out)
	}

	// The web update refuses a stale conductor without touching the row, and 404s a
	// missing log; it may keep a conductor who has since left.
	logID := strconv.Itoa(int(id.(float64)))
	if w := serveRouter(t, "PUT", "/api/train-logs/"+logID, stale, admin); w.Code != http.StatusBadRequest {
		t.Errorf("web update stale: %d", w.Code)
	}
	if w := serveRouter(t, "PUT", "/api/train-logs/99999", body, admin); w.Code != http.StatusNotFound {
		t.Errorf("web update missing: %d", w.Code)
	}
	db.Exec(`UPDATE members SET rank = 'EX' WHERE id = ?`, f.other)
	if w := serveRouter(t, "PUT", "/api/train-logs/"+logID, `{"date":"`+today+`","train_type":"PURCHASED","conductor_id":`+zed+`,"notes":"kept"}`, admin); w.Code != http.StatusOK {
		t.Errorf("edit a departed conductor's log: %d %s", w.Code, w.Body.String())
	}
	// Three creates (the retry logs nothing), merged by the activity log's create batching.
	if n := countRows(t, `SELECT COALESCE(SUM(entity_count), 0) FROM activity_log WHERE entity_type = 'train_log' AND action = 'created'`); n != 3 {
		t.Errorf("train creates logged = %d, want 3", n)
	}
}
