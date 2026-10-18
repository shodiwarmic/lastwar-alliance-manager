// handlers_reports.go — the Reports page and the web adapter over the reporting layer.
//
// Every report handler has one shape: parse → permission (on the route) → open a read
// transaction → run the report with the transaction as q → writeJSON. Running on one
// transaction gives a report's several queries one snapshot, so a week deleted or
// imported between two of them cannot show up half-applied. Nothing else goes here:
// the computing is the layer's (reports_*.go).

package app

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// handleReportsPage serves /reports to anyone holding at least one report key.
func handleReportsPage(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Reports - Alliance Manager", "reports")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}
	defs := visibleReports(getAuthUser(r))
	if len(defs) == 0 {
		data.Title = "403 Access Denied"
		data.ActivePage = "403"
		renderTemplateStatus(w, r, "403.html", http.StatusForbidden, data)
		return
	}
	data.Reports = defs
	renderTemplate(w, r, "reports.html", data)
}

// handleReportsList is GET /api/reports: the reports the caller may run.
func handleReportsList(w http.ResponseWriter, r *http.Request) {
	defs := visibleReports(getAuthUser(r))
	if defs == nil {
		defs = []reportDef{}
	}
	writeJSON(w, defs)
}

// reportRangeParamsFromQuery reads ?from=&to=&weeks=N|all&current=1.
func reportRangeParamsFromQuery(v url.Values) (reportRangeParams, error) {
	p := reportRangeParams{From: v.Get("from"), To: v.Get("to"), Current: v.Get("current") == "1"}
	switch w := v.Get("weeks"); w {
	case "":
	case "all":
		p.All = true
	default:
		n, err := strconv.Atoi(w)
		if err != nil || n < 1 {
			return p, badReportParam("weeks must be a positive number or all")
		}
		p.Weeks = n
	}
	return p, nil
}

// serveReport runs one report on a read transaction and writes its result: 400 for a
// parameter mistake, 500 (logged, not leaked) for anything else.
func serveReport(w http.ResponseWriter, name string, run func(q rowQueryer) (any, error)) {
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	result, err := run(tx)
	if err != nil {
		if isReportParamError(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Error("report failed", "report", name, "error", err)
		http.Error(w, "Failed to build the report", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// handleWeeklySummaryReport is GET /api/reports/weekly (view_report_weekly).
func handleWeeklySummaryReport(w http.ResponseWriter, r *http.Request) {
	window, err := growthWindowParam(r.URL.Query().Get("window"), growthWindowLongDefault)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	serveReport(w, "weekly", func(q rowQueryer) (any, error) {
		return runWeeklySummaryReport(q, weeklySummaryParams{GrowthWindowDays: window}, time.Now())
	})
}

// handleVSMissesReport is GET /api/reports/vs-misses?from=&to=&weeks=&current=&member_id=
// (view_report_vs_misses).
func handleVSMissesReport(w http.ResponseWriter, r *http.Request) {
	rng, err := reportRangeParamsFromQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := vsMissesParams{Range: rng}
	if v := r.URL.Query().Get("member_id"); v != "" {
		if p.MemberID, err = strconv.Atoi(v); err != nil || p.MemberID < 0 {
			http.Error(w, "member_id must be a member's id", http.StatusBadRequest)
			return
		}
	}
	serveReport(w, "vs-misses", func(q rowQueryer) (any, error) {
		return runVSMissesReport(q, p, time.Now())
	})
}

// handleDonationsReport is GET /api/reports/donations?from=&to=&weeks=&current=&member_id=
// (view_report_donations). Weeks are Monday–Sunday.
func handleDonationsReport(w http.ResponseWriter, r *http.Request) {
	rng, err := reportRangeParamsFromQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p := donationsParams{Range: rng}
	if v := r.URL.Query().Get("member_id"); v != "" {
		if p.MemberID, err = strconv.Atoi(v); err != nil || p.MemberID < 0 {
			http.Error(w, "member_id must be a member's id", http.StatusBadRequest)
			return
		}
	}
	serveReport(w, "donations", func(q rowQueryer) (any, error) {
		return runDonationsReport(q, p, time.Now())
	})
}

// handleParticipationGridReport is GET /api/reports/participation?from=&to=&weeks=&current=
// (view_report_participation).
func handleParticipationGridReport(w http.ResponseWriter, r *http.Request) {
	rng, err := reportRangeParamsFromQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	serveReport(w, "participation", func(q rowQueryer) (any, error) {
		return runParticipationGrid(q, participationGridParams{Range: rng}, time.Now())
	})
}
