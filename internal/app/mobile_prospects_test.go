package app

import (
	"net/http"
	"strconv"
	"testing"
)

// The mobile prospects store (Project 15, C8): game-read fields only.
func TestMobileProspects(t *testing.T) {
	setupMobileTestDB(t)
	setRankPerm(t, "R4", "view_recruiting", true)
	setRankPerm(t, "R4", "manage_recruiting", true)
	tok := mobileToken(t, 2)

	w := serveMobile(t, "POST", "/api/mobile/prospects", `{"name":"Scout","server":"1701","source_alliance":"ABC","power":5000,"rank_in_alliance":"R3"}`, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	out := decodeObject(t, w)
	assertKeys(t, "prospect", out, "id", "name", "server", "source_alliance", "power", "hero_power", "rank_in_alliance", "created")
	if out["created"] != true {
		t.Errorf("create = %v", out)
	}
	id := int(out["id"].(float64))
	var status, ptype string
	db.QueryRow(`SELECT status, prospect_type FROM prospects WHERE id = ?`, id).Scan(&status, &ptype)
	if status != "interested" || ptype != "transfer" {
		t.Errorf("defaults = %s %s", status, ptype)
	}

	// A duplicate (name ignoring case, same server) is refused with its id.
	w = serveMobile(t, "POST", "/api/mobile/prospects", `{"name":"scout","server":"1701"}`, tok)
	if w.Code != http.StatusConflict || decodeObject(t, w)["prospect_id"] != float64(id) {
		t.Errorf("duplicate: %d %s", w.Code, w.Body.String())
	}
	// Another server is another prospect.
	if w := serveMobile(t, "POST", "/api/mobile/prospects", `{"name":"Scout","server":"1702"}`, tok); w.Code != http.StatusOK {
		t.Errorf("other server: %d", w.Code)
	}

	// An update by id changes only the fields sent; officer fields survive.
	db.Exec(`UPDATE prospects SET status = 'contacted', notes = 'keep me', recruiter_id = 1 WHERE id = ?`, id)
	w = serveMobile(t, "POST", "/api/mobile/prospects", `{"prospect_id":`+strconv.Itoa(id)+`,"power":6000,"name":"Scout Renamed"}`, tok)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var name, notes, server string
	var power int64
	var recruiter int
	db.QueryRow(`SELECT name, server, power, status, notes, recruiter_id FROM prospects WHERE id = ?`, id).Scan(&name, &server, &power, &status, &notes, &recruiter)
	if name != "Scout Renamed" || server != "1701" || power != 6000 || status != "contacted" || notes != "keep me" || recruiter != 1 {
		t.Errorf("after update = %s %s %d %s %q %d", name, server, power, status, notes, recruiter)
	}
	var det string
	db.QueryRow(`SELECT details FROM activity_log WHERE entity_type = 'prospect' AND action = 'updated'`).Scan(&det)
	if det != "name: Scout → Scout Renamed; power: 5000 → 6000 · via mobile" {
		t.Errorf("details = %q", det)
	}

	// Validation.
	for _, body := range []string{`{"name":""}`, `{"name":"X","server":"abc"}`, `{"name":"X","rank_in_alliance":"R7"}`, `{"name":"X","power":0}`, `{"server":"1"}`} {
		if w := serveMobile(t, "POST", "/api/mobile/prospects", body, tok); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, w.Code)
		}
	}

	// The read lists them; R3 without view_recruiting is refused.
	w = serveMobile(t, "GET", "/api/mobile/prospects", "", tok)
	if w.Code != http.StatusOK {
		t.Errorf("list: %d", w.Code)
	}
	setRankPerm(t, "R3", "view_recruiting", false)
	if w := serveMobile(t, "GET", "/api/mobile/prospects", "", mobileToken(t, 3)); w.Code != http.StatusForbidden {
		t.Errorf("R3 list: %d", w.Code)
	}
}
