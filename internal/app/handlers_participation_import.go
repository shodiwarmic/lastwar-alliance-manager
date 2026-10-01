package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode" // the mail's phone-local time is converted with the browser's zone rules
)

// handlers_participation_import.go — importing a participation board from the
// post-event mail's screenshots (private-docs 171).
//
// Like the CSV import it SAVES NOTHING: the rows come back for the recording
// screen's check table, and the ordinary board PUT saves them. What it adds:
//
//   - the frames are read by the OCR service as the type's mail category
//     (participation_types.ocr_category, migration 082), after /health has said
//     the service can read it;
//   - rows from overlapping frames are merged by rank and by name, and every
//     disagreement is flagged, never resolved by a guess;
//   - imported ranks are kept as read — the board is not renumbered by position;
//   - the mail's own timestamp suggests which occurrence the board belongs to.

// Cloud Run caps an HTTP/1 request body at 32 MiB. A 33-frame Zombie Siege board
// is 15 MiB from a 1080x2404 phone and ~23 MiB from a 1320x2868 one, so the
// frames go in chunks of at most 20 MiB.
const ocrImportChunkBytes = 20 << 20

const maxImportFrames = 100

// mailLabels name a mail category for messages.
var mailLabels = map[string]string{
	"alliance_exercise": "Alliance Exercise",
	"zombie_siege":      "Zombie Siege",
	"desert_storm":      "Desert Storm",
}

// ptImportRow is one merged row of an imported board.
type ptImportRow struct {
	Rank         *int              `json:"rank"`
	RankInferred bool              `json:"rank_inferred,omitempty"`
	Name         string            `json:"name"`
	MemberID     *int              `json:"member_id"`
	MemberName   string            `json:"member_name"`
	MemberRank   string            `json:"member_rank"`
	How          string            `json:"how"`
	Values       map[string]*int64 `json:"values"`
	// Flags are the reasons this row needs an officer: rank_conflict,
	// rank_disagreement, rank_unread, score_unread, name_variants.
	Flags []string `json:"flags"`
	// Variants are the other spellings OCR gave this row's name on other frames.
	Variants []string `json:"variants,omitempty"`
}

// --- Chunking ----------------------------------------------------------------------

// chunkFrames splits the frames, in order, into runs of at most limit bytes. A
// single frame over the limit travels alone.
func chunkFrames(files []*multipart.FileHeader, limit int64) [][]*multipart.FileHeader {
	var out [][]*multipart.FileHeader
	var cur []*multipart.FileHeader
	var size int64
	for _, f := range files {
		if len(cur) > 0 && size+f.Size > limit {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, f)
		size += f.Size
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// --- Merging -----------------------------------------------------------------------

type importRead struct {
	name string
	key  string // lower-cased name
}

func hasFlag(flags []string, f string) bool {
	for _, x := range flags {
		if x == f {
			return true
		}
	}
	return false
}

func addFlag(flags *[]string, f string) {
	if !hasFlag(*flags, f) {
		*flags = append(*flags, f)
	}
}

// mergeImportedRows turns every frame's rows into one board. Frames overlap, so
// the same row is usually read more than once:
//
//   - same name, same rank → one row (a read score beats an unread one);
//   - same name at two ranks → one row at the rank read more often (a read rank
//     beats an inferred one), flagged rank_disagreement;
//   - same rank, different names → both kept, flagged rank_conflict;
//   - no rank at all → placed after the row read before it, flagged rank_unread;
//   - the same row read with different trailing marks on different frames
//     ("Ragnarocket 뀨우" / "Ragnarocket #¦") → one row, flagged name_variants —
//     only when the scores are read and equal, the ranks are equal (or one is
//     unread), and one name's letters and digits are a prefix of the other's.
//
// Nothing is renumbered and no value is invented.
func mergeImportedRows(players []OCRPlayer, primaryKey string) []ptImportRow {
	type group struct {
		first       importRead
		rankVotes   map[int]int
		readRanks   map[int]bool
		score       int64
		scoreRead   bool
		order       int
		predecessor int // order of the read before this name first appeared
		reads       int
		variants    []string
		mergedInto  *group
	}
	groups := map[string]*group{}
	var keys []string
	for i, p := range players {
		name := strings.TrimSpace(ptTagPrefix.ReplaceAllString(strings.TrimSpace(p.PlayerName), ""))
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		g, ok := groups[key]
		if !ok {
			g = &group{first: importRead{name: name, key: key}, rankVotes: map[int]int{},
				readRanks: map[int]bool{}, order: i, predecessor: i - 1}
			groups[key] = g
			keys = append(keys, key)
		}
		if p.Rank != nil {
			weight := 1
			if !p.RankInferred {
				weight = 100 // a read rank outvotes any number of inferred ones
				g.readRanks[*p.Rank] = true
			}
			g.rankVotes[*p.Rank] += weight
		}
		g.reads++
		if !p.ScoreUnread && !g.scoreRead {
			g.score, g.scoreRead = p.Score, true
		}
	}

	// One row read differently on different frames: fold the variant groups
	// together (see above). The name kept is the one read most often.
	bestRankOf := func(g *group) (int, bool) {
		best, votes := 0, 0
		for r, v := range g.rankVotes {
			if v > votes || (v == votes && r < best) {
				best, votes = r, v
			}
		}
		return best, votes > 0
	}
	root := func(g *group) *group {
		for g.mergedInto != nil {
			g = g.mergedInto
		}
		return g
	}
	for i, ka := range keys {
		for _, kb := range keys[i+1:] {
			a, b := root(groups[ka]), root(groups[kb])
			if a == b || !a.scoreRead || !b.scoreRead || a.score != b.score || !nameVariant(a.first.key, b.first.key) {
				continue
			}
			ra, okA := bestRankOf(a)
			rb, okB := bestRankOf(b)
			// Equal ranks, or one unread: scores tie constantly on a Zombie Siege
			// board, so a shared score and a shared start are not enough alone.
			if okA && okB && ra != rb {
				continue
			}
			keep, drop := a, b
			if b.reads > a.reads {
				keep, drop = b, a
			}
			for r, v := range drop.rankVotes {
				keep.rankVotes[r] += v
			}
			for r := range drop.readRanks {
				keep.readRanks[r] = true
			}
			keep.reads += drop.reads
			keep.variants = append(keep.variants, drop.first.name)
			keep.variants = append(keep.variants, drop.variants...)
			if drop.order < keep.order {
				keep.order, keep.predecessor = drop.order, drop.predecessor
			}
			drop.mergedInto = keep
		}
	}
	kept := keys[:0:0]
	for _, k := range keys {
		if groups[k].mergedInto == nil {
			kept = append(kept, k)
		}
	}
	keys = kept

	rows := make([]ptImportRow, 0, len(keys))
	orders := make([]int, 0, len(keys))
	preds := make([]int, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		row := ptImportRow{Name: g.first.name, How: "none", Values: map[string]*int64{}, Flags: []string{}, Variants: g.variants}
		if len(g.variants) > 0 {
			addFlag(&row.Flags, "name_variants")
		}
		best, bestVotes := 0, 0
		for r, v := range g.rankVotes {
			if v > bestVotes || (v == bestVotes && r < best) {
				best, bestVotes = r, v
			}
		}
		if bestVotes > 0 {
			rank := best
			row.Rank = &rank
			row.RankInferred = !g.readRanks[best]
			if len(g.readRanks) > 1 {
				addFlag(&row.Flags, "rank_disagreement")
			}
		} else {
			addFlag(&row.Flags, "rank_unread")
		}
		if g.scoreRead {
			v := g.score
			row.Values[primaryKey] = &v
		} else {
			row.Values[primaryKey] = nil
			addFlag(&row.Flags, "score_unread")
		}
		rows = append(rows, row)
		orders = append(orders, g.order)
		preds = append(preds, g.predecessor)
	}

	// Same rank, different names: both stay, both flagged.
	byRank := map[int][]int{}
	for i, r := range rows {
		if r.Rank != nil {
			byRank[*r.Rank] = append(byRank[*r.Rank], i)
		}
	}
	for _, idx := range byRank {
		if len(idx) > 1 {
			for _, i := range idx {
				addFlag(&rows[i].Flags, "rank_conflict")
			}
		}
	}

	// Order: ranked rows by rank (then first appearance); an unranked row right
	// after the nearest earlier read whose row has a rank.
	readKey := make([]string, len(players))
	for i, p := range players {
		readKey[i] = strings.ToLower(strings.TrimSpace(ptTagPrefix.ReplaceAllString(strings.TrimSpace(p.PlayerName), "")))
	}
	rowOf := map[string]int{}
	for i, key := range keys {
		rowOf[key] = i
	}
	for key, g := range groups { // a folded variant's reads place like its row's
		if g.mergedInto != nil {
			for r := g.mergedInto; ; r = r.mergedInto {
				if r.mergedInto == nil {
					rowOf[key] = rowOf[r.first.key]
					break
				}
			}
		}
	}
	type placed struct {
		row  ptImportRow
		sort float64
		tie  int
	}
	out := make([]placed, 0, len(rows))
	for i, r := range rows {
		if r.Rank != nil {
			out = append(out, placed{r, float64(*r.Rank), orders[i]})
			continue
		}
		after := 0.0
		for o := preds[i]; o >= 0; o-- {
			if j, ok := rowOf[readKey[o]]; ok && rows[j].Rank != nil {
				after = float64(*rows[j].Rank)
				break
			}
		}
		out = append(out, placed{r, after + 0.5, orders[i]})
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].sort != out[b].sort {
			return out[a].sort < out[b].sort
		}
		return out[a].tie < out[b].tie
	})
	final := make([]ptImportRow, len(out))
	for i, p := range out {
		final[i] = p.row
	}
	return final
}

// nameKey is a name's letters and digits, lower-cased: what is left when the
// marks OCR reads differently from frame to frame are taken away.
func nameKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nameVariant reports whether two names are one name read differently: one's
// letters and digits are a prefix of the other's, at least four long.
func nameVariant(a, b string) bool {
	ka, kb := nameKey(a), nameKey(b)
	if len([]rune(ka)) > len([]rune(kb)) {
		ka, kb = kb, ka
	}
	return len([]rune(ka)) >= 4 && strings.HasPrefix(kb, ka)
}

// rankRanges renders sorted ranks compactly: 4, 5, 24, 27–242 → "4–5, 24, 27–242".
func rankRanges(ranks []int) string {
	var parts []string
	for i := 0; i < len(ranks); {
		j := i
		for j+1 < len(ranks) && ranks[j+1] == ranks[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, strconv.Itoa(ranks[i])+"–"+strconv.Itoa(ranks[j]))
		} else {
			parts = append(parts, strconv.Itoa(ranks[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

// importProblems are the board-level checks: gaps in the ranks, scores out of
// order (the board is sorted best first; ties allowed), and every row that will
// block saving, worded as the board PUT words it.
func importProblems(rows []ptImportRow, t *ptType) []ptCSVProblem {
	problems := []ptCSVProblem{}
	primary := t.primaryKey()
	label := primary
	if len(t.Trackables) > 0 {
		label = t.Trackables[0].Label
	}

	ranks := map[int]bool{}
	for _, r := range rows {
		if r.Rank != nil {
			ranks[*r.Rank] = true
		}
	}
	if len(ranks) > 0 {
		max := 0
		for r := range ranks {
			if r > max {
				max = r
			}
		}
		var missing []int
		for r := 1; r <= max; r++ {
			if !ranks[r] {
				missing = append(missing, r)
			}
		}
		if len(missing) > 0 {
			problems = append(problems, ptCSVProblem{0, "No frame showed rank " + rankRanges(missing) +
				" — add the missing screenshots or the row by hand (a very high rank on its own is usually a misread one)"})
		}
	}

	var prev *int64
	var prevName string
	for _, r := range rows {
		v := r.Values[primary]
		if v == nil {
			continue
		}
		if prev != nil && *v > *prev {
			problems = append(problems, ptCSVProblem{0, fmt.Sprintf("%s (%d) is above %s (%d), but the board is sorted highest first — check both values",
				r.Name, *v, prevName, *prev)})
		}
		prev, prevName = v, r.Name
	}

	seen := map[int]bool{}
	for _, r := range rows {
		if hasFlag(r.Flags, "rank_conflict") && r.Rank != nil && !seen[*r.Rank] {
			seen[*r.Rank] = true
			problems = append(problems, ptCSVProblem{0, "Rank " + strconv.Itoa(*r.Rank) + " appears twice"})
		}
		if hasFlag(r.Flags, "score_unread") {
			problems = append(problems, ptCSVProblem{0, r.Name + " needs a value for " + label})
		}
	}
	return problems
}

// --- The mail's time and the occurrence it belongs to --------------------------------

// mailTimestamp is the modal timestamp across the frames, or "" with the reason
// there is none to go by.
func mailTimestamp(sections []OCRSectionDiagnostic) (string, string) {
	counts := map[string]int{}
	for _, s := range sections {
		if s.MailTimestamp != nil && *s.MailTimestamp != "" {
			counts[*s.MailTimestamp]++
		}
	}
	if len(counts) == 0 {
		return "", "no frame showed the mail's date and time"
	}
	if len(counts) > 1 {
		return "", "the frames show different mail times — more than one mail may be mixed in"
	}
	for ts := range counts {
		return ts, ""
	}
	return "", ""
}

// mailInstant turns the mail's phone-local timestamp into an instant, with the
// browser's zone (its IANA name, so the offset is the one in force on the mail's
// date) or, failing that, the offset in minutes the browser sent.
func mailInstant(ts, tz string, offsetMinutes int) (time.Time, error) {
	loc := time.FixedZone("browser", offsetMinutes*60)
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return time.ParseInLocation("2006-01-02 15:04:05", ts, loc)
}

type ptOccurrence struct {
	EventID   int     `json:"event_id"`
	EventDate string  `json:"event_date"`
	EventTime string  `json:"event_time"`
	AllDay    bool    `json:"all_day"`
	TaskForce *string `json:"task_force"`
	HasBoard  bool    `json:"has_board"`
}

// start is when the occurrence began, in game time: an all-day occurrence is its
// whole game day, so it starts at the day's 00:00.
func (o ptOccurrence) start() (time.Time, error) {
	clock := o.EventTime
	if o.AllDay || clock == "" {
		clock = "00:00"
	}
	return time.ParseInLocation("2006-01-02 15:04", o.EventDate+" "+clock, gameLoc)
}

type ptSuggestion struct {
	EventID   *int    `json:"event_id"`
	TaskForce *string `json:"task_force"`
	Reason    string  `json:"reason"`
}

const suggestionWindow = 72 * time.Hour

// suggestOccurrences returns the occurrences that started before the mail and
// within three days of it, nearest first. Several can tie (two task forces at one
// time, or two all-day rows).
func suggestOccurrences(mail time.Time, occs []ptOccurrence) []ptOccurrence {
	var best []ptOccurrence
	var bestStart time.Time
	for _, o := range occs {
		s, err := o.start()
		if err != nil || s.After(mail) || mail.Sub(s) > suggestionWindow {
			continue
		}
		switch {
		case len(best) == 0 || s.After(bestStart):
			best, bestStart = []ptOccurrence{o}, s
		case s.Equal(bestStart):
			best = append(best, o)
		}
	}
	return best
}

// taskForceByMembers picks the Desert Storm task force whose planner lineup holds
// most of the board's matched members, or "" when neither clearly does.
func taskForceByMembers(q rowQueryer, rows []ptImportRow) (string, error) {
	matched := map[int]bool{}
	for _, r := range rows {
		if r.MemberID != nil {
			matched[*r.MemberID] = true
		}
	}
	if len(matched) == 0 {
		return "", nil
	}
	count := map[string]int{}
	for _, tf := range []string{"A", "B"} {
		roles, err := loadRolePrefill(q, tf)
		if err != nil {
			return "", err
		}
		for _, rl := range roles {
			if matched[rl.MemberID] {
				count[tf]++
			}
		}
	}
	// A clear majority of the matched members, not a near tie.
	switch {
	case count["A"] > count["B"] && count["A"]*2 > len(matched):
		return "A", nil
	case count["B"] > count["A"] && count["B"]*2 > len(matched):
		return "B", nil
	}
	return "", nil
}

func loadImportCandidates(typeID int, mailDate string) ([]ptOccurrence, error) {
	from := gameDate()
	if mailDate != "" {
		from = mailDate
	}
	rows, err := db.Query(`
		SELECT se.id, se.event_date, se.event_time, se.all_day, se.task_force, b.id IS NOT NULL
		FROM schedule_events se
		LEFT JOIN participation_boards b ON b.schedule_event_id = se.id
		WHERE se.event_type_id = ? AND se.event_date <= ? AND se.event_date >= date(?, '-4 days')
		ORDER BY se.event_date DESC, se.event_time DESC, se.task_force`, typeID, from, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ptOccurrence{}
	for rows.Next() {
		var o ptOccurrence
		if err := rows.Scan(&o.EventID, &o.EventDate, &o.EventTime, &o.AllDay, &o.TaskForce, &o.HasBoard); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// --- The handler -------------------------------------------------------------------

// POST /api/participation/import — multipart images[], event_type_id, optional
// event_id (importing onto an occurrence already chosen), tz (IANA) and tz_offset
// (minutes east of UTC). Returns the CSV import's {rows, problems} plus the mail's
// timestamp, a suggested occurrence and the candidates.
func handleParticipationImport(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, 150<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Unable to read the upload — at most 100 screenshots, 150 MB in all", http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		files = r.MultipartForm.File["images[]"]
	}
	if len(files) == 0 {
		http.Error(w, "Pick the mail's screenshots to import", http.StatusBadRequest)
		return
	}
	if len(files) > maxImportFrames {
		http.Error(w, "At most "+strconv.Itoa(maxImportFrames)+" screenshots in one import", http.StatusBadRequest)
		return
	}
	typeID, _ := strconv.Atoi(r.FormValue("event_type_id"))
	eventID, _ := strconv.Atoi(r.FormValue("event_id"))
	offset, _ := strconv.Atoi(r.FormValue("tz_offset"))
	tz := strings.TrimSpace(r.FormValue("tz"))

	types, err := loadParticipationTypes(db)
	if err != nil {
		slog.Error("handleParticipationImport: types failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	t := types[typeID]
	if t == nil {
		http.Error(w, "That event type does not track participation", http.StatusBadRequest)
		return
	}
	if t.OCRCategory == "" {
		http.Error(w, t.Name+" has no mail that can be read from screenshots — use a CSV or add the rows by hand", http.StatusBadRequest)
		return
	}
	if eventID > 0 {
		var etype int
		if err := db.QueryRow(`SELECT event_type_id FROM schedule_events WHERE id = ?`, eventID).Scan(&etype); err != nil || etype != typeID {
			http.Error(w, "That occurrence is not a "+t.Name, http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Minute)
	defer cancel()

	// Ask before uploading: an older service cannot read a mail, and says so only
	// after spending Vision units on every frame.
	info, err := ocrServiceInfo(ctx)
	var down *OCRUnreachableError
	if errors.As(err, &down) {
		http.Error(w, "OCR service unreachable, try again", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		slog.Error("handleParticipationImport: service info failed", "error", err)
		http.Error(w, "OCR service unreachable, try again", http.StatusServiceUnavailable)
		return
	}
	label := mailLabels[t.OCRCategory]
	if label == "" {
		label = t.OCRCategory
	}
	if !info.Speaks() || !info.Reads(t.OCRCategory) {
		http.Error(w, fmt.Sprintf("This OCR service can't read %s mails yet; it needs an OCR service release that lists %s — v1.0.0 or later (it runs %s).",
			label, t.OCRCategory, info.Version), http.StatusConflict)
		return
	}

	var players []OCRPlayer
	var sections []OCRSectionDiagnostic
	var summaries []string
	for _, chunk := range chunkFrames(files, ocrImportChunkBytes) {
		res, diag, err := ProcessImagesForCategory(ctx, chunk, t.OCRCategory)
		if err != nil {
			var refusal *OCRServiceError
			if errors.As(err, &refusal) {
				http.Error(w, refusal.Message, http.StatusBadGateway)
				return
			}
			slog.Error("handleParticipationImport: OCR failed", "error", err)
			http.Error(w, "The OCR service could not read the screenshots — try again", http.StatusBadGateway)
			return
		}
		players = append(players, res[t.OCRCategory]...)
		if diag != nil {
			sections = append(sections, diag.Sections...)
			summaries = append(summaries, summarizeOCRDiagnostics(diag))
		}
	}

	rows := mergeImportedRows(players, t.primaryKey())
	if len(rows) == 0 {
		http.Error(w, "No rows could be read from those screenshots — are they "+label+" mails?", http.StatusUnprocessableEntity)
		return
	}
	names := make([]string, len(rows))
	for i, row := range rows {
		names[i] = row.Name
	}
	matches, err := resolveBoardNames(u.ID, names)
	if err != nil {
		slog.Error("handleParticipationImport: resolve failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	problems := importProblems(rows, t)
	usedBy := map[int]string{}
	for i := range rows {
		m := matches[i]
		rows[i].How = m.How
		if m.MemberID == nil {
			continue
		}
		if first, taken := usedBy[*m.MemberID]; taken {
			rows[i].How = "none"
			problems = append(problems, ptCSVProblem{0, rows[i].Name + " matches " + m.MemberName + ", already matched to " + first + " — pick the right member"})
			continue
		}
		usedBy[*m.MemberID] = rows[i].Name
		rows[i].MemberID, rows[i].MemberName, rows[i].MemberRank = m.MemberID, m.MemberName, m.MemberRank
	}
	for _, s := range sections {
		if s.Note != nil && *s.Note == "no_rows_below_header" {
			problems = append(problems, ptCSVProblem{0, s.Image + " shows the mail with its list collapsed — no rows were read from it"})
		}
	}

	ts, tsReason := mailTimestamp(sections)
	var mailDate string
	var instant time.Time
	if ts != "" {
		if instant, err = mailInstant(ts, tz, offset); err != nil {
			ts, tsReason = "", "the mail's time could not be read"
		} else {
			mailDate = instant.In(gameLoc).Format("2006-01-02")
		}
	}
	candidates, err := loadImportCandidates(typeID, mailDate)
	if err != nil {
		slog.Error("handleParticipationImport: candidates failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	suggested := ptSuggestion{Reason: tsReason}
	switch {
	case eventID > 0:
		id := eventID
		suggested = ptSuggestion{EventID: &id, Reason: "the occurrence you are recording"}
	case ts != "":
		near := suggestOccurrences(instant, candidates)
		if len(near) > 1 && t.AbsenceRule == ruleRole {
			// Two task forces at one time: settle it by who is on the board.
			tf, err := taskForceByMembers(db, rows)
			if err != nil {
				slog.Error("handleParticipationImport: planner read failed", "error", err)
			}
			var picked []ptOccurrence
			for _, o := range near {
				if o.TaskForce != nil && *o.TaskForce == tf {
					picked = append(picked, o)
				}
			}
			near = picked
		}
		switch len(near) {
		case 0:
			suggested.Reason = "no " + t.Name + " occurrence started in the three days before the mail (" + ts + ") — create one"
		case 1:
			id := near[0].EventID
			suggested = ptSuggestion{EventID: &id, TaskForce: near[0].TaskForce,
				Reason: "the nearest " + t.Name + " before the mail (" + ts + ")"}
		default:
			suggested.Reason = "several occurrences fit the mail's time (" + ts + ") — pick one"
		}
	}
	if suggested.EventID == nil && t.AbsenceRule == ruleRole {
		// Creating an occurrence: offer the task force the matched members say.
		if tf, err := taskForceByMembers(db, rows); err == nil && tf != "" {
			suggested.TaskForce = &tf
		}
	}

	writeJSON(w, map[string]any{
		"rows":           rows,
		"problems":       problems,
		"category":       t.OCRCategory,
		"mail_timestamp": ts,
		"suggested":      suggested,
		"candidates":     candidates,
		"frames":         len(files),
		"ocr_summary":    strings.Join(summaries, " · "),
	})
}
