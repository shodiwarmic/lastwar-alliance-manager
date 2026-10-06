// handlers_preview.go - Admin "preview as rank" (175): use the app with a given rank's
// permissions, without a second account. The preview itself is applied in
// loadSessionUser (middleware.go); these handlers only set and clear the session flag.

package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// startRankPreview serves POST /api/preview-rank {rank}. Also used to switch rank while a
// preview is already running.
func startRankPreview(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var body struct {
		Rank string `json:"rank"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !validPreviewRank(body.Rank) {
		http.Error(w, "Unknown rank", http.StatusBadRequest)
		return
	}
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM rank_permissions WHERE rank = ?)`, body.Rank).Scan(&exists); err != nil {
		slog.Error("preview-rank: rank lookup failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "That rank has no permissions row", http.StatusBadRequest)
		return
	}

	session, _ := store.Get(r, "session")
	previous, _ := session.Values[previewRankKey].(string)
	session.Values[previewRankKey] = body.Rank
	if err := session.Save(r, w); err != nil {
		slog.Error("preview-rank: session save failed", "error", err)
		http.Error(w, "Failed to start the preview", http.StatusInternalServerError)
		return
	}
	details := "previewing as " + body.Rank
	if previous != "" && previous != body.Rank {
		details = "switched from " + previous + " to " + body.Rank
	}
	logActivity(user.ID, user.Username, "started", "rank_preview", body.Rank, true, details)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"preview_rank": body.Rank})
}

// endRankPreview serves DELETE /api/preview-rank. A no-op (and no activity row) when no
// preview is running.
func endRankPreview(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	session, _ := store.Get(r, "session")
	rank, _ := session.Values[previewRankKey].(string)
	if rank == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	delete(session.Values, previewRankKey)
	if err := session.Save(r, w); err != nil {
		slog.Error("preview-rank: session save failed", "error", err)
		http.Error(w, "Failed to end the preview", http.StatusInternalServerError)
		return
	}
	logActivity(user.ID, user.Username, "ended", "rank_preview", rank, true)
	w.WriteHeader(http.StatusNoContent)
}
