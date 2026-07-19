package main

import "time"

const (
	SessionMaxAge     = 86400    // 24 hours in seconds
	MaxFileUploadSize = 50 << 20 // 50 MB
	MaxCSVUploadSize  = 10 << 20 // 10 MB
	MinSessionKeyLen  = 32
)

// FileEditLogWindow throttles the "updated file" activity row written on each
// Collabora/WOPI save. Collabora autosaves every few seconds and the activity
// batcher only coalesces "created" rows, so an activity entry is written at most
// once per file per window (the updated_at bump itself is unthrottled).
const FileEditLogWindow = 10 * time.Minute

// ValidRanks is the ordered list of member ranks (highest to lowest).
var ValidRanks = []string{"R5", "R4", "R3", "R2", "R1"}

// FileTagColors is the server-side allowlist for a file_tag's color column. Each
// value is a semantic token key (see styles.css / files.css) — never a raw hex
// value, so tag chips and badges adapt to light/dark themes.
var FileTagColors = []string{"neutral", "info", "success", "warning", "danger", "purple"}

// rankTier maps a rank string to its numeric tier for comparisons. "Admin" is a
// synthetic top tier used for admin accounts (which have no member rank). Unknown
// strings return 0, which never satisfies a >= check against a real rank.
// (Distinct from handlers_train.go's rankValue, which is float64 and R1–R5 only.)
func rankTier(rank string) int {
	switch rank {
	case "R1":
		return 1
	case "R2":
		return 2
	case "R3":
		return 3
	case "R4":
		return 4
	case "R5":
		return 5
	case "Admin":
		return 6
	}
	return 0
}

// CareerTypeLabels maps LastRank career_type codes to profession names.
// 103 (Diplomat) exists in the game but has not been released as of Season 6.
var CareerTypeLabels = map[int]string{
	101: "Engineer",
	102: "War Leader",
}

// CareerTypeLabel returns the profession name for a career_type code, or
// "Unknown" if the code is not recognised. Callers that write the result back to
// members.profession must instead gate on the map's comma-ok (see the LastRank
// sync) so an unset/unknown code never clobbers a real profession with "Unknown".
func CareerTypeLabel(code int) string {
	if label, ok := CareerTypeLabels[code]; ok {
		return label
	}
	return "Unknown"
}
