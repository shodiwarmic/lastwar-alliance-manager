package main

const (
	SessionMaxAge     = 86400    // 24 hours in seconds
	MaxFileUploadSize = 50 << 20 // 50 MB
	MaxCSVUploadSize  = 10 << 20 // 10 MB
	MinSessionKeyLen  = 32
)

// ValidRanks is the ordered list of member ranks (highest to lowest).
var ValidRanks = []string{"R5", "R4", "R3", "R2", "R1"}

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
