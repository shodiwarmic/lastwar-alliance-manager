package lastrank

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	rePlayerURL   = regexp.MustCompile(`/p/(\d+)`)
	reBareInt     = regexp.MustCompile(`^\s*(\d+)\s*$`)
	reAllianceURL = regexp.MustCompile(`/a/([0-9a-fA-F]{32})`)
	reBareHex     = regexp.MustCompile(`^\s*([0-9a-fA-F]{32})\s*$`)
	reStrictPath  = regexp.MustCompile(`^/a/([0-9a-fA-F]{32})$`)
)

// allowedHosts are the only hosts a pasted opponent URL may come from (F-R05).
var allowedHosts = map[string]bool{"lastrank.fun": true, "www.lastrank.fun": true}

// ParsePlayerID accepts a full player URL (…/p/1585224) or a bare integer id and
// returns the numeric public_id.
func ParsePlayerID(input string) (int, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, false
	}
	if m := rePlayerURL.FindStringSubmatch(input); m != nil {
		if id, err := strconv.Atoi(m[1]); err == nil {
			return id, true
		}
	}
	if m := reBareInt.FindStringSubmatch(input); m != nil {
		if id, err := strconv.Atoi(m[1]); err == nil {
			return id, true
		}
	}
	return 0, false
}

// ParseAllianceID accepts a full alliance URL (…/a/<32-hex>) or a bare 32-char hex id
// and returns the lowercased id. Deliberately lax — see ParseAllianceIDStrict.
func ParseAllianceID(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	if m := reAllianceURL.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	if m := reBareHex.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	return "", false
}

// ParseAllianceIDStrict is the STRICT variant used by the VS League opponent lookup and
// the Scout Report: it accepts a bare 32-hex id, or a URL on an approved LastRank host
// whose path is exactly /a/<hex>. ParseAllianceID stays lax for its existing callers;
// this one stops an officer pasting a URL from an unrelated site that merely contains
// /a/<hex>.
func ParseAllianceIDStrict(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	if m := reBareHex.FindStringSubmatch(input); m != nil {
		return strings.ToLower(m[1]), true
	}
	u, err := url.Parse(input)
	if err != nil || !allowedHosts[strings.ToLower(u.Hostname())] {
		return "", false
	}
	if m := reStrictPath.FindStringSubmatch(u.Path); m != nil {
		return strings.ToLower(m[1]), true
	}
	return "", false
}
