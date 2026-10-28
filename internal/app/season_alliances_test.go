package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"lastwar-alliance/internal/lastrank"
)

func putSeasonAlliance(t *testing.T, body string, actor *AuthUser) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	handleSeasonAlliancePut(rr, shReq(http.MethodPut, body, nil, actor))
	return rr
}

func TestSeasonFactionsAndAllianceRows(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-02", true, false)
	seedSeasonRow(t, 2, 5, "2031-01-05", false, false)

	// Factions: the key is unique per season even for a repeated label; one is ours.
	addFaction := func(season int, label string, ours bool) int {
		rr := httptest.NewRecorder()
		handleSeasonFactionCreate(rr, shReq(http.MethodPost,
			`{"season_id":`+strconv.Itoa(season)+`,"label":"`+label+`","is_ours":`+strconv.FormatBool(ours)+`}`, nil, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("faction: %d %s", rr.Code, rr.Body.String())
		}
		var out struct{ ID int }
		json.Unmarshal(rr.Body.Bytes(), &out)
		return out.ID
	}
	kage := addFaction(1, "Kage no Sato", true)
	addFaction(1, "Koubutai", false)
	addFaction(1, "Koubutai", true)
	if n := countT(t, `SELECT COUNT(DISTINCT key) FROM season_factions WHERE season_id = 1`); n != 3 {
		t.Errorf("%d distinct faction keys, want 3", n)
	}
	if n := countT(t, `SELECT COUNT(*) FROM season_factions WHERE season_id = 1 AND is_ours = 1`); n != 1 {
		t.Errorf("%d factions flagged ours, want 1", n)
	}

	// A row by tag and server mints the registry identity; unknown is the default.
	rr := putSeasonAlliance(t, `{"season_id":1,"tag":"WAR","server":1712,"name":"War Machine"}`, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rr.Code, rr.Body.String())
	}
	var rel string
	db.QueryRow(`SELECT relationship FROM season_alliances WHERE season_id = 1`).Scan(&rel)
	if rel != "unknown" {
		t.Errorf("default relationship = %q, want unknown", rel)
	}
	var ext int
	db.QueryRow(`SELECT external_alliance_id FROM season_alliances WHERE season_id = 1`).Scan(&ext)

	// The same alliance on another season is a second row; editing one leaves the other.
	putSeasonAlliance(t, `{"season_id":2,"external_alliance_id":`+strconv.Itoa(ext)+`,"relationship":"enemy"}`, nil)
	putSeasonAlliance(t, `{"season_id":1,"external_alliance_id":`+strconv.Itoa(ext)+`,"relationship":"allied","faction_id":`+strconv.Itoa(kage)+`,"terms":"no hits"}`, nil)
	var r1, r2, terms string
	db.QueryRow(`SELECT relationship, terms FROM season_alliances WHERE season_id = 1`).Scan(&r1, &terms)
	db.QueryRow(`SELECT relationship FROM season_alliances WHERE season_id = 2`).Scan(&r2)
	if r1 != "allied" || r2 != "enemy" || terms != "no hits" {
		t.Errorf("rows = %s/%s (%q), want allied in S4 and enemy in S5", r1, r2, terms)
	}
	// A partial write keeps what it leaves out.
	putSeasonAlliance(t, `{"season_id":1,"external_alliance_id":`+strconv.Itoa(ext)+`,"notes":"met in W2"}`, nil)
	db.QueryRow(`SELECT terms FROM season_alliances WHERE season_id = 1`).Scan(&terms)
	if terms != "no hits" {
		t.Errorf("a notes-only write cleared the terms: %q", terms)
	}

	// A faction from another season, a bare tag, and a faction in use are refused.
	if rr := putSeasonAlliance(t, `{"season_id":2,"external_alliance_id":`+strconv.Itoa(ext)+`,"faction_id":`+strconv.Itoa(kage)+`}`, nil); rr.Code != http.StatusBadRequest {
		t.Errorf("another season's faction: %d", rr.Code)
	}
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"WAR"}`, nil); rr.Code != http.StatusBadRequest {
		t.Errorf("bare tag: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	handleSeasonFactionDelete(rr, shReq(http.MethodDelete, "", map[string]string{"id": strconv.Itoa(kage)}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("deleting a faction in use: %d", rr.Code)
	}

	// The registry row cannot be deleted while a season names it.
	rr = httptest.NewRecorder()
	deleteExternalAlliance(rr, shReq(http.MethodDelete, "", map[string]string{"id": strconv.Itoa(ext)}, nil))
	if rr.Code != http.StatusConflict {
		t.Errorf("registry delete with season rows: %d %s", rr.Code, rr.Body.String())
	}

	// The matchup fields are the War tab's: an Allies-only officer may not set them.
	alliesOnly := &AuthUser{ID: 3, Username: "allies", Rank: "R3"}
	mustExecT(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_allies', json('true'), '$.manage_season_war', json('false')) WHERE rank = 'R3'`)
	m := seedMember(t, "Diplomat", "R3")
	alliesOnly.MemberID = &m
	if rr := putSeasonAlliance(t, `{"season_id":1,"external_alliance_id":`+strconv.Itoa(ext)+`,"group_rank":3}`, alliesOnly); rr.Code != http.StatusForbidden {
		t.Errorf("group_rank without manage_season_war: %d", rr.Code)
	}
	if rr := putSeasonAlliance(t, `{"season_id":1,"external_alliance_id":`+strconv.Itoa(ext)+`,"terms":"updated"}`, alliesOnly); rr.Code != http.StatusOK {
		t.Errorf("terms with manage_allies: %d %s", rr.Code, rr.Body.String())
	}
}

func TestOurOwnAllianceHasNoSeasonRow(t *testing.T) {
	setupSettingsTestDB(t)
	seedSeasonRow(t, 1, 4, "2030-11-02", true, false)
	mustExecT(t, `UPDATE settings SET alliance_tag = 'OURS' WHERE id = 1`)
	mustExecT(t, `INSERT INTO allies (id, server, tag, name) VALUES (1, '1712', 'OURS', 'Us')`)

	rr := httptest.NewRecorder()
	req := shReq(http.MethodGet, "", nil, nil)
	req.URL.RawQuery = "ally_id=1"
	handleAllySeasons(rr, req)
	if !strings.Contains(rr.Body.String(), `"own":true`) {
		t.Errorf("own ally's seasons = %s, want own:true and no section", rr.Body.String())
	}
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"ours","server":1712}`, nil); rr.Code != http.StatusBadRequest {
		t.Errorf("a row for our own tag: %d", rr.Code)
	}
	if n := countT(t, `SELECT COUNT(*) FROM external_alliances`); n != 0 {
		t.Errorf("our own alliance was written to the registry (%d rows)", n)
	}
}

// The War tab's LastRank pick links the registry row (never retargeting one linked to another
// LastRank alliance), and LastRank's figures come in as an observation at LastRank's own instant
// (private-docs 277, staging report 2026-10-28).
func TestSeasonAllianceLastRankLink(t *testing.T) {
	setupSettingsTestDB(t)
	mustExecT(t, `INSERT INTO users (id, username, password, is_admin, is_active) VALUES (1, 'admin', 'x', 1, 1)`)
	seedSeasonRow(t, 1, 4, "2030-11-02", true, false)
	t.Setenv("DEMO_MODE", "true") // links without the LastRank request
	const lrA, lrB = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"

	rr := putSeasonAlliance(t, `{"season_id":1,"tag":"WAR","server":1712,"name":"War Machine","lastrank_id":"https://lastrank.fun/a/`+lrA+`"}`, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("put with a LastRank link: %d %s", rr.Code, rr.Body.String())
	}
	var stored string
	db.QueryRow(`SELECT COALESCE(lastrank_id, '') FROM external_alliances WHERE tag = 'WAR'`).Scan(&stored)
	if stored != lrA {
		t.Errorf("registry lastrank_id = %q, want %q", stored, lrA)
	}
	// The same LastRank alliance under a new tag is the same registry row and season row.
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"WAR2","server":1712,"lastrank_id":"`+lrA+`"}`, nil); rr.Code != http.StatusOK {
		t.Fatalf("put by a known id: %d %s", rr.Code, rr.Body.String())
	}
	if n := countT(t, `SELECT COUNT(*) FROM external_alliances`) + countT(t, `SELECT COUNT(*) FROM season_alliances`); n != 2 {
		t.Errorf("%d registry + season rows, want one of each", n)
	}
	// A row linked to another LastRank alliance is not retargeted; junk and Allies-only are refused.
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"WAR","server":1712,"lastrank_id":"`+lrB+`"}`, nil); rr.Code != http.StatusConflict {
		t.Errorf("a different LastRank id on a linked row: %d", rr.Code)
	}
	mustExecT(t, `INSERT INTO external_alliances (id, tag, server) VALUES (50, 'OTH', 1712)`)
	if rr := putSeasonAlliance(t, `{"season_id":1,"external_alliance_id":50,"lastrank_id":"`+lrA+`"}`, nil); rr.Code != http.StatusConflict {
		t.Errorf("another registry row's LastRank id: %d", rr.Code)
	}
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"NEW","server":1712,"lastrank_id":"https://example.com/a/`+lrB+`"}`, nil); rr.Code != http.StatusBadRequest {
		t.Errorf("a link from another site: %d", rr.Code)
	}
	alliesOnly := &AuthUser{ID: 3, Username: "allies", Rank: "R3"}
	mustExecT(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.manage_allies', json('true'), '$.manage_season_war', json('false')) WHERE rank = 'R3'`)
	m := seedMember(t, "Diplomat", "R3")
	alliesOnly.MemberID = &m
	if rr := putSeasonAlliance(t, `{"season_id":1,"tag":"NEW","server":1712,"lastrank_id":"`+lrB+`"}`, alliesOnly); rr.Code != http.StatusForbidden {
		t.Errorf("a LastRank link without manage_season_war: %d", rr.Code)
	}
	db.QueryRow(`SELECT lastrank_id FROM external_alliances WHERE tag = 'WAR2'`).Scan(&stored)
	if stored != lrA {
		t.Errorf("registry lastrank_id after the refusals = %q, want %q", stored, lrA)
	}

	// The import: power and member count at LastRank's instant, once; no instant, nothing.
	var sa int64
	db.QueryRow(`SELECT id FROM season_alliances WHERE season_id = 1`).Scan(&sa)
	a := &lastrank.Alliance{AllianceID: lrA, Abbr: "WAR2", Fightpower: 1234000000, CurMember: 97, LastSeenAt: "2030-11-03T04:05:06Z"}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	first, err1 := importLastRankObservationTx(tx, 1, sa, a, 1)
	again, err2 := importLastRankObservationTx(tx, 1, sa, a, 1)
	undated, err3 := importLastRankObservationTx(tx, 1, sa, &lastrank.Alliance{Fightpower: 5}, 1)
	if err := tx.Commit(); err != nil || err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("import: %v %v %v %v", err, err1, err2, err3)
	}
	if !first || again || undated {
		t.Errorf("written = %v, %v, %v; want true, then unchanged, then nothing", first, again, undated)
	}
	var at, src string
	var pw, mc int64
	db.QueryRow(`SELECT observed_at, alliance_power, member_count, source FROM season_opponent_observations`).Scan(&at, &pw, &mc, &src)
	if at != "2030-11-03 04:05:06" || pw != 1234000000 || mc != 97 || src != "lastrank" {
		t.Errorf("observation = %s %d %d %s, want LastRank's instant, power and member count", at, pw, mc, src)
	}
}
