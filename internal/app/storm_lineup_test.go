package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// The Desert Storm lineup is what the participation role snapshot copies, so a save
// that silently drops a member produces a battle where that member has no role and is
// never suggested. These tests pin the round trip and the failure path.

func setupStormLineupTestDB(t *testing.T) (groupID int) {
	t.Helper()
	setupSettingsTestDB(t)
	if _, err := db.Exec(`INSERT INTO members (id, name, rank) VALUES (1,'Alpha','R4'),(2,'Bravo','R3'),(3,'Charlie','R2'),(4,'Delta','R1')`); err != nil {
		t.Fatalf("members: %v", err)
	}
	res, err := db.Exec(`INSERT INTO storm_groups (task_force, name, instructions, sort_order) VALUES ('A','Group 1','',0)`)
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

func stormLineupRequest(t *testing.T, method, path string, groupID int, body any) *http.Request {
	t.Helper()
	b, _ := json.Marshal(body)
	req := scheduleTestActor(httptest.NewRequest(method, path, strings.NewReader(string(b))))
	return mux.SetURLVars(req, map[string]string{"id": strconv.Itoa(groupID)})
}

func readStormGroup(t *testing.T, groupID int) StormGroup {
	t.Helper()
	req := scheduleTestActor(httptest.NewRequest(http.MethodGet, "/api/storm/groups?task_force=A", nil))
	rr := httptest.NewRecorder()
	getStormGroups(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getStormGroups = %d (%s)", rr.Code, rr.Body.String())
	}
	var groups []StormGroup
	if err := json.Unmarshal(rr.Body.Bytes(), &groups); err != nil {
		t.Fatalf("decode groups: %v", err)
	}
	for _, g := range groups {
		if g.ID == groupID {
			return g
		}
	}
	t.Fatalf("group %d not returned", groupID)
	return StormGroup{}
}

func TestStormLineupRoundTrip(t *testing.T) {
	groupID := setupStormLineupTestDB(t)

	rr := httptest.NewRecorder()
	saveGroupBuildings(rr, stormLineupRequest(t, http.MethodPut, "/api/storm/groups/x/buildings", groupID, []StormGroupBuilding{
		{BuildingID: "oil_refinery_1", SortOrder: 0, Members: []StormGroupMember{
			{MemberID: 1, IsSub: false, Position: 0},
			{MemberID: 2, IsSub: true, Position: 1},
		}},
	}))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("saveGroupBuildings = %d (%s)", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	saveGroupDirectMembers(rr, stormLineupRequest(t, http.MethodPut, "/api/storm/groups/x/members", groupID, []StormGroupMember{
		{MemberID: 3, IsSub: false, Position: 0},
		{MemberID: 4, IsSub: true, Position: 1},
	}))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("saveGroupDirectMembers = %d (%s)", rr.Code, rr.Body.String())
	}

	g := readStormGroup(t, groupID)
	if len(g.Buildings) != 1 || len(g.Buildings[0].Members) != 2 {
		t.Fatalf("buildings = %+v, want one building with two members", g.Buildings)
	}
	if bm := g.Buildings[0].Members; bm[0].MemberID != 1 || bm[0].IsSub || bm[1].MemberID != 2 || !bm[1].IsSub {
		t.Errorf("building members = %+v, want 1 starter then 2 sub", bm)
	}
	if dm := g.DirectMembers; len(dm) != 2 || dm[0].MemberID != 3 || dm[0].IsSub || dm[1].MemberID != 4 || !dm[1].IsSub {
		t.Errorf("direct members = %+v, want 3 starter then 4 sub", dm)
	}
}

// Every building's members must come back, not only the last building's. The reader
// indexes buildings by pointer while it is still appending to the group's slice.
func TestStormLineupReadsEveryBuilding(t *testing.T) {
	groupID := setupStormLineupTestDB(t)
	rr := httptest.NewRecorder()
	saveGroupBuildings(rr, stormLineupRequest(t, http.MethodPut, "/", groupID, []StormGroupBuilding{
		{BuildingID: "b1", SortOrder: 0, Members: []StormGroupMember{{MemberID: 1, Position: 0}}},
		{BuildingID: "b2", SortOrder: 1, Members: []StormGroupMember{{MemberID: 2, Position: 0}}},
		{BuildingID: "b3", SortOrder: 2, Members: []StormGroupMember{{MemberID: 3, Position: 0}}},
	}))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("saveGroupBuildings = %d (%s)", rr.Code, rr.Body.String())
	}
	g := readStormGroup(t, groupID)
	if len(g.Buildings) != 3 {
		t.Fatalf("buildings = %d, want 3", len(g.Buildings))
	}
	for i, b := range g.Buildings {
		if len(b.Members) != 1 || b.Members[0].MemberID != i+1 {
			t.Errorf("building %s members = %+v, want member %d", b.BuildingID, b.Members, i+1)
		}
	}
}

// A per-member INSERT that fails must roll the whole save back and report it. Before
// #144 the error was discarded: the DELETE and the surviving inserts committed and the
// handler answered 204, so the officer saw success with a member missing.
func TestStormLineupSaveFailureRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
		save    func(groupID int) *httptest.ResponseRecorder
		members string // the surviving member ids, which must be the original lineup
		want    string
	}{
		{
			name:    "direct members",
			trigger: `CREATE TRIGGER fail_member BEFORE INSERT ON storm_group_members WHEN NEW.member_id = 4 BEGIN SELECT RAISE(ABORT, 'injected'); END`,
			save: func(groupID int) *httptest.ResponseRecorder {
				rr := httptest.NewRecorder()
				saveGroupDirectMembers(rr, stormLineupRequest(t, http.MethodPut, "/", groupID, []StormGroupMember{
					{MemberID: 3, Position: 0}, {MemberID: 4, Position: 1},
				}))
				return rr
			},
			members: `SELECT group_concat(member_id) FROM storm_group_members`,
			want:    "1",
		},
		{
			name:    "building members",
			trigger: `CREATE TRIGGER fail_member BEFORE INSERT ON storm_group_building_members WHEN NEW.member_id = 4 BEGIN SELECT RAISE(ABORT, 'injected'); END`,
			save: func(groupID int) *httptest.ResponseRecorder {
				rr := httptest.NewRecorder()
				saveGroupBuildings(rr, stormLineupRequest(t, http.MethodPut, "/", groupID, []StormGroupBuilding{
					{BuildingID: "b1", Members: []StormGroupMember{{MemberID: 3, Position: 0}, {MemberID: 4, Position: 1}}},
				}))
				return rr
			},
			members: `SELECT group_concat(member_id) FROM storm_group_building_members`,
			want:    "2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := setupStormLineupTestDB(t)
			// The existing lineup the failed save must leave intact.
			if _, err := db.Exec(`INSERT INTO storm_group_members (group_id, member_id, is_sub, position) VALUES (?, 1, 0, 0)`, groupID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO storm_group_buildings (id, group_id, building_id, sort_order) VALUES (50, ?, 'b0', 0)`, groupID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO storm_group_building_members (group_building_id, member_id, is_sub, position) VALUES (50, 2, 0, 0)`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(tc.trigger); err != nil {
				t.Fatalf("trigger: %v", err)
			}

			rr := tc.save(groupID)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500 (body %q)", rr.Code, rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "injected") {
				t.Errorf("raw error reached the client: %q", rr.Body.String())
			}
			var got string
			if err := db.QueryRow(tc.members).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("lineup after a failed save = %q, want the original %q (rolled back)", got, tc.want)
			}
		})
	}
}

func TestDeleteMemberRegistrationReportsFailure(t *testing.T) {
	setupStormLineupTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_reg BEFORE DELETE ON storm_registrations BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO storm_registrations (member_id, slot_1) VALUES (1, 1)`); err != nil {
		t.Fatal(err)
	}
	req := scheduleTestActor(httptest.NewRequest(http.MethodDelete, "/", nil))
	req = mux.SetURLVars(req, map[string]string{"member_id": "1"})
	rr := httptest.NewRecorder()
	deleteMemberRegistration(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when the DELETE fails", rr.Code)
	}
}
