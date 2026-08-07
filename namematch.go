package main

// namematch.go — accent-insensitive name matching for roster resolution.
//
// SQLite's LOWER() and the NOCASE collation are both ASCII-only, so a roster
// "Pàcha" never matches an incoming "Pacha" (or vice versa). In the LastRank
// sync that miss costs twice over: the member lands in "Unmatched names" AND —
// because nothing confirmed them — in "Possibly left the alliance", inviting an
// officer to archive an active member over a stray accent.
//
// This file provides the folded fallback tier. It is strictly additive: it only
// ever runs after exact and alias matching have both missed, so it can turn a
// miss into a match but can never change an existing match. Ambiguity is
// resolved by refusing to guess — see foldedNameIndex.lookup.

import (
	"database/sql"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// foldName strips diacritics and lowercases, so "Pàcha" and "PACHA" both fold to
// "pacha". This is the Go counterpart of window.foldSearch in static/global.js;
// the two must agree, or client-side search and server-side matching disagree
// about what counts as the same name.
//
// NFD decomposition splits "à" into "a" + U+0300, and everything NFD produces for
// an accented Latin letter is a nonspacing mark (unicode.Mn), so dropping Mn
// leaves the base letters. (JS uses \p{Diacritic}, which additionally covers a
// few spacing modifiers; for player names the two are equivalent, and Mn is the
// safer of the pair because it never strips a character that carries meaning on
// its own.)
func foldName(s string) string {
	decomposed := norm.NFD.String(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// foldedNameIndex maps a folded name to every member reachable under it, via
// either a primary name or a visible alias.
//
// Build it ONCE per batch and pass it down. resolveMemberAlias is called once per
// incoming row inside loops over the whole roster (the LastRank preview, OCR/VS
// import, mobilePreview), so rebuilding it per lookup would be O(N²) queries on
// an app that holds exactly one database connection.
type foldedNameIndex struct {
	byFolded map[string][]*Member
}

// buildFoldedNameIndex loads every (member, matchable-name) pair in one query and
// folds them into the lookup map. Alias visibility mirrors resolveMemberAlias:
// this user's personal aliases plus all global/OCR ones, never another user's
// personal alias.
//
// Archived (rank 'EX') members are deliberately included — tiers 1 and 2 match
// them too, and excluding them here would make the fallback tier answer a
// different question than the tiers it backs up.
func buildFoldedNameIndex(tx *sql.Tx, currentUserID int) (*foldedNameIndex, error) {
	rows, err := tx.Query(`
		SELECT m.id, m.name, m.rank, m.name AS match_key
		FROM members m
		UNION ALL
		SELECT m.id, m.name, m.rank, a.alias AS match_key
		FROM member_aliases a
		JOIN members m ON a.member_id = m.id
		WHERE a.user_id = ? OR a.user_id IS NULL`, currentUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	idx := &foldedNameIndex{byFolded: make(map[string][]*Member)}
	for rows.Next() {
		var m Member
		var key string
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank, &key); err != nil {
			return nil, err
		}
		folded := foldName(key)
		if folded == "" {
			continue
		}
		idx.add(folded, m)
	}
	return idx, rows.Err()
}

// add records a member under a folded key, deduping by member id so that a member
// whose primary name and alias fold alike doesn't look like two candidates.
func (idx *foldedNameIndex) add(folded string, m Member) {
	for _, existing := range idx.byFolded[folded] {
		if existing.ID == m.ID {
			return
		}
	}
	member := m
	idx.byFolded[folded] = append(idx.byFolded[folded], &member)
}

// lookup returns the single member matching name once folded, or nil.
//
// A folded key reaching two or more distinct members is reported as no match, not
// as a guess. If a roster genuinely holds both "José" and "Jose", picking either
// would silently attribute one player's stats to the other; leaving the row
// unmatched puts it in front of an officer instead, which is the outcome this
// whole tier exists to produce.
func (idx *foldedNameIndex) lookup(name string) (*Member, bool) {
	if idx == nil {
		return nil, false
	}
	folded := foldName(name)
	if folded == "" {
		return nil, false
	}
	candidates := idx.byFolded[folded]
	if len(candidates) != 1 {
		return nil, false
	}
	return candidates[0], true
}
