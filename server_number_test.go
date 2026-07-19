package main

import "testing"

func TestParseServerNumber(t *testing.T) {
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		// The shapes officers actually type. The ally modal placeholder used to read "e.g. S123",
		// so S-prefixed values are in the live data.
		{"1712", 1712, true},
		{"S1712", 1712, true},
		{"s1712", 1712, true},
		{"#1712", 1712, true},
		{" s1712 ", 1712, true},
		{"S 1712", 1712, true},
		{"Server 1712", 1712, true},

		// First contiguous run only. Stripping every digit instead would fuse these into
		// 17121713 and 123456 — corrupt server numbers that match nothing but look plausible.
		{"1712 / 1713", 1712, true},
		{"123S456", 123, true},

		// No digits, or a value that can't be a server.
		{"", 0, false},
		{"abc", 0, false},
		{"S", 0, false},
		{"0", 0, false},
	}
	for _, tc := range tests {
		got, ok := parseServerNumber(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseServerNumber(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A non-ASCII numeral must not shadow the ASCII digits after it. unicode.IsDigit matches ٣
// (Arabic-Indic three), but the ASCII extraction loop cannot — so an IndexFunc-based start would
// point at a byte the loop rejects, silently returning ok=false past a perfectly good "1712".
func TestParseServerNumberNonASCIIDigit(t *testing.T) {
	if got, ok := parseServerNumber("٣ 1712"); !ok || got != 1712 {
		t.Errorf("parseServerNumber(\"٣ 1712\") = (%d, %v), want (1712, true)", got, ok)
	}
}
