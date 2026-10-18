// handlers_donations.go - the donations read endpoint behind the Rankings tab and the
// dashboard card (Project 20). The store, the derivation and the judgement are in
// donations.go; this only assembles one week for every active member.

package app

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"lastwar-alliance/internal/gametime"
)

type donationMemberWeek struct {
	ID    int               `json:"id"`
	Name  string            `json:"name"`
	Rank  string            `json:"rank"`
	Days  [7]donationDay    `json:"days"`
	Total donationWeekTotal `json:"total"`
	donationJudgement
}

type donationsWeekResponse struct {
	WeekDate string               `json:"week_date"`
	WeekOver bool                 `json:"week_over"`
	Days     [7]string            `json:"days"`
	Weeks    []string             `json:"weeks"`
	Minimums donationMinimums     `json:"minimums"`
	Members  []donationMemberWeek `json:"members"`
}

// resolveDonationWeek turns the week parameter into a Monday: absent is the current
// Monday–Sunday game week, "previous" the one before (resolved here, so no browser
// computes a Monday), a date the week holding it. A week after the current one is refused.
func resolveDonationWeek(raw string, now time.Time) (string, error) {
	_, current := donationEvalWeeks(now)
	switch raw {
	case "", "current":
		return current, nil
	case "previous":
		return addDays(current, -7), nil
	}
	if _, err := time.Parse(gametime.DateLayout, raw); err != nil {
		return "", fmt.Errorf("week %q is not a YYYY-MM-DD date", raw)
	}
	week := donationWeekMonday(raw)
	if week > current {
		return "", fmt.Errorf("week %s has not started", week)
	}
	return week, nil
}

// loadDonationsWeek is one week for every active member, blanks included (a duty tracker
// has to show who was not read), sorted by total with blanks last, then name. Every read
// is made before the member cursor opens.
func loadDonationsWeek(q rowQueryer, week string, now time.Time) (*donationsWeekResponse, error) {
	weeks, err := loadDonationWeeks(q, week, week, now)
	if err != nil {
		return nil, err
	}
	mins, err := donationMinimumsForWeek(q, week)
	if err != nil {
		return nil, err
	}
	available, err := donationWeeksAvailable(q)
	if err != nil {
		return nil, err
	}
	_, current := donationEvalWeeks(now)
	out := &donationsWeekResponse{WeekDate: week, WeekOver: !now.Before(donationWeekEnd(week)), Minimums: mins,
		Weeks: []string{current}, Members: []donationMemberWeek{}}
	for _, w := range available {
		if w != current {
			out.Weeks = append(out.Weeks, w)
		}
	}
	for d := 0; d < 7; d++ {
		out.Days[d] = addDays(week, d)
	}

	rows, err := q.Query(`SELECT id, name, rank, COALESCE(joined_at, '') FROM members WHERE rank != 'EX'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m donationMemberWeek
		var joined string
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &joined); err != nil {
			rows.Close()
			return nil, err
		}
		w, ok := weeks[m.ID][week]
		if !ok {
			w = deriveDonationWeek(week, [7]donationDayReadings{}, now)
		}
		m.Days, m.Total = w.Days, w.Total
		m.donationJudgement = judgeDonationWeek(w, mins, joined)
		out.Members = append(out.Members, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	sort.SliceStable(out.Members, func(i, j int) bool {
		a, b := out.Members[i], out.Members[j]
		if (a.Total.Status == donationBlank) != (b.Total.Status == donationBlank) {
			return b.Total.Status == donationBlank
		}
		if a.Total.Amount != b.Total.Amount {
			return a.Total.Amount > b.Total.Amount
		}
		return a.Name < b.Name
	})
	return out, nil
}

// GET /api/donations?week=YYYY-MM-DD|previous — view_rankings.
func getDonations(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	week, err := resolveDonationWeek(r.URL.Query().Get("week"), now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// One read transaction, so the week, its minimums and the roster are one snapshot.
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "getDonations: begin", err)
		return
	}
	defer tx.Rollback()
	resp, err := loadDonationsWeek(tx, week, now)
	if err != nil {
		dbError(w, "getDonations", err)
		return
	}
	writeJSON(w, resp)
}
