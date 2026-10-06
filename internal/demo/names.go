package demo

import (
	"math/rand/v2"
	"strings"
)

// Names are composed, never drawn from any list of players: syllables per script, plus
// the decorations in-game names carry (clan capitals, digits, emoji, underscores). The
// mix of scripts and accents is the point — it exercises the accent-folded search and
// name matching the way a real server's roster does.

var latinOnsets = []string{"k", "v", "th", "dr", "s", "m", "r", "z", "br", "l", "n", "t", "gr", "j", "f", "h", "c", "x", "w", "qu"}
var latinVowels = []string{"a", "e", "i", "o", "u", "ae", "ai", "y", "ia", "ou"}
var latinCodas = []string{"", "n", "r", "th", "x", "l", "s", "nd", "k", "m", "ra", "ion", "or", "en", "yn"}

// Latin with diacritics: the vowel is swapped for an accented one.
var accented = map[string]string{"a": "á", "e": "ë", "i": "í", "o": "ø", "u": "ü", "y": "ÿ"}

// Cyrillic and Greek names take a capitalised first syllable and lowercase ones after.
var cyrillicStart = []string{"Дра", "Вол", "Мир", "Све", "Бор", "Зор", "Ка", "Лю", "Яр"}
var cyrillicRest = []string{"ко", "на", "ра", "тла", "ис", "ян", "тя", "слав", "мир"}
var hangul = []string{"별", "빛", "달", "하", "늘", "바", "람", "용", "사", "자", "호", "랑", "이", "봄"}
var greekStart = []string{"Θεο", "Αρ", "Νύ", "Ζέ", "Ιώ", "Λυ", "Κα"}
var greekRest = []string{"λύ", "κος", "τέ", "μις", "ξ", "φυ", "ρος", "νη"}

var emoji = []string{"🔥", "⚡", "🐺", "🌙", "💀", "🛡", "⭐", "🦅", "🌵", "👑"}
var clanTags = []string{"xX", "Mr", "Lord", "DJ", "Sir", "The", "Big", "Lil"}

func latinWord(r *rand.Rand, syllables int, accent bool) string {
	var b strings.Builder
	for i := 0; i < syllables; i++ {
		b.WriteString(latinOnsets[r.IntN(len(latinOnsets))])
		v := latinVowels[r.IntN(len(latinVowels))]
		if accent && i == syllables-1 {
			if a, ok := accented[v[:1]]; ok {
				v = a + v[1:]
			}
		}
		b.WriteString(v)
	}
	b.WriteString(latinCodas[r.IntN(len(latinCodas))])
	s := b.String()
	return strings.ToUpper(s[:1]) + s[1:]
}

func fromTable(r *rand.Rand, t []string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(t[r.IntN(len(t))])
	}
	return b.String()
}

// composeName builds one name in one of several styles.
func composeName(r *rand.Rand) string {
	switch k := r.IntN(20); {
	case k < 6: // plain Latin
		return latinWord(r, 1+r.IntN(2), false)
	case k < 9: // accented Latin
		return latinWord(r, 1+r.IntN(2), true)
	case k < 10:
		return fromTable(r, cyrillicStart, 1) + fromTable(r, cyrillicRest, 1+r.IntN(2))
	case k < 12:
		return fromTable(r, hangul, 2) + latinWord(r, 1, false)
	case k < 13:
		return fromTable(r, greekStart, 1) + fromTable(r, greekRest, 1+r.IntN(2))
	case k < 15: // clan style
		w := latinWord(r, 2, false)
		if r.IntN(2) == 0 {
			return clanTags[r.IntN(len(clanTags))] + w
		}
		return strings.ToUpper(w)
	case k < 17: // digits
		return latinWord(r, 2, false) + []string{"77", "07", "99", "_x", "88", "21"}[r.IntN(6)]
	case k < 19: // emoji
		e := emoji[r.IntN(len(emoji))]
		if r.IntN(2) == 0 {
			return e + latinWord(r, 2, false)
		}
		return latinWord(r, 2, false) + e
	default: // underscore pair
		return latinWord(r, 1, false) + "_" + latinWord(r, 1, false)
	}
}

// foldKey is a rough accent-and-case fold, used only to keep the generated roster free of
// unintended near-duplicates; the deliberate ones are added afterwards.
func foldKey(s string) string {
	repl := strings.NewReplacer("á", "a", "ë", "e", "í", "i", "ø", "o", "ü", "u", "ÿ", "y")
	return strings.ToLower(repl.Replace(s))
}

// accentTwins are the deliberate near-duplicates: for each, a second member whose name
// differs from an existing one only by an accent. Two members folding to one key is the
// case the folded name-matching tier must refuse to guess between, and the Members
// page's search shows both.
const accentTwins = 2

// generateNames returns n distinct names, the last accentTwins of which are accent twins
// of earlier ones.
func generateNames(n int) []string {
	r := stream("names")
	seen := map[string]bool{}
	var out []string
	for len(out) < n-accentTwins {
		name := composeName(r)
		if len([]rune(name)) < 3 || len([]rune(name)) > 16 || seen[foldKey(name)] {
			continue
		}
		seen[foldKey(name)] = true
		out = append(out, name)
	}
	twins := 0
	for i := 0; twins < accentTwins && i < len(out); i++ {
		base := out[i]
		if foldKey(base) != strings.ToLower(base) || !strings.ContainsAny(base, "aeiou") {
			continue // already accented, or nothing to accent
		}
		// An ordered list, not a map: map order is random and the roster must not be.
		for _, sw := range [][2]string{{"a", "á"}, {"e", "ë"}, {"o", "ø"}} {
			plain, acc := sw[0], sw[1]
			if idx := strings.LastIndex(base, plain); idx > 0 {
				out = append(out, base[:idx]+acc+base[idx+len(plain):])
				twins++
				break
			}
		}
	}
	return out
}
