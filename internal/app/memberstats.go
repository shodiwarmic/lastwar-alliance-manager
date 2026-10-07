// memberstats.go - the member-stat categories a scan can record, and their rules.

package app

import (
	"fmt"
	"strconv"
)

// memberStat is one per-member history a scan writes: its table and value column, and
// the smallest value that is a reading rather than a misread.
type memberStat struct {
	Table  string
	Column string
	Min    int64
}

// memberStats maps a commit category to the history it writes. Every write goes through
// recordHistoryIfChanged, so an unchanged reading adds no row.
var memberStats = map[string]memberStat{
	"power": {Table: "power_history", Column: "power", Min: 1},
	"kills": {Table: "kill_history", Column: "kills", Min: 0},
}

// checkMemberStat validates a reading against its category's rule, worded for errors[].
func checkMemberStat(category string, value int64) error {
	st, ok := memberStats[category]
	if !ok {
		return fmt.Errorf("unknown member-stat category %q", category)
	}
	if value < st.Min {
		return fmt.Errorf("%s %d is below %s", category, value, strconv.FormatInt(st.Min, 10))
	}
	return nil
}
