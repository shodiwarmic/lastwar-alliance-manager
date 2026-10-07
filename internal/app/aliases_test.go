package app

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The one alias helper (private-docs 195): scopes, no-ops, healing, and one audit row per
// change written inside the caller's transaction.

var testActor = aliasActor{UserID: 2, Username: "officer", Via: "test"}

func saveAlias(t *testing.T, w aliasWrite) (*aliasChange, error) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ch, err := saveAliasTx(tx, w)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ch, nil
}

func aliasRowsFor(t *testing.T, text string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT a.category, m.name, COALESCE(a.user_id, 0) FROM member_aliases a
		JOIN members m ON m.id = a.member_id WHERE LOWER(a.alias) = LOWER(?) ORDER BY a.category, m.name`, text)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var cat, name string
		var uid int
		rows.Scan(&cat, &name, &uid)
		s := cat + ":" + name
		if uid != 0 {
			s += "@" + strconv.Itoa(uid)
		}
		out = append(out, s)
	}
	return out
}

func lastAliasActivity(t *testing.T) (action, name, details string) {
	t.Helper()
	db.QueryRow(`SELECT action, entity_name, details FROM activity_log WHERE entity_type = 'alias'
		ORDER BY id DESC LIMIT 1`).Scan(&action, &name, &details)
	return
}

func TestSaveAliasScopes(t *testing.T) {
	f := setupMobileTestDB(t)
	a, b := f.other, f.r3Member // "Zed Player", "Member Three"
	member := 3

	// created
	ch, err := saveAlias(t, aliasWrite{MemberID: a, Alias: " Zz ", Category: "global", Actor: testActor})
	if err != nil || ch == nil || ch.Action != "created" {
		t.Fatalf("create: %+v %v", ch, err)
	}
	if act, name, det := lastAliasActivity(t); act != "created" || name != "Zz" || det != "Zed Player (global) · via test" {
		t.Errorf("activity = %q %q %q", act, name, det)
	}

	// unchanged: same member and category, any case
	if ch, err := saveAlias(t, aliasWrite{MemberID: a, Alias: "zz", Category: "global", Actor: testActor}); err != nil || ch != nil {
		t.Errorf("same mapping: %+v %v, want no change", ch, err)
	}

	// updated: re-pointed, leaving another user's personal alias alone
	seedAlias(t, a, "zz", "personal", &member)
	ch, err = saveAlias(t, aliasWrite{MemberID: b, Alias: "ZZ", Category: "global", Actor: testActor})
	if err != nil || ch == nil || ch.Action != "updated" {
		t.Fatalf("re-point: %+v %v", ch, err)
	}
	if got := strings.Join(aliasRowsFor(t, "zz"), ","); got != "global:Member Three,personal:Zed Player@3" {
		t.Errorf("rows after re-point = %s", got)
	}
	if _, _, det := lastAliasActivity(t); !strings.HasSuffix(det, "; re-pointed from Zed Player") {
		t.Errorf("re-point details = %q", det)
	}

	// updated: category change for the same member
	seedAlias(t, a, "Cat", "ocr", nil)
	if ch, err := saveAlias(t, aliasWrite{MemberID: a, Alias: "Cat", Category: "global", Actor: testActor}); err != nil || ch == nil || ch.PrevCategory != "ocr" {
		t.Fatalf("category change: %+v %v", ch, err)
	}
	if _, _, det := lastAliasActivity(t); !strings.HasSuffix(det, "; category ocr → global") {
		t.Errorf("category details = %q", det)
	}

	// OCR over another member's global: refused without manage_members, replaces with it.
	seedAlias(t, a, "Gx", "global", nil)
	_, err = saveAlias(t, aliasWrite{MemberID: b, Alias: "gx", Category: "ocr", Actor: testActor})
	if !isAliasRefusal(err) || !strings.Contains(err.Error(), "global alias for Zed Player") {
		t.Errorf("ocr over global: %v", err)
	}
	if got := strings.Join(aliasRowsFor(t, "gx"), ","); got != "global:Zed Player" {
		t.Errorf("refused write changed rows: %s", got)
	}
	if ch, err := saveAlias(t, aliasWrite{MemberID: b, Alias: "gx", Category: "ocr", Actor: testActor, MayOverrideGlobal: true}); err != nil || ch == nil {
		t.Fatalf("ocr over global with manage_members: %+v %v", ch, err)
	}
	if got := strings.Join(aliasRowsFor(t, "gx"), ","); got != "ocr:Member Three" {
		t.Errorf("override rows = %s", got)
	}

	// OCR matching the global's member: no-op.
	seedAlias(t, a, "Same", "global", nil)
	if ch, err := saveAlias(t, aliasWrite{MemberID: a, Alias: "same", Category: "ocr", Actor: testActor}); err != nil || ch != nil {
		t.Errorf("ocr matching global: %+v %v", ch, err)
	}

	// A global write replaces OCR rows; an OCR write leaves a personal alone.
	seedAlias(t, b, "Mix", "ocr", nil)
	seedAlias(t, b, "Mix", "personal", &member)
	saveAlias(t, aliasWrite{MemberID: a, Alias: "Mix", Category: "ocr", Actor: testActor})
	if got := strings.Join(aliasRowsFor(t, "mix"), ","); got != "ocr:Zed Player,personal:Member Three@3" {
		t.Errorf("ocr write rows = %s", got)
	}

	// personal: only its owner's rows
	seedAlias(t, b, "Pers", "personal", &member)
	saveAlias(t, aliasWrite{MemberID: a, Alias: "Pers", Category: "personal", Actor: testActor})
	if got := strings.Join(aliasRowsFor(t, "pers"), ","); got != "personal:Member Three@3,personal:Zed Player@2" {
		t.Errorf("personal rows = %s", got)
	}

	// duplicates heal to one
	seedAlias(t, a, "Dup", "global", nil)
	seedAlias(t, a, "dup", "global", nil)
	seedAlias(t, b, "DUP", "ocr", nil)
	saveAlias(t, aliasWrite{MemberID: a, Alias: "Dup", Category: "global", Actor: testActor})
	if got := strings.Join(aliasRowsFor(t, "dup"), ","); got != "global:Zed Player" {
		t.Errorf("healed rows = %s", got)
	}

	// refusals
	for _, w := range []aliasWrite{
		{MemberID: a, Alias: "  ", Category: "global", Actor: testActor},
		{MemberID: a, Alias: "x", Category: "bogus", Actor: testActor},
		{MemberID: a, Alias: "x", Category: "personal"},
		{MemberID: 99999, Alias: "x", Category: "global", Actor: testActor},
	} {
		if _, err := saveAlias(t, w); !isAliasRefusal(err) {
			t.Errorf("%+v: %v, want a refusal", w, err)
		}
	}
}

func TestDeleteAliasesAndAudit(t *testing.T) {
	f := setupMobileTestDB(t)
	member := 3
	seedAlias(t, f.other, "Gone", "ocr", nil)
	seedAlias(t, f.other, "gone", "personal", &member)
	tx, _ := db.Begin()
	n, err := deleteAliasesTx(tx, "GONE", []string{"ocr"}, testActor)
	tx.Commit()
	if err != nil || n != 1 {
		t.Fatalf("delete: %d %v", n, err)
	}
	if got := strings.Join(aliasRowsFor(t, "gone"), ","); got != "personal:Zed Player@3" {
		t.Errorf("rows = %s", got)
	}
	if act, _, det := lastAliasActivity(t); act != "deleted" || det != "Zed Player (ocr) · via test" {
		t.Errorf("activity = %q %q", act, det)
	}

	// Two creates in a row are two activity rows (alias is never batched).
	before := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'alias'`)
	saveAlias(t, aliasWrite{MemberID: f.other, Alias: "One", Category: "global", Actor: testActor})
	saveAlias(t, aliasWrite{MemberID: f.other, Alias: "Two", Category: "global", Actor: testActor})
	if n := countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'alias'`); n != before+2 {
		t.Errorf("activity rows %d → %d, want two more", before, n)
	}

	// The audit row failing rolls the alias write back with it.
	if _, err := db.Exec(`ALTER TABLE activity_log RENAME TO activity_log_away`); err != nil {
		t.Fatal(err)
	}
	if _, err := saveAlias(t, aliasWrite{MemberID: f.other, Alias: "NoRecord", Category: "global", Actor: testActor}); err == nil || isAliasRefusal(err) {
		t.Errorf("save without an activity log: %v, want a database error", err)
	}
	db.Exec(`ALTER TABLE activity_log_away RENAME TO activity_log`)
	if got := aliasRowsFor(t, "NoRecord"); len(got) != 0 {
		t.Errorf("alias written without its record: %v", got)
	}
}

// aliasActivityVia counts alias activity rows whose details end with "via <path>"
// (optionally followed by an update's suffix).
func aliasActivityVia(t *testing.T, action, text, via string) int {
	t.Helper()
	return countRows(t, `SELECT COUNT(*) FROM activity_log WHERE entity_type = 'alias' AND action = ?
		AND entity_name = ? AND (details LIKE ? OR details LIKE ?)`, action, text, "% · via "+via, "% · via "+via+";%")
}

// Every write site goes through the helper and records its path.
func TestAliasWriteSitesLogTheirPath(t *testing.T) {
	f := setupMobileTestDB(t)
	admin := sessionCookie(t, 1, "")
	other := strconv.Itoa(f.other)

	// VS import commit
	w := serveRouter(t, "POST", "/api/vs-points/import/commit", `{"week_date":"2026-10-05","records":[],
		"save_aliases":[{"failed_alias":"ViaVS","member_id":`+other+`,"category":"ocr"}]}`, admin)
	if w.Code != http.StatusOK || aliasActivityVia(t, "created", "ViaVS", "VS import") != 1 {
		t.Errorf("VS import: %d %s", w.Code, w.Body.String())
	}

	// Mobile commit
	w = serveMobile(t, "POST", "/api/mobile/commit", `{"week_date":"2026-10-05","records":[],
		"save_aliases":[{"failed_alias":"ViaMobile","member_id":`+other+`,"category":"personal"}]}`, mobileToken(t, 2))
	if w.Code != http.StatusOK || aliasActivityVia(t, "created", "ViaMobile", "mobile") != 1 {
		t.Errorf("mobile: %d %s", w.Code, w.Body.String())
	}

	// Member modal add and delete
	if w := serveRouter(t, "POST", "/api/members/"+other+"/aliases", `{"alias":"ViaModal","is_global":true}`, admin); w.Code != http.StatusCreated {
		t.Errorf("modal add: %d", w.Code)
	}
	var id int
	db.QueryRow(`SELECT id FROM member_aliases WHERE alias = 'ViaModal'`).Scan(&id)
	if w := serveRouter(t, "DELETE", "/api/aliases/"+strconv.Itoa(id), "", admin); w.Code != http.StatusOK {
		t.Errorf("modal delete: %d", w.Code)
	}
	if aliasActivityVia(t, "created", "ViaModal", "Members page") != 1 || aliasActivityVia(t, "deleted", "ViaModal", "Members page") != 1 {
		t.Error("modal add/delete rows missing")
	}

	// Renames on the Members page: the old name becomes a global alias, re-pointing an
	// existing one instead of adding a duplicate.
	seedAlias(t, f.r3Member, "Zed Player", "global", nil)
	if w := serveRouter(t, "PUT", "/api/members/"+other, `{"name":"Zed Renamed","rank":"R2","eligible":true}`, admin); w.Code != http.StatusOK {
		t.Errorf("rename: %d %s", w.Code, w.Body.String())
	}
	if got := strings.Join(aliasRowsFor(t, "Zed Player"), ","); got != "global:Zed Renamed" {
		t.Errorf("rename alias rows = %s", got)
	}
	if aliasActivityVia(t, "updated", "Zed Player", "rename on the Members page") != 1 {
		t.Error("rename alias row missing")
	}
	if w := serveRouter(t, "PUT", "/api/former-members/"+strconv.Itoa(f.former), `{"name":"Gone Renamed","leave_reason":"x"}`, admin); w.Code != http.StatusNoContent {
		t.Errorf("former rename: %d", w.Code)
	}
	if aliasActivityVia(t, "created", "Gone Player", "rename on the Members page") != 1 {
		t.Error("former rename alias row missing")
	}

	// LastRank commit: a rename (old name → global alias, an OCR alias spelling the new
	// name dropped) and an unmatched name aliased.
	r4 := strconv.Itoa(f.r4Member)
	seedAlias(t, f.r4Member, "Officer Five", "ocr", nil)
	w = serveRouter(t, "POST", "/api/lastrank/commit", `{"members":[{"member_id":`+r4+`,"name_action":"rename","name_new":"Officer Five"}],
		"unmatched":[{"lastrank_name":"LR Name","action":"alias","member_id":`+r4+`}]}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("lastrank commit: %d %s", w.Code, w.Body.String())
	}
	if aliasActivityVia(t, "created", "Officer Four", "LastRank") != 1 ||
		aliasActivityVia(t, "deleted", "Officer Five", "LastRank") != 1 ||
		aliasActivityVia(t, "created", "LR Name", "LastRank") != 1 {
		t.Error("LastRank commit rows missing")
	}

	// LastRank review queue: accepting a name proposal as an alias.
	if _, err := db.Exec(`INSERT INTO lastrank_pending_changes (kind, subject_key, member_id, proposed_value, fingerprint)
		VALUES ('name', 'k1', ?, 'Queued Name', 'f1')`, f.r4Member); err != nil {
		t.Fatal(err)
	}
	var pid int
	db.QueryRow(`SELECT id FROM lastrank_pending_changes WHERE subject_key = 'k1'`).Scan(&pid)
	w = serveRouter(t, "POST", "/api/lastrank/review/action", `{"ids":[`+strconv.Itoa(pid)+`],"action":"apply"}`, admin)
	if w.Code != http.StatusOK || aliasActivityVia(t, "created", "Queued Name", "LastRank review") != 1 {
		t.Errorf("review: %d %s", w.Code, w.Body.String())
	}
}

// The Season Hub contributions import saves a resolved name's alias through the helper
// (driven through the OCR stub).
func TestSeasonHubImportAliasThroughHelper(t *testing.T) {
	srv, _ := fakeOCRService(t, 200, `{"schema_version": 1, "results": {"mutual_assistance_weekly": [{"player_name": "Unknown Guy", "score": 50}]}}`, "")
	setupOCRTestDB(t, OCRBackendCloud, srv.URL)
	f := seedMobileFixture(t)
	res, err := db.Exec(`INSERT INTO seasons (name, season_number, start_date) VALUES ('S', 3, '2026-09-01')`)
	if err != nil {
		t.Fatal(err)
	}
	sid, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO season_trackables (season_id, key, label) VALUES (?, 'mutual_assistance', 'MA')`, sid); err != nil {
		t.Fatal(err)
	}
	seedAlias(t, f.r3Member, "Unknown Guy", "global", nil) // an OCR save may not demote it

	post := func(alias string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, _ := mw.CreateFormFile("images", "f.png")
		part.Write([]byte("png"))
		mw.WriteField("season_id", strconv.FormatInt(sid, 10))
		mw.WriteField("week_number", "1")
		mw.WriteField("category", "mutual_assistance_weekly")
		mw.WriteField("commit", "true")
		mw.WriteField("resolved_mappings", `[{"original_name":"`+alias+`","points":50,"member_id":`+strconv.Itoa(f.other)+`,"alias_type":"ocr"}]`)
		mw.Close()
		req := httptest.NewRequest("POST", "/api/season-hub/contributions/import", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.AddCookie(sessionCookie(t, 3, "")) // R3: manage_season_hub granted below, no manage_members
		w := httptest.NewRecorder()
		buildRouter().ServeHTTP(w, req)
		return w
	}
	db.Exec(`UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_season_hub', json('true')) WHERE rank = 'R3'`)

	w := post("Unknown Guy")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "global alias for Member Three") {
		t.Errorf("refused alias: %d %s", w.Code, w.Body.String())
	}
	if got := strings.Join(aliasRowsFor(t, "Unknown Guy"), ","); got != "global:Member Three" {
		t.Errorf("rows = %s", got)
	}
	w = post("Other Guy")
	if w.Code != http.StatusOK || aliasActivityVia(t, "created", "Other Guy", "Season Hub import") != 1 {
		t.Errorf("season hub alias: %d %s", w.Code, w.Body.String())
	}
}
