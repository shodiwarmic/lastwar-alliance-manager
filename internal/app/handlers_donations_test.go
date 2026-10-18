package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The Rankings tab's endpoint: every active member, blanks last, sorted by total; the
// week resolved server-side ("previous" included); an empty week is 200 with every member
// blank; the route needs view_rankings.
func TestDonationsWeek(t *testing.T) {
	setupPreviewTestDB(t) // Leader (R5), Officer (R3)
	a := seedMember(t, "Alder", "R3")
	b := seedMember(t, "Birch", "R2")
	seedMember(t, "Former", "EX")
	mustExec(t, `INSERT INTO donation_week_minimums (week_date, weekly_minimum, daily_minimum) VALUES ('2026-10-05', 5000, 0)`)
	donationDayAt(t, a, "2026-10-11", 500, 4000)
	donationDayAt(t, b, "2026-10-10", 900, 9000)
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	resp, err := loadDonationsWeek(db, "2026-10-05", now)
	if err != nil {
		t.Fatal(err)
	}
	if resp.WeekDate != "2026-10-05" || !resp.WeekOver || resp.Days[6] != "2026-10-11" || resp.Minimums.Weekly != 5000 {
		t.Errorf("week %+v", resp)
	}
	if len(resp.Weeks) != 2 || resp.Weeks[0] != "2026-10-12" || resp.Weeks[1] != "2026-10-05" {
		t.Errorf("weeks %v", resp.Weeks)
	}
	if len(resp.Members) != 4 {
		t.Fatalf("%d members, want the 4 active", len(resp.Members))
	}
	order := []string{resp.Members[0].Name, resp.Members[1].Name}
	if order[0] != "Birch" || order[1] != "Alder" || resp.Members[2].Total.Status != donationBlank {
		t.Errorf("order %v, then %+v", order, resp.Members[2])
	}
	// Alder: Sunday read, final 4,000 < 5,000. Birch: no Sunday reading, not read to the end.
	if m := resp.Members[1]; m.Total != (donationWeekTotal{4000, donationFinal}) || !m.BelowWeekly {
		t.Errorf("Alder %+v", m)
	}
	if m := resp.Members[0]; !m.NotReadToEnd || m.BelowWeekly || m.Days[5].Status != donationSoFar {
		t.Errorf("Birch %+v", m)
	}

	// Week resolution.
	for raw, want := range map[string]string{"": "2026-10-12", "previous": "2026-10-05", "2026-10-08": "2026-10-05"} {
		if got, err := resolveDonationWeek(raw, now); err != nil || got != want {
			t.Errorf("week %q = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, bad := range []string{"2026-10-19", "13/10/2026"} {
		if _, err := resolveDonationWeek(bad, now); err == nil {
			t.Errorf("week %q accepted", bad)
		}
	}

	// A week with no readings: 200, every member blank.
	resp, err = loadDonationsWeek(db, "2026-09-28", now)
	if err != nil || len(resp.Members) != 4 {
		t.Fatalf("empty week: %v %v", resp, err)
	}
	for _, m := range resp.Members {
		if m.Total.Status != donationBlank {
			t.Errorf("empty week: %+v", m)
		}
	}

	// The route: view_rankings.
	get := func(user int) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/donations?week=previous", nil)
		r.AddCookie(sessionCookie(t, user, ""))
		w := httptest.NewRecorder()
		buildRouter().ServeHTTP(w, r)
		return w
	}
	if w := get(1); w.Code != http.StatusOK {
		t.Errorf("admin: %d %s", w.Code, w.Body.String())
	} else {
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		if _, ok := body["members"]; !ok {
			t.Errorf("body keys %v", keysOf(body))
		}
	}
	mustExec(t, `UPDATE rank_permissions SET permissions = json_set(permissions, '$.view_rankings', json('false')) WHERE rank = 'R3'`)
	if w := get(3); w.Code != http.StatusForbidden {
		t.Errorf("R3 without view_rankings: %d", w.Code)
	}
}
