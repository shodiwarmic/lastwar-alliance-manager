package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The participation board import from post-event mail screenshots
// (private-docs 171): merge across frames, flags, checks, the occurrence
// suggestion, the capability refusal, chunking, and how the board PUT stores
// an imported board.

func rk(n int) *int { return &n }

// --- merge ------------------------------------------------------------------------

func TestMergeImportedRows(t *testing.T) {
	t.Run("overlapping frames collapse to one row each", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Alpha", Score: 30, Rank: rk(1)}, {PlayerName: "[PoWr]Bravo", Score: 20, Rank: rk(2)},
			{PlayerName: "Bravo", Score: 20, Rank: rk(2)}, {PlayerName: "Charlie", Score: 10, Rank: rk(3)},
		}, "damage")
		if len(rows) != 3 || rows[1].Name != "Bravo" || *rows[2].Rank != 3 {
			t.Fatalf("rows = %+v", rows)
		}
		for _, r := range rows {
			if len(r.Flags) != 0 {
				t.Errorf("%s flagged %v", r.Name, r.Flags)
			}
		}
	})
	t.Run("same rank, different names: both kept, both flagged", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Bang", Score: 35, Rank: rk(1)}, {PlayerName: "Ally canceled the Rally.", Score: 35, Rank: rk(1)},
		}, "damage")
		if len(rows) != 2 || !hasFlag(rows[0].Flags, "rank_conflict") || !hasFlag(rows[1].Flags, "rank_conflict") {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("same name at two ranks: the one read more often, flagged", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Alpha", Score: 5, Rank: rk(13)}, {PlayerName: "Alpha", Score: 5, Rank: rk(14)},
			{PlayerName: "Alpha", Score: 5, Rank: rk(14)},
		}, "points")
		if len(rows) != 1 || *rows[0].Rank != 14 || !hasFlag(rows[0].Flags, "rank_disagreement") {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("a read rank beats an inferred one", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Alpha", Score: 5, Rank: rk(4), RankInferred: true},
			{PlayerName: "Alpha", Score: 5, Rank: rk(4), RankInferred: true},
			{PlayerName: "Alpha", Score: 5, Rank: rk(5)},
		}, "points")
		if *rows[0].Rank != 5 || rows[0].RankInferred {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("inferred rank kept and marked; unread placed after its predecessor", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "A", Score: 9, Rank: rk(1)}, {PlayerName: "B", Score: 8, Rank: rk(2), RankInferred: true},
			{PlayerName: "C", Score: 7}, {PlayerName: "D", Score: 6, Rank: rk(4)},
		}, "points")
		got := []string{}
		for _, r := range rows {
			got = append(got, r.Name)
		}
		if strings.Join(got, ",") != "A,B,C,D" || !rows[1].RankInferred || rows[2].Rank != nil || !hasFlag(rows[2].Flags, "rank_unread") {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("zero waves kept; an unread score is null and flagged", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "A", Score: 0, Rank: rk(98)}, {PlayerName: "B", Score: 0, Rank: rk(99), ScoreUnread: true},
		}, "waves")
		if rows[0].Values["waves"] == nil || *rows[0].Values["waves"] != 0 || len(rows[0].Flags) != 0 {
			t.Errorf("A = %+v", rows[0])
		}
		if rows[1].Values["waves"] != nil || !hasFlag(rows[1].Flags, "score_unread") {
			t.Errorf("B = %+v", rows[1])
		}
	})
	t.Run("one row read with different marks on different frames is one row", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Thorbooster 뀨우", Score: 4854918, Rank: rk(1)}, {PlayerName: "Thorbooster #¦", Score: 4854918, Rank: rk(1)},
			{PlayerName: "Thorbooster 뀨우", Score: 4854918, Rank: rk(1)},
			{PlayerName: "Tsquall 7|| 72|", Score: 1764804, Rank: rk(14)}, {PlayerName: "Tsquall 7|| 72||", Score: 1764804, Rank: rk(15)},
			{PlayerName: "Lord Mistle", Score: 1641421, Rank: rk(15)},
		}, "points")
		if rows[0].Name != "Thorbooster 뀨우" || !hasFlag(rows[0].Flags, "name_variants") ||
			len(rows[0].Variants) != 1 || hasFlag(rows[0].Flags, "rank_conflict") {
			t.Fatalf("rows = %+v", rows)
		}
		// A variant read at another rank is not folded: that stays for the officer.
		if len(rows) != 4 || !hasFlag(rows[2].Flags, "rank_conflict") {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("different players with the same score stay apart", func(t *testing.T) {
		// Zombie Siege scores tie constantly; a shared start is not a shared name.
		// "pudgey" is a prefix of "pudgeychucuplun", but they sit at different ranks.
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "Pudgey chucuplun", Score: 20, Rank: rk(61)}, {PlayerName: "Pudgey", Score: 20, Rank: rk(62)},
			{PlayerName: "Stocky X", Score: 20, Rank: rk(63)}, {PlayerName: "Stocky Y", Score: 20, Rank: rk(64)},
		}, "waves")
		if len(rows) != 4 {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("a later frame's read score replaces an unread one", func(t *testing.T) {
		rows := mergeImportedRows([]OCRPlayer{
			{PlayerName: "A", Score: 0, Rank: rk(9), ScoreUnread: true}, {PlayerName: "A", Score: 12, Rank: rk(9)},
		}, "waves")
		if *rows[0].Values["waves"] != 12 || len(rows[0].Flags) != 0 {
			t.Fatalf("rows = %+v", rows)
		}
	})
}

func TestRankRanges(t *testing.T) {
	if got := rankRanges([]int{4, 5, 24, 27, 28, 29}); got != "4–5, 24, 27–29" {
		t.Errorf("got %q", got)
	}
}

func TestImportProblems(t *testing.T) {
	typ := &ptType{Trackables: []ptTrackable{{Key: "points", Label: "Individual Points"}}}
	rows := mergeImportedRows([]OCRPlayer{
		{PlayerName: "A", Score: 50, Rank: rk(1)}, {PlayerName: "B", Score: 60, Rank: rk(2)},
		{PlayerName: "C", Score: 40, Rank: rk(5)}, {PlayerName: "D", Score: 40, Rank: rk(6), ScoreUnread: true},
	}, "points")
	var msgs []string
	for _, p := range importProblems(rows, typ) {
		msgs = append(msgs, p.Message)
	}
	all := strings.Join(msgs, " | ")
	for _, want := range []string{"No frame showed rank 3–4", "B (60) is above A (50)", "D needs a value for Individual Points"} {
		if !strings.Contains(all, want) {
			t.Errorf("problems %q lack %q", all, want)
		}
	}
	if strings.Contains(all, "C (40)") {
		t.Errorf("a tie was reported as out of order: %q", all)
	}
}

// --- the mail's time ---------------------------------------------------------------

func strp(s string) *string { return &s }

func TestMailTimestamp(t *testing.T) {
	same := []OCRSectionDiagnostic{{MailTimestamp: strp("2026-09-26 22:47:11")}, {MailTimestamp: strp("2026-09-26 22:47:11")}, {}}
	if ts, _ := mailTimestamp(same); ts != "2026-09-26 22:47:11" {
		t.Errorf("consensus = %q", ts)
	}
	mixed := []OCRSectionDiagnostic{{MailTimestamp: strp("2026-09-26 22:47:11")}, {MailTimestamp: strp("2026-09-25 21:30:12")}}
	if ts, why := mailTimestamp(mixed); ts != "" || !strings.Contains(why, "different") {
		t.Errorf("conflict = %q, %q", ts, why)
	}
	if ts, why := mailTimestamp(nil); ts != "" || why == "" {
		t.Errorf("none = %q, %q", ts, why)
	}
}

func TestMailInstantUsesTheZoneOnTheMailsDate(t *testing.T) {
	// 2026-09-26 is in EDT (UTC-4) even when the browser's current offset is EST.
	got, err := mailInstant("2026-09-26 22:47:11", "America/New_York", -300)
	if err != nil || !got.Equal(time.Date(2026, 9, 27, 2, 47, 11, 0, time.UTC)) {
		t.Fatalf("got %v, %v", got.UTC(), err)
	}
	got, _ = mailInstant("2026-09-26 22:47:11", "", -240)
	if !got.Equal(time.Date(2026, 9, 27, 2, 47, 11, 0, time.UTC)) {
		t.Fatalf("offset fallback got %v", got.UTC())
	}
}

func TestSuggestOccurrences(t *testing.T) {
	a, b := "A", "B"
	occs := []ptOccurrence{
		{EventID: 1, EventDate: "2026-09-25", EventTime: "14:30", TaskForce: &b},
		{EventID: 2, EventDate: "2026-09-25", EventTime: "19:30", TaskForce: &a},
		{EventID: 3, EventDate: "2026-09-21", EventTime: "19:30", TaskForce: &a}, // four days back
	}
	// The A mail: 21:30:12 phone time, EDT → 01:30 UTC on the 26th → 23:30 game time on the 25th.
	mail := time.Date(2026, 9, 26, 1, 30, 12, 0, time.UTC)
	if got := suggestOccurrences(mail, occs); len(got) != 1 || got[0].EventID != 2 {
		t.Errorf("A mail → %+v, want event 2", got)
	}
	// The B mail at 16:30:06 phone time → 18:30 game time: after B's 14:30, before A's 19:30.
	mail = time.Date(2026, 9, 25, 20, 30, 6, 0, time.UTC)
	if got := suggestOccurrences(mail, occs); len(got) != 1 || got[0].EventID != 1 {
		t.Errorf("B mail → %+v, want event 1", got)
	}
	t.Run("an all-day occurrence is its whole game day", func(t *testing.T) {
		day := []ptOccurrence{{EventID: 7, EventDate: "2026-09-23", AllDay: true}}
		// 01:59 UTC on the 24th is 23:59 game time on the 23rd: inside the day.
		if got := suggestOccurrences(time.Date(2026, 9, 24, 1, 59, 0, 0, time.UTC), day); len(got) != 1 {
			t.Errorf("inside the game day: %+v", got)
		}
		// 01:59 UTC on the 23rd is still the 22nd in game time: before it started.
		if got := suggestOccurrences(time.Date(2026, 9, 23, 1, 59, 0, 0, time.UTC), day); len(got) != 0 {
			t.Errorf("before game midnight: %+v", got)
		}
	})
	t.Run("nothing within three days", func(t *testing.T) {
		if got := suggestOccurrences(time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), occs); len(got) != 0 {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("ties are all returned", func(t *testing.T) {
		tied := []ptOccurrence{{EventID: 8, EventDate: "2026-09-25", AllDay: true, TaskForce: &a},
			{EventID: 9, EventDate: "2026-09-25", AllDay: true, TaskForce: &b}}
		if got := suggestOccurrences(time.Date(2026, 9, 25, 23, 0, 0, 0, time.UTC), tied); len(got) != 2 {
			t.Errorf("got %+v", got)
		}
	})
}

func TestChunkFrames(t *testing.T) {
	sizes := []int64{8, 8, 8, 30, 1}
	var files []*multipart.FileHeader
	for i, s := range sizes {
		files = append(files, &multipart.FileHeader{Filename: fmt.Sprint(i), Size: s})
	}
	var got []string
	for _, c := range chunkFrames(files, 20) {
		var names []string
		for _, f := range c {
			names = append(names, f.Filename)
		}
		got = append(got, strings.Join(names, ""))
	}
	if strings.Join(got, "|") != "01|2|3|4" {
		t.Fatalf("chunks = %v (order kept, at most 20, an oversized frame alone)", got)
	}
}

// --- the endpoint ------------------------------------------------------------------

// pointOCRAt points the current test database's OCR settings at srv.
func pointOCRAt(t *testing.T, url string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE settings SET ocr_backend_mode = 'cloud', cv_worker_url = ?, ocr_archive_mode = 'none' WHERE id = 1`, url); err != nil {
		t.Fatal(err)
	}
	prev := newCloudOCRClient
	newCloudOCRClient = func(ctx context.Context, workerURL string) (*http.Client, func(), error) {
		return &http.Client{Timeout: 10 * time.Second}, func() {}, nil
	}
	invalidateOCRServiceInfo()
	t.Cleanup(func() { newCloudOCRClient = prev; invalidateOCRServiceInfo() })
}

// mailService answers /health as a v1.0.0 service that reads `categories`, and
// /process-batch with one response per call from `replies` (the last repeats).
func mailService(t *testing.T, categories string, replies ...string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			fmt.Fprintf(w, `{"status":"ok","version":"v1.0.0","commit":"c","schema_versions":[1],"categories":[%s]}`, categories)
			return
		}
		io.Copy(io.Discard, r.Body)
		i := calls
		if i >= len(replies) {
			i = len(replies) - 1
		}
		calls++
		io.WriteString(w, replies[i])
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func postImport(t *testing.T, typeID, eventID int, frameSizes ...int) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for i, n := range frameSizes {
		part, _ := w.CreateFormFile("images", fmt.Sprintf("%02d.png", i+1))
		part.Write(bytes.Repeat([]byte{'x'}, n))
	}
	w.WriteField("event_type_id", fmt.Sprint(typeID))
	if eventID > 0 {
		w.WriteField("event_id", fmt.Sprint(eventID))
	}
	w.WriteField("tz", "America/New_York")
	w.Close()
	r := ptReq(http.MethodPost, "/api/participation/import", nil, nil, nil)
	r.Body = io.NopCloser(&body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	handleParticipationImport(rr, r)
	var out map[string]any
	json.Unmarshal(rr.Body.Bytes(), &out)
	return rr, out
}

const zsReply = `{"schema_version":1,"results":{"zombie_siege":[
	{"player_name":"Alpha","score":20,"rank":1},
	{"player_name":"Bravo","score":0,"rank":2},
	{"player_name":"Charlie","score":0,"rank":3,"score_unread":true}]},
	"diagnostics":{"sections":[{"image":"01.png","mail_timestamp":"2026-09-23 23:03:00"}]}}`

func TestParticipationImportEndpoint(t *testing.T) {
	t.Run("an old OCR service is refused readably, before any upload", func(t *testing.T) {
		f := setupParticipationTestDB(t)
		srv, calls := mailService(t, `"weekly"`, zsReply)
		pointOCRAt(t, srv.URL)
		rr, _ := postImport(t, f.zs, 0, 10)
		if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "can't read Zombie Siege mails yet") || *calls != 0 {
			t.Fatalf("status %d, %q, %d batch calls", rr.Code, rr.Body.String(), *calls)
		}
	})
	t.Run("an unreachable service is a 503, not a 409", func(t *testing.T) {
		f := setupParticipationTestDB(t)
		pointOCRAt(t, "http://127.0.0.1:1")
		rr, _ := postImport(t, f.zs, 0, 10)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
	})
	t.Run("rows, matches, flags and the suggestion by time", func(t *testing.T) {
		f := setupParticipationTestDB(t)
		ev := seedEvent(t, f.zs, "2026-09-23") // 20:00 game time; the mail is 23:03 EDT = 01:03 game, the 24th
		seedEvent(t, f.zs, "2026-09-18")
		srv, _ := mailService(t, `"zombie_siege"`, zsReply)
		pointOCRAt(t, srv.URL)
		rr, out := postImport(t, f.zs, 0, 10)
		if rr.Code != 200 {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		rows := out["rows"].([]any)
		alpha := rows[0].(map[string]any)
		if alpha["member_id"] != float64(1) || alpha["rank"] != float64(1) {
			t.Errorf("Alpha = %v", alpha)
		}
		bravo := rows[1].(map[string]any)
		if bravo["values"].(map[string]any)["waves"] != float64(0) {
			t.Errorf("a zero-wave row lost its zero: %v", bravo)
		}
		charlie := rows[2].(map[string]any)
		if charlie["values"].(map[string]any)["waves"] != nil || !strings.Contains(fmt.Sprint(charlie["flags"]), "score_unread") {
			t.Errorf("Charlie = %v", charlie)
		}
		sug := out["suggested"].(map[string]any)
		if sug["event_id"] != float64(ev) || out["mail_timestamp"] != "2026-09-23 23:03:00" {
			t.Errorf("suggested %v (timestamp %v), want event %d", sug, out["mail_timestamp"], ev)
		}
	})
	t.Run("an oversized upload goes in chunks", func(t *testing.T) {
		f := setupParticipationTestDB(t)
		srv, calls := mailService(t, `"zombie_siege"`, zsReply)
		pointOCRAt(t, srv.URL)
		rr, _ := postImport(t, f.zs, 0, 12<<20, 12<<20)
		if rr.Code != 200 || *calls != 2 {
			t.Fatalf("status %d, %d calls, want 2 chunks", rr.Code, *calls)
		}
	})
}

func TestParticipationImportTaskForceByMembers(t *testing.T) {
	f := setupParticipationTestDB(t)
	_ = f
	var ds int
	if err := db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'DS'`).Scan(&ds); err != nil {
		t.Fatal(err)
	}
	rows := []ptImportRow{{MemberID: rk(1)}, {MemberID: rk(2)}}
	// No planner lineup at all: the task force is left to the officer.
	if tf, err := taskForceByMembers(db, rows); err != nil || tf != "" {
		t.Fatalf("tf = %q, %v", tf, err)
	}
}

// --- saving an imported board -------------------------------------------------------

func TestParticipationPutStoresTheImport(t *testing.T) {
	f := setupParticipationTestDB(t)
	ev := seedEvent(t, f.zs, "2026-09-01")
	rr := putBoard(t, ev, map[string]any{"source": "import", "entries": []any{
		entry(1, "Alpha", 1, "waves", 20), entry(3, "Bravo", 2, "waves", 0), // rank 2 missing: saved with the gap
	}})
	if rr.Code != 200 {
		t.Fatalf("put: %d %s", rr.Code, rr.Body.String())
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards WHERE source = 'import'`); n != 1 {
		t.Errorf("import boards = %d", n)
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_entries WHERE rank = 3`); n != 1 {
		t.Errorf("the gap was renumbered")
	}
	if rr := putBoard(t, ev, map[string]any{"source": "legacy", "entries": []any{}}); rr.Code != http.StatusBadRequest {
		t.Errorf("a client sent source legacy: %d", rr.Code)
	}
	if rr := putBoard(t, ev, map[string]any{"entries": []any{entry(1, "Alpha", 1, "waves", 20)}}); rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards WHERE source = 'manual'`); n != 1 {
		t.Errorf("replacing did not update the source")
	}
}

func TestParticipationImportReplacesALegacyBoard(t *testing.T) {
	setupParticipationTestDB(t)
	var ds int
	db.QueryRow(`SELECT id FROM schedule_event_types WHERE short_name = 'DS'`).Scan(&ds)
	ev := seedEvent(t, ds, "2026-08-28")
	res, _ := db.Exec(`INSERT INTO participation_boards (schedule_event_id, source) VALUES (?, 'legacy')`, ev)
	board, _ := res.LastInsertId()
	db.Exec(`INSERT INTO participation_entries (board_id, member_id, name_snapshot, rank) VALUES (?, 1, 'Alpha', NULL)`, board)
	db.Exec(`INSERT INTO participation_exceptions (board_id, member_id, kind, reason) VALUES (?, 2, 'missed', ''), (?, 3, 'excused', 'away')`, board, board)

	rr := putBoard(t, ev, map[string]any{"source": "import", "entries": []any{
		entry(1, "Alpha", 1, "points", 900), entry(2, "Bravo", 2, "points", 800),
	}})
	if rr.Code != 200 {
		t.Fatalf("put: %d %s", rr.Code, rr.Body.String())
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_boards WHERE id = ? AND source = 'import'`, board); n != 1 {
		t.Error("source not updated")
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_exceptions WHERE board_id = ? AND kind = 'missed'`, board); n != 0 {
		t.Error("the migrated misses survived the import")
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_exceptions WHERE board_id = ? AND kind = 'excused'`, board); n != 1 {
		t.Error("an excuse was lost")
	}
	if n := count(t, `SELECT COUNT(*) FROM participation_entries WHERE board_id = ? AND rank IN (1, 2)`, board); n != 2 {
		t.Error("ranks not kept")
	}
}

func TestMigration082SeedsOCRCategories(t *testing.T) {
	setupParticipationTestDB(t)
	want := map[string]string{"MG": "alliance_exercise", "LS": "alliance_exercise", "ZS": "zombie_siege", "DS": "desert_storm"}
	for short, cat := range want {
		var got string
		err := db.QueryRow(`SELECT COALESCE(pt.ocr_category, '') FROM participation_types pt
			JOIN schedule_event_types t ON t.id = pt.event_type_id WHERE t.short_name = ?`, short).Scan(&got)
		if err != nil || got != cat {
			t.Errorf("%s: %q, %v (want %q)", short, got, err, cat)
		}
	}
}
