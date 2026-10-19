// handlers_donations.go - the donations read endpoint behind the Rankings tab and the
// dashboard card (Project 20). The store, the derivation and the judgement are in
// donations.go; this only assembles one week for every active member.

package app

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
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

// --- entered days: Save on the Donations tab, and the CSV import ---------------------------

type donationEntriesRequest struct {
	Source string              `json:"source"` // manual (the tab's Save) | csv (an import's confirm)
	Cells  []donationEntryCell `json:"cells"`
}

// PUT /api/donations/entries — manage_members. Writes the changed day cells in one
// transaction. A manual save follows the VS page's rule: a cell changed underneath to
// something other than what was typed is a conflict, answered 409 with the list and nothing
// written; the page asks which to keep and resubmits. A CSV confirm replaces.
func putDonationEntries(w http.ResponseWriter, r *http.Request) {
	var req donationEntriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		req.Source = "manual"
	}
	if req.Source != "manual" && req.Source != "csv" {
		http.Error(w, "source must be manual or csv", http.StatusBadRequest)
		return
	}
	user := getAuthUser(r)
	now := time.Now()
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "putDonationEntries: begin", err)
		return
	}
	defer tx.Rollback()
	res, err := saveDonationEntriesTx(tx, req.Cells, req.Source, req.Source == "manual", user.ID, now)
	if err != nil {
		dbError(w, "putDonationEntries", err)
		return
	}
	if len(res.Conflicts) > 0 {
		// As the VS page: nothing written; the page asks, cell by cell, which to keep.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(res)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "putDonationEntries: commit", err)
		return
	}
	if res.Saved+res.Cleared > 0 {
		var dates []string
		for _, c := range req.Cells {
			dates = append(dates, c.GameDate)
		}
		action, how := "updated", "by hand"
		if req.Source == "csv" {
			action, how = "imported", "from CSV"
		}
		details := fmt.Sprintf("%s entered, %d cleared", plural(res.Saved, "day", "days"), res.Cleared)
		if res.Overridden > 0 {
			details += fmt.Sprintf(", %d conflicts overridden", res.Overridden)
		}
		logActivity(user.ID, user.Username, action, "donations", dateSpan(dates), false, details+" · "+how)
	}
	writeJSON(w, res)
}

// donationCSVRow is one member's cells from an import file, before name resolution.
type donationCSVRow struct {
	Name  string
	Cells []donationEntryCell // MemberID unset
}

var donationCSVWeekdays = map[string]int{
	"mon": 0, "monday": 0, "tue": 1, "tues": 1, "tuesday": 1, "wed": 2, "wednesday": 2,
	"thu": 3, "thur": 3, "thurs": 3, "thursday": 3, "fri": 4, "friday": 4,
	"sat": 5, "saturday": 5, "sun": 6, "sunday": 6,
}

// parseDonationAmount reads "1,250", " 900 " or "" (blank: no value). Anything else is an error.
func parseDonationAmount(s string) (*int64, error) {
	s = strings.NewReplacer(",", "", " ", "", " ", "").Replace(strings.TrimSpace(s))
	if s == "" || s == "—" || s == "-" {
		return nil, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a whole number", s)
	}
	return &n, nil
}

// parseDonationCSV reads an import file in one of two shapes: long, with a member, a date and
// an amount column (any dates); or wide, a member column and Mon…Sun columns for the week of
// `week` (the Donations tab's own export). Blank cells are skipped, never cleared. Columns
// are found by header, before any row is read.
func parseDonationCSV(records [][]string, week string) ([]donationCSVRow, []string, error) {
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("the file has no rows under its header")
	}
	nameCol, dateCol, amountCol := -1, -1, -1
	dayCols := map[int]int{} // column → weekday
	for i, h := range records[0] {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))
		switch {
		case h == "member" || h == "name" || h == "commander" || h == "player":
			nameCol = i
		case h == "date" || h == "day" || h == "game_date" || h == "game date":
			dateCol = i
		case h == "amount" || h == "donation" || h == "donations" || h == "points":
			amountCol = i
		default:
			if d, ok := donationCSVWeekdays[h]; ok {
				dayCols[i] = d
			}
		}
	}
	if nameCol < 0 {
		return nil, nil, fmt.Errorf("no member column (Member, Name or Commander)")
	}
	long := dateCol >= 0 && amountCol >= 0
	if !long && len(dayCols) == 0 {
		return nil, nil, fmt.Errorf("give Date and Amount columns, or Mon…Sun columns with a week")
	}
	if !long {
		if _, err := time.Parse(gametime.DateLayout, week); err != nil {
			return nil, nil, fmt.Errorf("choose the week the Mon…Sun columns are for")
		}
		week = donationWeekMonday(week)
	}
	byName := map[string]*donationCSVRow{}
	var order []string
	var errs []string
	add := func(name, date string, amount *int64) {
		if byName[name] == nil {
			byName[name] = &donationCSVRow{Name: name}
			order = append(order, name)
		}
		byName[name].Cells = append(byName[name].Cells, donationEntryCell{GameDate: date, Amount: amount})
	}
	for n, rec := range records[1:] {
		line := n + 2
		if nameCol >= len(rec) || strings.TrimSpace(rec[nameCol]) == "" {
			continue
		}
		name := strings.TrimSpace(rec[nameCol])
		if long {
			if dateCol >= len(rec) || amountCol >= len(rec) {
				errs = append(errs, fmt.Sprintf("line %d (%s): missing date or amount", line, name))
				continue
			}
			date := strings.TrimSpace(rec[dateCol])
			if _, err := time.Parse(gametime.DateLayout, date); err != nil {
				errs = append(errs, fmt.Sprintf("line %d (%s): %q is not a YYYY-MM-DD date", line, name, date))
				continue
			}
			amount, err := parseDonationAmount(rec[amountCol])
			if err != nil {
				errs = append(errs, fmt.Sprintf("line %d (%s): %v", line, name, err))
				continue
			}
			if amount != nil {
				add(name, date, amount)
			}
			continue
		}
		for col, d := range dayCols {
			if col >= len(rec) {
				continue
			}
			amount, err := parseDonationAmount(rec[col])
			if err != nil {
				errs = append(errs, fmt.Sprintf("line %d (%s, %s): %v", line, name, records[0][col], err))
				continue
			}
			if amount != nil {
				add(name, addDays(week, d), amount)
			}
		}
	}
	out := make([]donationCSVRow, 0, len(order))
	for _, n := range order {
		row := byName[n]
		sort.Slice(row.Cells, func(i, j int) bool { return row.Cells[i].GameDate < row.Cells[j].GameDate })
		out = append(out, *row)
	}
	return out, errs, nil
}

type donationImportPreviewRow struct {
	OriginalName  string              `json:"original_name"`
	MatchedMember *Member             `json:"matched_member"`
	MatchType     string              `json:"match_type"`
	Cells         []donationEntryCell `json:"cells"`
}

// POST /api/donations/import/preview — manage_members. Reads a CSV (csv_file, and week for
// the Mon…Sun shape), resolves each name as the VS import does, and saves nothing: the page
// shows the rows, the officer maps any unresolved name, and the confirm goes through
// PUT /api/donations/entries with source csv.
func previewDonationImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxCSVUploadSize)
	if err := r.ParseMultipartForm(MaxCSVUploadSize); err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("csv_file")
	if err != nil {
		http.Error(w, "Missing csv_file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		http.Error(w, "Invalid CSV format", http.StatusBadRequest)
		return
	}
	rows, errs, err := parseDonationCSV(records, r.FormValue("week"))
	if err != nil {
		http.Error(w, "Cannot import: "+err.Error(), http.StatusBadRequest)
		return
	}
	userID := getAuthUser(r).ID
	tx, err := db.Begin()
	if err != nil {
		dbError(w, "previewDonationImport: begin", err)
		return
	}
	defer tx.Rollback()
	idx, err := buildFoldedNameIndex(tx, userID)
	if err != nil {
		idx = nil
	}
	out := []donationImportPreviewRow{}
	for _, row := range rows {
		p := donationImportPreviewRow{OriginalName: row.Name, Cells: row.Cells, MatchType: "unresolved"}
		if m, how, err := resolveMemberAliasWithIndex(tx, row.Name, userID, idx); err == nil {
			p.MatchedMember, p.MatchType = m, how
			for i := range p.Cells {
				p.Cells[i].MemberID = m.ID
			}
		}
		out = append(out, p)
	}
	if errs == nil {
		errs = []string{}
	}
	writeJSON(w, map[string]any{"rows": out, "errors": errs})
}

// plural is "1 day" / "3 days".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
