// season_import.go — the Season Hub's CSV imports (decision 27): one preview/confirm pair for
// every kind of observation, so each import maps its headers before any row, says what it will
// write row by row, and writes exactly what the preview showed (the confirm re-parses the same
// text with the same rules and refuses if any row has an error).
//
// A kind declares its columns (with header aliases), who may import it, how to parse a row and
// how to write the parsed rows. Member columns resolve through resolveMemberAliasWithIndex,
// alliance columns through tag AND server.

package app

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type importColumn struct {
	Key      string
	Label    string
	Aliases  []string
	Required bool
}

// importCtx is what a kind's parser and writer may read: the season, the caller, and anything
// the kind loaded before the rows (one connection: nothing is read inside a row loop that a
// transaction holds).
type importCtx struct {
	Season *Season
	User   *AuthUser
	Data   map[string]any
}

type importKind struct {
	Title   string
	Perms   []string // any-of
	Columns []importColumn
	// Prepare loads lookups once, before the rows (optional).
	Prepare func(ctx *importCtx) error
	// Parse turns one row into an item and a summary for the preview, or errors.
	Parse func(ctx *importCtx, rec map[string]string) (item any, summary map[string]any, errs []string)
	// Write stores every item in the caller's transaction; returns written and unchanged counts.
	Write func(tx *sql.Tx, ctx *importCtx, items []any) (written, unchanged int, err error)
}

// seasonImportKinds is the registry; later issues add their kinds here.
var seasonImportKinds = map[string]importKind{}

func registerSeasonImportKind(key string, k importKind) { seasonImportKinds[key] = k }

func normHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
	h = strings.NewReplacer(" ", "_", "-", "_", "/", "_", ".", "").Replace(h)
	return h
}

type importRow struct {
	Line    int            `json:"line"`
	Summary map[string]any `json:"summary"`
	Errors  []string       `json:"errors"`
	item    any
}

// parseSeasonImport reads the CSV text: header check first, then every row.
func parseSeasonImport(k importKind, ctx *importCtx, text string) ([]importRow, string) {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, "The file is not readable CSV: " + err.Error()
	}
	if len(records) < 1 {
		return nil, "The file is empty"
	}
	colIdx := map[string]int{}
	for i, h := range records[0] {
		nh := normHeader(h)
		for _, c := range k.Columns {
			if nh == c.Key {
				colIdx[c.Key] = i
			}
			for _, a := range c.Aliases {
				if nh == a {
					if _, ok := colIdx[c.Key]; !ok {
						colIdx[c.Key] = i
					}
				}
			}
		}
	}
	var missing []string
	for _, c := range k.Columns {
		if _, ok := colIdx[c.Key]; c.Required && !ok {
			missing = append(missing, c.Label)
		}
	}
	if len(missing) > 0 {
		return nil, "The file is missing required column(s): " + strings.Join(missing, ", ")
	}
	var rows []importRow
	for li, rec := range records[1:] {
		blank := true
		for _, v := range rec {
			if strings.TrimSpace(v) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		vals := map[string]string{}
		for key, i := range colIdx {
			if i < len(rec) {
				vals[key] = strings.TrimSpace(rec[i])
			}
		}
		item, summary, errs := k.Parse(ctx, vals)
		if errs == nil {
			errs = []string{}
		}
		rows = append(rows, importRow{Line: li + 2, Summary: summary, Errors: errs, item: item})
	}
	if len(rows) == 0 {
		return nil, "The file has a header but no rows"
	}
	return rows, ""
}

type seasonImportBody struct {
	SeasonID int    `json:"season_id"`
	Kind     string `json:"kind"`
	CSV      string `json:"csv"`
}

// seasonImportSetup decodes and checks a preview or confirm request; nil means it answered.
func seasonImportSetup(w http.ResponseWriter, r *http.Request) (*importKind, *importCtx, *seasonImportBody) {
	user := getAuthUser(r)
	var body seasonImportBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return nil, nil, nil
	}
	k, ok := seasonImportKinds[body.Kind]
	if !ok {
		http.Error(w, "Unknown import kind", http.StatusBadRequest)
		return nil, nil, nil
	}
	if !anyPermission(user, k.Perms) {
		http.Error(w, "Forbidden: you do not have permission to import "+k.Title, http.StatusForbidden)
		return nil, nil, nil
	}
	s, err := loadSeasonByID(body.SeasonID)
	if err == sql.ErrNoRows {
		http.Error(w, "Season not found", http.StatusNotFound)
		return nil, nil, nil
	}
	if err != nil {
		slog.Error("seasonImportSetup: season", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return nil, nil, nil
	}
	if s.ArchivedAt != "" {
		http.Error(w, "Season is archived and cannot be modified", http.StatusConflict)
		return nil, nil, nil
	}
	ctx := &importCtx{Season: s, User: user, Data: map[string]any{}}
	if k.Prepare != nil {
		if err := k.Prepare(ctx); err != nil {
			slog.Error("seasonImportSetup: prepare", "kind", body.Kind, "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return nil, nil, nil
		}
	}
	return &k, ctx, &body
}

func importColumnsOut(k *importKind) []map[string]any {
	out := []map[string]any{}
	for _, c := range k.Columns {
		out = append(out, map[string]any{"key": c.Key, "label": c.Label, "required": c.Required})
	}
	return out
}

// POST /api/season-hub/import/preview {season_id, kind, csv}
func handleSeasonImportPreview(w http.ResponseWriter, r *http.Request) {
	k, ctx, body := seasonImportSetup(w, r)
	if k == nil {
		return
	}
	rows, msg := parseSeasonImport(*k, ctx, body.CSV)
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	invalid := 0
	for _, row := range rows {
		if len(row.Errors) > 0 {
			invalid++
		}
	}
	writeJSON(w, map[string]any{"kind": body.Kind, "title": k.Title, "columns": importColumnsOut(k),
		"rows": rows, "valid": len(rows) - invalid, "invalid": invalid})
}

// POST /api/season-hub/import/confirm {season_id, kind, csv} — re-parses the same text and
// writes it in one transaction, or refuses while any row has an error.
func handleSeasonImportConfirm(w http.ResponseWriter, r *http.Request) {
	k, ctx, body := seasonImportSetup(w, r)
	if k == nil {
		return
	}
	rows, msg := parseSeasonImport(*k, ctx, body.CSV)
	if msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	var items []any
	for _, row := range rows {
		if len(row.Errors) > 0 {
			http.Error(w, fmt.Sprintf("Line %d: %s — fix the file and preview again", row.Line, strings.Join(row.Errors, "; ")), http.StatusBadRequest)
			return
		}
		items = append(items, row.item)
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonImportConfirm: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	written, unchanged, err := k.Write(tx, ctx, items)
	if err != nil {
		slog.Error("handleSeasonImportConfirm: write", "kind", body.Kind, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonImportConfirm: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(ctx.User.ID, ctx.User.Username, "imported", "season_import", k.Title+" — "+ctx.Season.Name, false,
		fmt.Sprintf("%s: %d written, %d unchanged · via CSV", body.Kind, written, unchanged))
	writeJSON(w, map[string]any{"written": written, "unchanged": unchanged})
}

// ── Shared parsers ─────────────────────────────────────────────────────────

func optIntCell(v string) (*int, error) {
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(strings.ReplaceAll(v, ",", ""))
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
