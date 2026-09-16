package app

// starred.go — starred missions (the game calls them the Secret Mobile Squad).
//
// The game rotates starred missions across a block of servers — the sector — in
// three groups, on a three-day cycle. Which group a server is in follows from the
// date it opened; which group is starred on a given day follows from the date.
//
// Two things are deliberately NOT stored:
//
//   - The A/B/C letters. The game's monthly image labels the groups, and the
//     letters rotate month to month. Storing them would bake in a mapping that
//     expires. Groups here are 1/2/3 by residue and are stable forever; the
//     Settings help text says so, because an officer holding this month's image
//     will otherwise expect the letters to line up.
//   - The sector width. See Settings.SectorStart.
//
// The residue arithmetic lives in Go ONLY. The API hands the client an explicit
// day → group map for the range it asked about, so there is one implementation of
// the rule rather than a second one in JavaScript drifting away from it.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"strconv"
	"time"

	"github.com/gorilla/mux"
)

// maxSectorWidth bounds the configured sector. Operational rather than a game
// rule — see the validation in updateSettings.
const maxSectorWidth = 128

// starredEpoch anchors the three-day cycle. Any fixed date works: the rule is a
// residue, so the epoch only decides which of the three labels a given day gets,
// and the labels are ours rather than the game's.
var starredEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// gameDateOf converts an RFC3339 timestamp with an offset into a game date
// (UTC−2).
//
// This is the load-bearing conversion in the whole feature, not a formatting
// nicety: a server that opened in the small hours UTC belongs to the previous
// game day, and getting it wrong moves that server into the wrong group. Two of
// the eighteen servers checked against the published grid move a day.
func gameDateOf(ts string) (string, error) {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "", err
	}
	return t.In(gameLoc).Format("2006-01-02"), nil
}

// daysSinceStarredEpoch counts whole days from the epoch to date. Negative for a
// date before it, which starredGroup handles.
func daysSinceStarredEpoch(date string) (int, error) {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, err
	}
	return int(d.Sub(starredEpoch).Hours() / 24), nil
}

// starredGroup returns 1, 2 or 3 for a date: the group starred that day, or —
// applied to a server's opening date — the group that server belongs to.
//
// Written with a Euclidean modulo. Go's % takes the sign of the dividend, so a
// date before the epoch would otherwise produce 0 or a negative group.
func starredGroup(date string) (int, error) {
	days, err := daysSinceStarredEpoch(date)
	if err != nil {
		return 0, err
	}
	return ((days%3)+3)%3 + 1, nil
}

// loadSector reads the configured sector. ok is false when it is unset, and every
// caller then reports "not configured" rather than guessing a range.
func loadSector() (start, end int, ok bool) {
	db.QueryRow(`SELECT COALESCE(sector_start, 0), COALESCE(sector_end, 0) FROM settings WHERE id = 1`).
		Scan(&start, &end)
	return start, end, start > 0 && end >= start
}

// serverOpenDate is one row of server_open_dates, decorated with its group.
type serverOpenDate struct {
	ServerID  int    `json:"server_id"`
	OpenDate  string `json:"open_date"`
	Group     int    `json:"group"`
	Source    string `json:"source"`
	FetchedAt string `json:"fetched_at"`
}

// loadSectorOpenDates reads every stored open date INSIDE the sector.
//
// The sector bound is applied in SQL rather than trusted from the table. A server
// that has since fallen outside the configured sector must never appear in a group
// list whatever server_open_dates still holds: this fails closed, so a sector
// correction takes effect immediately instead of waiting for a cleanup nobody runs.
func loadSectorOpenDates(start, end int) ([]serverOpenDate, error) {
	rows, err := db.Query(`
		SELECT server_id, open_date, source, COALESCE(fetched_at, '')
		FROM server_open_dates
		WHERE server_id BETWEEN ? AND ?
		ORDER BY server_id`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []serverOpenDate
	for rows.Next() {
		var r serverOpenDate
		if err := rows.Scan(&r.ServerID, &r.OpenDate, &r.Source, &r.FetchedAt); err != nil {
			return nil, err
		}
		if g, err := starredGroup(r.OpenDate); err == nil {
			r.Group = g
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GET /api/schedule/starred?from=&to=
//
// Returns the group membership lists, plus an explicit day → group map for the
// requested range. The map is the point: the calendar never computes the residue,
// so there is exactly one implementation of it.
func getStarredSchedule(w http.ResponseWriter, r *http.Request) {
	start, end, ok := loadSector()
	resp := map[string]any{
		"configured": ok,
		"sector":     map[string]int{"start": start, "end": end},
	}
	if !ok {
		writeJSON(w, resp)
		return
	}

	known, err := loadSectorOpenDates(start, end)
	if err != nil {
		slog.Error("getStarredSchedule open dates", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	groups := map[string][]int{"1": {}, "2": {}, "3": {}}
	seen := map[int]bool{}
	manual := []int{}
	for _, row := range known {
		seen[row.ServerID] = true
		if row.Group >= 1 && row.Group <= 3 {
			key := strconv.Itoa(row.Group)
			groups[key] = append(groups[key], row.ServerID)
		}
		if row.Source == "manual" {
			manual = append(manual, row.ServerID)
		}
	}

	// Servers in the sector with no stored open date. Named rather than omitted:
	// a group list that is quietly short is worse than one that says what it is
	// missing, because the officer cannot tell the difference from the outside.
	unknown := []int{}
	for id := start; id <= end; id++ {
		if !seen[id] {
			unknown = append(unknown, id)
		}
	}

	days := map[string]int{}
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from != "" && to != "" {
		fromT, errF := time.Parse("2006-01-02", from)
		toT, errT := time.Parse("2006-01-02", to)
		if errF == nil && errT == nil && !toT.Before(fromT) {
			// Bounded so a hand-built query cannot ask for a decade of days.
			for d := fromT; !d.After(toT) && len(days) < 400; d = d.AddDate(0, 0, 1) {
				ds := d.Format("2006-01-02")
				if g, err := starredGroup(ds); err == nil {
					days[ds] = g
				}
			}
		}
	}

	resp["groups"] = groups
	resp["days"] = days
	resp["unknown"] = unknown
	resp["manual"] = manual
	writeJSON(w, resp)
}

// GET /api/starred/servers — every server in the sector, known open date or not.
func getStarredServers(w http.ResponseWriter, r *http.Request) {
	start, end, ok := loadSector()
	if !ok {
		writeJSON(w, map[string]any{"configured": false, "servers": []serverOpenDate{}})
		return
	}
	known, err := loadSectorOpenDates(start, end)
	if err != nil {
		slog.Error("getStarredServers", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	byID := map[int]serverOpenDate{}
	for _, row := range known {
		byID[row.ServerID] = row
	}

	servers := make([]serverOpenDate, 0, end-start+1)
	for id := start; id <= end; id++ {
		if row, found := byID[id]; found {
			servers = append(servers, row)
			continue
		}
		servers = append(servers, serverOpenDate{ServerID: id, Source: "unknown"})
	}
	writeJSON(w, map[string]any{
		"configured": true,
		"sector":     map[string]int{"start": start, "end": end},
		"servers":    servers,
	})
}

// PUT /api/starred/servers/{id} — an officer's correction.
//
// A manual row always wins: the sweep only ever plans servers with NO row, so
// nothing overwrites this later. That is the whole reason source is stored.
func updateStarredServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}
	var req struct {
		OpenDate string `json:"open_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("2006-01-02", req.OpenDate); err != nil {
		http.Error(w, "open_date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	start, end, ok := loadSector()
	if !ok {
		http.Error(w, "Set the starred-mission sector first", http.StatusBadRequest)
		return
	}
	if id < start || id > end {
		http.Error(w, fmt.Sprintf("Server %d is outside the sector (%d–%d)", id, start, end), http.StatusBadRequest)
		return
	}

	if _, err := db.Exec(`
		INSERT INTO server_open_dates (server_id, open_date, source, fetched_at)
		VALUES (?, ?, 'manual', datetime('now'))
		ON CONFLICT(server_id) DO UPDATE SET
			open_date = excluded.open_date, source = 'manual', fetched_at = excluded.fetched_at`,
		id, req.OpenDate); err != nil {
		slog.Error("updateStarredServer", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	group, _ := starredGroup(req.OpenDate)
	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "updated", "starred_server", strconv.Itoa(id), false,
		fmt.Sprintf("open date %s (group %d)", req.OpenDate, group))
	writeJSON(w, map[string]any{"server_id": id, "open_date": req.OpenDate, "group": group, "source": "manual"})
}

// DELETE /api/starred/servers/{id} — drop the manual row so the next sweep
// re-derives it from LastRank.
func deleteStarredServer(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid server id", http.StatusBadRequest)
		return
	}
	if _, err := db.Exec(`DELETE FROM server_open_dates WHERE server_id = ?`, id); err != nil {
		slog.Error("deleteStarredServer", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	user := getAuthUser(r)
	logActivity(user.ID, user.Username, "deleted", "starred_server", strconv.Itoa(id), false, "cleared the stored open date")
	w.WriteHeader(http.StatusNoContent)
}
