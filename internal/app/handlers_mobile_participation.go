// handlers_mobile_participation.go - the mobile participation store: boards read from
// the post-event mails. Two inputs share one preview core with the web import — rows
// the phone has read (JSON, the OCR wire shape per frame) and frames the server reads —
// so "one reader or two" (private-docs 172) needs no backend release either way. A board
// saved from a phone writes entries and values only: roles, result and notes are officer
// judgement and stay as they are.

package app

import (
	"net/http"
	"strconv"
	"strings"
)

// GET /api/mobile/participation/types (view_ or manage_participation)
func getMobileParticipationTypes(w http.ResponseWriter, r *http.Request) {
	types, err := loadParticipationTypes(db)
	if err != nil {
		dbError(w, "getMobileParticipationTypes", err)
		return
	}
	sorted, err := sortedParticipationTypes(db, types)
	if err != nil {
		dbError(w, "getMobileParticipationTypes order", err)
		return
	}
	out := []map[string]any{}
	for _, t := range sorted {
		tr := []map[string]string{}
		for _, k := range t.Trackables {
			tr = append(tr, map[string]string{"key": k.Key, "label": k.Label})
		}
		out = append(out, map[string]any{"event_type_id": t.EventTypeID, "name": t.Name,
			"absence_rule": t.AbsenceRule, "ocr_category": t.OCRCategory, "trackables": tr})
	}
	writeJSON(w, out)
}

// POST /api/mobile/participation/preview (manage_participation)
// Rows the phone has read, per frame in the OCR wire shape, with the frame's mail
// timestamp; each frame's timestamp becomes a section diagnostic, so the occurrence is
// suggested exactly as it is for the OCR service's own frames.
func mobileParticipationPreview(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	var req struct {
		EventTypeID int  `json:"event_type_id"`
		EventID     *int `json:"event_id"`
		Frames      []struct {
			Players       []OCRPlayer `json:"players"`
			MailTimestamp string      `json:"mail_timestamp"`
		} `json:"frames"`
		TZ       string `json:"tz"`
		TZOffset int    `json:"tz_offset"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	if len(req.Frames) == 0 || len(req.Frames) > maxImportFrames {
		http.Error(w, "Send between 1 and 100 frames", http.StatusBadRequest)
		return
	}
	eventID := 0
	if req.EventID != nil {
		eventID = *req.EventID
	}
	t, status, msg := participationImportType(req.EventTypeID, eventID)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	var players []OCRPlayer
	sections := make([]OCRSectionDiagnostic, 0, len(req.Frames))
	for i, f := range req.Frames {
		players = append(players, f.Players...)
		sec := OCRSectionDiagnostic{Image: "frame " + strconv.Itoa(i+1), PlayersFound: len(f.Players)}
		if ts := strings.TrimSpace(f.MailTimestamp); ts != "" {
			sec.MailTimestamp = &ts
		}
		sections = append(sections, sec)
	}
	out, status, msg := participationImportCore(u, t, req.EventTypeID, eventID, players, sections, strings.TrimSpace(req.TZ), req.TZOffset)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	out["frames"] = len(req.Frames)
	writeJSON(w, out)
}

// POST /api/mobile/participation/import (manage_participation)
// Frames, multipart, with the web import's fields and limits: read by the server as the
// web import reads them, then the same core.
func mobileParticipationImport(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	files, typeID, eventID, tz, offset, ok := readImportForm(w, r)
	if !ok {
		return
	}
	t, status, msg := participationImportType(typeID, eventID)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	players, sections, _, status, msg := readParticipationFrames(r.Context(), t, files)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	out, status, msg := participationImportCore(u, t, typeID, eventID, players, sections, tz, offset)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	out["frames"] = len(files)
	writeJSON(w, out)
}

// POST /api/mobile/participation/occurrences (manage_participation) — the web body.
func mobileParticipationOccurrence(w http.ResponseWriter, r *http.Request) {
	var req scheduleEventCreate
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	id, status, msg := createParticipationOccurrence(getAuthUser(r), req)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	writeJSON(w, map[string]any{"id": id})
}

// PUT /api/mobile/participation/boards/{eventID} (manage_participation)
// {"entries": [{rank, name, member_id, values}]}. Entries and values replace the
// board's; its roles, result, notes and exceptions are kept. Source is "import": the
// recording screen keys its own-rank mode on it.
func mobileParticipationBoardPut(w http.ResponseWriter, r *http.Request) {
	u := getAuthUser(r)
	id, ok := eventIDVar(r)
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var req struct {
		Entries []ptPutEntry `json:"entries"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	body := ptPutBody{Entries: req.Entries, Source: "import"}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "mobileParticipationBoardPut begin", err)
		return
	}
	defer tx.Rollback()
	sv, status, msg, err := saveBoardTx(tx, u.ID, id, &body, true)
	if err != nil {
		dbError(w, "mobileParticipationBoardPut", err)
		return
	}
	if status != 0 {
		http.Error(w, msg, status)
		return
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "mobileParticipationBoardPut commit", err)
		return
	}
	logActivity(u.ID, u.Username, sv.Action, "participation_board", sv.Activity, false, boardSaveDetails(&body, sv)+" · via mobile")
	writeJSON(w, map[string]any{"board_id": sv.BoardID, "event_id": id, "entries": len(body.Entries), "matched": sv.Matched})
}
