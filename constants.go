package main

const (
	SessionMaxAge     = 86400   // 24 hours in seconds
	MaxFileUploadSize = 50 << 20 // 50 MB
	MaxCSVUploadSize  = 10 << 20 // 10 MB
	MinSessionKeyLen  = 32
)

// ValidRanks is the ordered list of member ranks (highest to lowest).
var ValidRanks = []string{"R5", "R4", "R3", "R2", "R1"}
