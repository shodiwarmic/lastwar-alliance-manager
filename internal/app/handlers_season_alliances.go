// handlers_season_alliances.go — a season's factions and its alliance rows (private-docs 271).
//
// season_alliances is one row per (season, registry alliance): which faction the alliance is
// in that season, the relationship, the season's terms, and (from the War tab) the matchup
// fields. The Ally modal writes an ally's row and the War tab an opponent's — the same table,
// so nothing is entered twice, and a row never crosses into another season.
//
// Identity is the registry row found by tag AND server (upsertExternalAllianceIdentityTx);
// tags repeat across servers. Our own alliance has no registry row (Rule 2), so no season row.

package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// SeasonFaction is one of a season's factions.
type SeasonFaction struct {
	ID       int    `json:"id"`
	SeasonID int    `json:"season_id"`
	Key      string `json:"key"`
	Label    string `json:"label"`
	IsOurs   bool   `json:"is_ours"`
	Position int    `json:"position"`
}

// SeasonAlliance is one alliance's row for a season, with its registry identity and, when the
// alliance is one of our allies, that ally.
type SeasonAlliance struct {
	ID                 int     `json:"id"`
	SeasonID           int     `json:"season_id"`
	ExternalAllianceID int     `json:"external_alliance_id"`
	Tag                string  `json:"tag"`
	Name               string  `json:"name"`
	Server             int     `json:"server"`
	FactionID          *int    `json:"faction_id"`
	FactionKey         string  `json:"faction_key"`
	FactionLabel       string  `json:"faction_label"`
	FactionIsOurs      bool    `json:"faction_is_ours"`
	Relationship       string  `json:"relationship"`
	Terms              string  `json:"terms"`
	Notes              string  `json:"notes"`
	GroupRank          *int    `json:"group_rank"`
	AssessmentOverride *string `json:"assessment_override"`
	AssessmentNote     string  `json:"assessment_note"`
	AllyID             *int    `json:"ally_id"`
	AllyName           string  `json:"ally_name"`
	AllyActive         bool    `json:"ally_active"`
}

var validSeasonRelationships = map[string]bool{"allied": true, "enemy": true, "neutral": true, "unknown": true}

// validAssessmentOverrides are the matchup ratings an officer may set beside the calculated one.
var validAssessmentOverrides = map[string]bool{
	"very_favorable": true, "favorable": true, "even": true, "unfavorable": true, "very_unfavorable": true,
}

func loadSeasonFactions(q rowQueryer, seasonID int) ([]SeasonFaction, error) {
	rows, err := q.Query(`SELECT id, season_id, key, label, is_ours, position FROM season_factions
		WHERE season_id = ? ORDER BY position, id`, seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeasonFaction{}
	for rows.Next() {
		var f SeasonFaction
		var ours int
		if err := rows.Scan(&f.ID, &f.SeasonID, &f.Key, &f.Label, &ours, &f.Position); err != nil {
			return nil, err
		}
		f.IsOurs = ours == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// seasonAllianceSelect is the reader's SELECT; the ally is the active one sharing the registry
// row, or the tag and server (an ally's own link may predate the server-strict lookup).
const seasonAllianceSelect = `
	SELECT sa.id, sa.season_id, sa.external_alliance_id, COALESCE(ea.tag, ''), COALESCE(ea.name, ''),
	       COALESCE(ea.server, 0), sa.faction_id, COALESCE(f.key, ''), COALESCE(f.label, ''),
	       COALESCE(f.is_ours, 0), sa.relationship, sa.terms, sa.notes, sa.group_rank,
	       sa.assessment_override, sa.assessment_note,
	       (SELECT a.id FROM allies a WHERE a.external_alliance_id = ea.id
	           OR (a.tag = ea.tag COLLATE NOCASE AND CAST(a.server AS INTEGER) = ea.server)
	        ORDER BY a.active DESC, a.id LIMIT 1)
	FROM season_alliances sa
	LEFT JOIN external_alliances ea ON ea.id = sa.external_alliance_id
	LEFT JOIN season_factions f ON f.id = sa.faction_id`

func scanSeasonAlliances(rows *sql.Rows) ([]SeasonAlliance, error) {
	defer rows.Close()
	out := []SeasonAlliance{}
	for rows.Next() {
		var a SeasonAlliance
		var fid, rank, ally sql.NullInt64
		var override sql.NullString
		var ours int
		if err := rows.Scan(&a.ID, &a.SeasonID, &a.ExternalAllianceID, &a.Tag, &a.Name, &a.Server,
			&fid, &a.FactionKey, &a.FactionLabel, &ours, &a.Relationship, &a.Terms, &a.Notes, &rank,
			&override, &a.AssessmentNote, &ally); err != nil {
			return nil, err
		}
		a.FactionIsOurs = ours == 1
		if fid.Valid {
			v := int(fid.Int64)
			a.FactionID = &v
		}
		if rank.Valid {
			v := int(rank.Int64)
			a.GroupRank = &v
		}
		if override.Valid {
			v := override.String
			a.AssessmentOverride = &v
		}
		if ally.Valid {
			v := int(ally.Int64)
			a.AllyID = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// loadSeasonAlliances returns a season's alliance rows joined to the registry and to allies.
// The ally names are read after the cursor closes — one connection.
func loadSeasonAlliances(q rowQueryer, seasonID int) ([]SeasonAlliance, error) {
	rows, err := q.Query(seasonAllianceSelect+` WHERE sa.season_id = ? ORDER BY COALESCE(f.position, 999), ea.tag`, seasonID)
	if err != nil {
		return nil, err
	}
	out, err := scanSeasonAlliances(rows)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].AllyID != nil {
			var active int
			q.QueryRow(`SELECT name, active FROM allies WHERE id = ?`, *out[i].AllyID).Scan(&out[i].AllyName, &active)
			out[i].AllyActive = active == 1
		}
	}
	return out, nil
}

// ── Factions ───────────────────────────────────────────────────────────────

// GET /api/season-hub/factions?season_id=
func handleSeasonFactionsList(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if sid == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	out, err := loadSeasonFactions(db, sid)
	if err != nil {
		slog.Error("handleSeasonFactionsList", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"factions": out})
}

// POST /api/season-hub/factions {season_id, label, is_ours}
func handleSeasonFactionCreate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		SeasonID int    `json:"season_id"`
		Label    string `json:"label"`
		IsOurs   bool   `json:"is_ours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	if body.SeasonID == 0 || body.Label == "" {
		http.Error(w, "season_id and label are required", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	stem := templateKeySlug(body.Label)
	if stem == "" {
		stem = "faction"
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonFactionCreate: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	key := stem
	for n := 2; ; n++ {
		var exists int
		tx.QueryRow(`SELECT COUNT(*) FROM season_factions WHERE season_id = ? AND key = ?`, body.SeasonID, key).Scan(&exists)
		if exists == 0 {
			break
		}
		key = fmt.Sprintf("%s_%d", stem, n)
	}
	if body.IsOurs {
		if _, err := tx.Exec(`UPDATE season_factions SET is_ours = 0 WHERE season_id = ?`, body.SeasonID); err != nil {
			slog.Error("handleSeasonFactionCreate: clear ours", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	res, err := tx.Exec(`INSERT INTO season_factions (season_id, key, label, is_ours, position)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM season_factions WHERE season_id = ?))`,
		body.SeasonID, key, body.Label, boolToInt(body.IsOurs), body.SeasonID)
	if err != nil {
		slog.Error("handleSeasonFactionCreate: insert", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonFactionCreate: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	logActivity(user.ID, user.Username, "created", "season_faction", body.Label, false)
	writeJSON(w, map[string]any{"id": id, "key": key})
}

// PUT /api/season-hub/factions/{id} {label, is_ours, position}
func handleSeasonFactionUpdate(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid faction ID", http.StatusBadRequest)
		return
	}
	var body struct {
		Label    string `json:"label"`
		IsOurs   bool   `json:"is_ours"`
		Position *int   `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	body.Label = strings.TrimSpace(body.Label)
	if body.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	var seasonID, position int
	var oldLabel string
	var oldOurs int
	if err := db.QueryRow(`SELECT season_id, label, is_ours, position FROM season_factions WHERE id = ?`, id).
		Scan(&seasonID, &oldLabel, &oldOurs, &position); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonFactionUpdate: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if body.Position != nil {
		position = *body.Position
	}
	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonFactionUpdate: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if body.IsOurs {
		if _, err := tx.Exec(`UPDATE season_factions SET is_ours = 0 WHERE season_id = ? AND id != ?`, seasonID, id); err != nil {
			slog.Error("handleSeasonFactionUpdate: clear ours", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	}
	if _, err := tx.Exec(`UPDATE season_factions SET label = ?, is_ours = ?, position = ? WHERE id = ?`,
		body.Label, boolToInt(body.IsOurs), position, id); err != nil {
		slog.Error("handleSeasonFactionUpdate: update", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonFactionUpdate: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var changes []string
	if oldLabel != body.Label {
		changes = append(changes, "label: "+oldLabel+" → "+body.Label)
	}
	if (oldOurs == 1) != body.IsOurs {
		changes = append(changes, fmt.Sprintf("ours: %v → %v", oldOurs == 1, body.IsOurs))
	}
	logActivity(user.ID, user.Username, "updated", "season_faction", body.Label, false, strings.Join(changes, "; "))
	writeJSON(w, map[string]string{"message": "Updated"})
}

// DELETE /api/season-hub/factions/{id} — refused while an alliance row names the faction.
func handleSeasonFactionDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid faction ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var label string
	if err := db.QueryRow(`SELECT season_id, label FROM season_factions WHERE id = ?`, id).Scan(&seasonID, &label); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonFactionDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	var used int
	db.QueryRow(`SELECT COUNT(*) FROM season_alliances WHERE faction_id = ?`, id).Scan(&used)
	if used > 0 {
		http.Error(w, fmt.Sprintf("%d alliance(s) are in this faction — move them first", used), http.StatusConflict)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_factions WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonFactionDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_faction", label, false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// ── Season alliance rows ───────────────────────────────────────────────────

// GET /api/season-hub/alliances?season_id=
func handleSeasonAlliancesList(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.Atoi(r.URL.Query().Get("season_id"))
	if sid == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	factions, err := loadSeasonFactions(db, sid)
	if err != nil {
		slog.Error("handleSeasonAlliancesList: factions", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	rows, err := loadSeasonAlliances(db, sid)
	if err != nil {
		slog.Error("handleSeasonAlliancesList: rows", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"factions": factions, "alliances": rows})
}

// GET /api/season-hub/ally-seasons?ally_id= — the Ally modal's Seasons section: every
// unarchived season with its factions and this ally's row, if any. Read-only: an ally with no
// registry row yet gets one on its first save.
func handleAllySeasons(w http.ResponseWriter, r *http.Request) {
	allyID, _ := strconv.Atoi(r.URL.Query().Get("ally_id"))
	if allyID == 0 {
		http.Error(w, "ally_id is required", http.StatusBadRequest)
		return
	}
	var tag, serverStr string
	if err := db.QueryRow(`SELECT tag, server FROM allies WHERE id = ?`, allyID).Scan(&tag, &serverStr); err == sql.ErrNoRows {
		http.Error(w, "Ally not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleAllySeasons: ally", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if isOwnAlliance("", tag) {
		writeJSON(w, map[string]any{"own": true, "seasons": []any{}})
		return
	}
	server, _ := parseServerNumber(serverStr)
	var extID int
	if server > 0 {
		db.QueryRow(`SELECT id FROM external_alliances WHERE tag = ? COLLATE NOCASE AND server = ?
			ORDER BY updated_at DESC LIMIT 1`, tag, server).Scan(&extID)
	}

	type seasonRef struct {
		ID     int    `json:"season_id"`
		Name   string `json:"name"`
		Number int    `json:"season_number"`
	}
	srows, err := db.Query(`SELECT id, name, season_number FROM seasons WHERE archived_at IS NULL ORDER BY start_date DESC`)
	if err != nil {
		slog.Error("handleAllySeasons: seasons", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	var refs []seasonRef
	for srows.Next() {
		var s seasonRef
		srows.Scan(&s.ID, &s.Name, &s.Number)
		refs = append(refs, s)
	}
	srows.Close()

	type allySeason struct {
		seasonRef
		Factions []SeasonFaction `json:"factions"`
		Row      *SeasonAlliance `json:"row"`
	}
	out := []allySeason{}
	for _, s := range refs {
		f, err := loadSeasonFactions(db, s.ID)
		if err != nil {
			slog.Error("handleAllySeasons: factions", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		as := allySeason{seasonRef: s, Factions: f}
		if extID != 0 {
			rows, err := db.Query(seasonAllianceSelect+` WHERE sa.season_id = ? AND sa.external_alliance_id = ?`, s.ID, extID)
			if err == nil {
				if found, err := scanSeasonAlliances(rows); err == nil && len(found) == 1 {
					as.Row = &found[0]
				}
			}
		}
		out = append(out, as)
	}
	writeJSON(w, map[string]any{"own": false, "server_known": server > 0, "seasons": out})
}

// seasonAllianceWrite is the PUT body. The alliance is named by external_alliance_id, by an
// ally, or by tag AND server (a bare tag is ambiguous across servers and refused). Absent fields
// keep the stored value.
type seasonAllianceWrite struct {
	SeasonID           int         `json:"season_id"`
	ExternalAllianceID int         `json:"external_alliance_id"`
	AllyID             int         `json:"ally_id"`
	Tag                string      `json:"tag"`
	Server             int         `json:"server"`
	Name               string      `json:"name"`
	FactionID          optionalInt `json:"faction_id"`
	Relationship       *string     `json:"relationship"`
	Terms              *string     `json:"terms"`
	Notes              *string     `json:"notes"`
	GroupRank          optionalInt `json:"group_rank"`
	AssessmentOverride *string     `json:"assessment_override"`
	AssessmentNote     *string     `json:"assessment_note"`
}

// PUT /api/season-hub/alliances — create or update one alliance's row for a season. The Ally
// modal (manage_allies) writes the faction, relationship, terms and notes; the matchup fields
// (group rank, the assessment override and its note) need manage_season_war.
func handleSeasonAlliancePut(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body seasonAllianceWrite
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SeasonID == 0 {
		http.Error(w, "season_id is required", http.StatusBadRequest)
		return
	}
	canWar := userHasPermission(user, "manage_season_war")
	if !canWar && (body.GroupRank.Set || body.AssessmentOverride != nil || body.AssessmentNote != nil) {
		http.Error(w, "Forbidden: the matchup fields need the War tab's manage permission", http.StatusForbidden)
		return
	}
	if body.Relationship != nil && !validSeasonRelationships[*body.Relationship] {
		http.Error(w, "relationship must be allied, enemy, neutral or unknown", http.StatusBadRequest)
		return
	}
	if body.AssessmentOverride != nil && *body.AssessmentOverride != "" && !validAssessmentOverrides[*body.AssessmentOverride] {
		http.Error(w, "assessment_override is not a recognised rating", http.StatusBadRequest)
		return
	}
	if ok, msg := seasonAcceptsEdits(body.SeasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if body.FactionID.Set && body.FactionID.Value != nil {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM season_factions WHERE id = ? AND season_id = ?`, *body.FactionID.Value, body.SeasonID).Scan(&n)
		if n == 0 {
			http.Error(w, "faction_id is not one of this season's factions", http.StatusBadRequest)
			return
		}
	}

	// Resolve the identity to look up (all reads before the transaction).
	tag, name, server := strings.TrimSpace(body.Tag), strings.TrimSpace(body.Name), body.Server
	if body.ExternalAllianceID == 0 && body.AllyID != 0 {
		var serverStr string
		if err := db.QueryRow(`SELECT tag, name, server FROM allies WHERE id = ?`, body.AllyID).Scan(&tag, &name, &serverStr); err != nil {
			http.Error(w, "Ally not found", http.StatusNotFound)
			return
		}
		server, _ = parseServerNumber(serverStr)
	}
	if body.ExternalAllianceID == 0 {
		if tag == "" || server <= 0 {
			http.Error(w, "an alliance is named by its registry id, an ally, or a tag together with its server", http.StatusBadRequest)
			return
		}
		if isOwnAlliance("", tag) {
			http.Error(w, "Our own alliance has no season row", http.StatusBadRequest)
			return
		}
	} else {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM external_alliances WHERE id = ?`, body.ExternalAllianceID).Scan(&n)
		if n == 0 {
			http.Error(w, "external_alliance_id not found", http.StatusNotFound)
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("handleSeasonAlliancePut: begin", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	extID := int64(body.ExternalAllianceID)
	if extID == 0 {
		id, err := upsertExternalAllianceIdentityTx(tx, tag, name, server)
		if err != nil {
			slog.Error("handleSeasonAlliancePut: identity", "error", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
		if !id.Valid {
			http.Error(w, "Our own alliance has no season row", http.StatusBadRequest)
			return
		}
		extID = id.Int64
	}
	id, created, err := writeSeasonAllianceTx(tx, body.SeasonID, extID, body)
	if err != nil {
		slog.Error("handleSeasonAlliancePut: write", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("handleSeasonAlliancePut: commit", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	label := seasonAllianceLabel(extID)
	action := "updated"
	if created {
		action = "created"
	}
	logActivity(user.ID, user.Username, action, "season_alliance", label, false)
	writeJSON(w, map[string]any{"id": id, "external_alliance_id": extID})
}

// writeSeasonAllianceTx upserts one row, keeping every field the body leaves out.
func writeSeasonAllianceTx(tx *sql.Tx, seasonID int, extID int64, b seasonAllianceWrite) (int64, bool, error) {
	var (
		id                      int64
		faction, rank           sql.NullInt64
		rel, terms, notes, note string
		override                sql.NullString
		created                 bool
	)
	err := tx.QueryRow(`SELECT id, faction_id, relationship, terms, notes, group_rank, assessment_override, assessment_note
		FROM season_alliances WHERE season_id = ? AND external_alliance_id = ?`, seasonID, extID).
		Scan(&id, &faction, &rel, &terms, &notes, &rank, &override, &note)
	if err == sql.ErrNoRows {
		created, rel = true, "unknown"
	} else if err != nil {
		return 0, false, err
	}
	if b.FactionID.Set {
		faction = sql.NullInt64{}
		if b.FactionID.Value != nil {
			faction = sql.NullInt64{Int64: int64(*b.FactionID.Value), Valid: true}
		}
	}
	if b.Relationship != nil {
		rel = *b.Relationship
	}
	if b.Terms != nil {
		terms = strings.TrimSpace(*b.Terms)
	}
	if b.Notes != nil {
		notes = strings.TrimSpace(*b.Notes)
	}
	if b.GroupRank.Set {
		rank = sql.NullInt64{}
		if b.GroupRank.Value != nil {
			rank = sql.NullInt64{Int64: int64(*b.GroupRank.Value), Valid: true}
		}
	}
	if b.AssessmentOverride != nil {
		override = sql.NullString{String: *b.AssessmentOverride, Valid: *b.AssessmentOverride != ""}
	}
	if b.AssessmentNote != nil {
		note = strings.TrimSpace(*b.AssessmentNote)
	}
	if created {
		res, err := tx.Exec(`INSERT INTO season_alliances (season_id, external_alliance_id, faction_id, relationship,
			terms, notes, group_rank, assessment_override, assessment_note) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seasonID, extID, faction, rel, terms, notes, rank, override, note)
		if err != nil {
			return 0, false, err
		}
		id, _ = res.LastInsertId()
		return id, true, nil
	}
	_, err = tx.Exec(`UPDATE season_alliances SET faction_id = ?, relationship = ?, terms = ?, notes = ?, group_rank = ?,
		assessment_override = ?, assessment_note = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		faction, rel, terms, notes, rank, override, note, id)
	return id, false, err
}

// seasonAllianceLabel is "[TAG] Name (server)" for the activity log.
func seasonAllianceLabel(extID int64) string {
	var tag, name string
	var server int
	db.QueryRow(`SELECT COALESCE(tag,''), COALESCE(name,''), COALESCE(server,0) FROM external_alliances WHERE id = ?`, extID).
		Scan(&tag, &name, &server)
	return fmt.Sprintf("[%s] %s (%d)", tag, name, server)
}

// DELETE /api/season-hub/alliances/{id} — removes one alliance's row for a season.
func handleSeasonAllianceDelete(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}
	var seasonID int
	var extID int64
	if err := db.QueryRow(`SELECT season_id, external_alliance_id FROM season_alliances WHERE id = ?`, id).Scan(&seasonID, &extID); err == sql.ErrNoRows {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	} else if err != nil {
		slog.Error("handleSeasonAllianceDelete: load", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if ok, msg := seasonAcceptsEdits(seasonID); !ok {
		seasonEditRefusal(w, msg)
		return
	}
	if msg := seasonAllianceInUse(id); msg != "" {
		http.Error(w, msg, http.StatusConflict)
		return
	}
	if _, err := db.Exec(`DELETE FROM season_alliances WHERE id = ?`, id); err != nil {
		slog.Error("handleSeasonAllianceDelete: delete", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "deleted", "season_alliance", seasonAllianceLabel(extID), false)
	writeJSON(w, map[string]string{"message": "Deleted"})
}

// seasonAllianceInUse names what still points at a season alliance row, or "". Every table
// that references season_alliances adds its check here (foreign_keys is off).
func seasonAllianceInUse(id int) string {
	for _, check := range []func(int) string{warSeasonAllianceInUse} {
		if msg := check(id); msg != "" {
			return msg
		}
	}
	return ""
}
