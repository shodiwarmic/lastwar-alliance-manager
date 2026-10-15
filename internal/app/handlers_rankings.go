package app

import (
	"net/http"
	"strconv"
	"time"
)

// growthWindowParam reads a growth window in days: blank → def, 1–365, anything else an error.
func growthWindowParam(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < growthWindowMin || n > growthWindowMax {
		return 0, badReportParam("growth windows are whole days from 1 to 365")
	}
	return n, nil
}

// getMemberRankings is GET /api/rankings?short=&long= (view_rankings): the Tracking page's
// growth table, over windows the page chooses (default 7 and 30 days).
func getMemberRankings(w http.ResponseWriter, r *http.Request) {
	var p growthTableParams
	var err error
	if p.ShortDays, err = growthWindowParam(r.URL.Query().Get("short"), growthWindowShortDefault); err == nil {
		p.LongDays, err = growthWindowParam(r.URL.Query().Get("long"), growthWindowLongDefault)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	serveReport(w, "growth", func(q rowQueryer) (any, error) {
		return runGrowthTableReport(q, p, time.Now())
	})
}
