// reports.go — the reporting layer's core: the report registry and date ranges.
//
// The layer is every reports_*.go file. Its rule (CLAUDE.md → Reporting layer) is that
// a report is a plain function of a query interface, plain params and a clock:
//
//	func run<Name>Report(q rowQueryer, p <Name>Params, now time.Time) (*<Name>Report, error)
//
// No http.Request, no ResponseWriter, no session, no global db and no time.Now(): the
// web handler passes a read transaction, a test passes a temp DB, and the MCP adapter
// (a later project) passes its own transaction — all three get the same rows.

package app

import (
	"database/sql"
	"errors"
	"time"

	"lastwar-alliance/internal/gametime"
)

// reportDef is one report on the Reports page. Permission is the key a caller must
// hold, checked with userHasPermission by the web handlers and any later adapter alike.
type reportDef struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Permission string `json:"permission"`
}

// reportDefs is the registry, in page order.
var reportDefs = []reportDef{
	{Key: "weekly", Title: "Weekly summary", Permission: "view_report_weekly"},
	{Key: "vs-misses", Title: "VS misses", Permission: "view_report_vs_misses"},
	{Key: "participation", Title: "Participation grid", Permission: "view_report_participation"},
}

// reportDefFor returns the registry entry for key.
func reportDefFor(key string) (reportDef, bool) {
	for _, d := range reportDefs {
		if d.Key == key {
			return d, true
		}
	}
	return reportDef{}, false
}

// visibleReports returns the reports u may run, in registry order.
func visibleReports(u *AuthUser) []reportDef {
	var out []reportDef
	for _, d := range reportDefs {
		if userHasPermission(u, d.Permission) {
			out = append(out, d)
		}
	}
	return out
}

// reportParamError is a caller mistake in a report's parameters (a 400 on the web).
type reportParamError struct{ msg string }

func (e reportParamError) Error() string { return e.msg }

func badReportParam(msg string) error { return reportParamError{msg} }

// isReportParamError reports whether err is a parameter mistake rather than a failure.
func isReportParamError(err error) bool {
	var pe reportParamError
	return errors.As(err, &pe)
}

// reportRangeParams asks for a date range in one of three ways, in this precedence:
// Weeks > 0 (the N most recent complete VS weeks), All (from the earliest VS week on
// the install), or explicit From/To game dates. Current adds the current, partial week
// to the first two.
type reportRangeParams struct {
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Weeks   int    `json:"weeks,omitempty"`
	All     bool   `json:"all,omitempty"`
	Current bool   `json:"current,omitempty"`
}

// reportRange is a resolved range of game dates, both inclusive.
type reportRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// lastCompleteSaturday is the most recent Saturday strictly before today's game date:
// the end of the last VS week whose six days are all over (yesterday on a Sunday, the
// day before on a Monday, a week back on a Saturday).
func lastCompleteSaturday(now time.Time) string {
	today, _ := parseDate(gametime.DateOf(now))
	dow := (int(today.Weekday()) + 6) % 7 // Mon=0 … Sun=6
	back := (dow - 5 + 7) % 7
	if back == 0 {
		back = 7
	}
	return formatDateString(today.AddDate(0, 0, -back))
}

// resolveReportRange turns params into explicit game dates, so the page, the exports
// and an adapter resolve "last 4 weeks" identically. The end is never after today's
// game date; nothing reads the wall clock. There is no span cap: report queries are
// bounded by stored rows, not by the span asked for.
func resolveReportRange(q rowQuerier, p reportRangeParams, now time.Time) (reportRange, error) {
	today := gametime.DateOf(now)
	lastSat := lastCompleteSaturday(now)
	end := lastSat
	if p.Current {
		end = today
	}

	switch {
	case p.Weeks < 0:
		return reportRange{}, badReportParam("weeks must be positive")
	case p.Weeks > 0:
		sat, _ := parseDate(lastSat)
		from := formatDateString(sat.AddDate(0, 0, -5-7*(p.Weeks-1)))
		return reportRange{From: from, To: end}, nil
	case p.All:
		var earliest sql.NullString
		if err := q.QueryRow(`SELECT MIN(week_date) FROM vs_points`).Scan(&earliest); err != nil {
			return reportRange{}, err
		}
		if !earliest.Valid || earliest.String == "" {
			return reportRange{}, badReportParam("no VS points are recorded yet")
		}
		return reportRange{From: earliest.String, To: end}, nil
	}

	if p.From == "" || p.To == "" {
		return reportRange{}, badReportParam("give weeks, all, or both from and to")
	}
	from, err := parseDate(p.From)
	if err != nil {
		return reportRange{}, badReportParam("from must be YYYY-MM-DD")
	}
	to, err := parseDate(p.To)
	if err != nil {
		return reportRange{}, badReportParam("to must be YYYY-MM-DD")
	}
	if from.After(to) {
		return reportRange{}, badReportParam("from is after to")
	}
	r := reportRange{From: formatDateString(from), To: formatDateString(to)}
	if r.To > today {
		r.To = today
	}
	if r.From > r.To {
		return reportRange{}, badReportParam("the range is in the future")
	}
	return r, nil
}
