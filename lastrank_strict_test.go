package main

import "testing"

func TestParseLastRankAllianceStrict(t *testing.T) {
	const hex = "7dd57e38b63d4a3989a0f603156398b3"
	cases := []struct {
		in     string
		wantID string
		wantOK bool
	}{
		{hex, hex, true},                                   // bare hex
		{"  " + hex + "\n", hex, true},                     // trimmed
		{"https://lastrank.fun/a/" + hex, hex, true},       // approved host
		{"https://www.lastrank.fun/a/" + hex, hex, true},   // approved www host
		{"https://evil.example/a/" + hex, "", false},       // wrong host — rejected (unlike the lax parser)
		{"lol /a/" + hex + " lol", "", false},              // stray text — rejected
		{"https://lastrank.fun/x/" + hex, "", false},       // wrong path
		{"https://lastrank.fun/a/" + hex + "/extra", "", false}, // path not exact
		{"not-a-hex", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		id, ok := parseLastRankAllianceStrict(c.in)
		if ok != c.wantOK || id != c.wantID {
			t.Errorf("parseLastRankAllianceStrict(%q) = (%q,%v), want (%q,%v)", c.in, id, ok, c.wantID, c.wantOK)
		}
	}
}
