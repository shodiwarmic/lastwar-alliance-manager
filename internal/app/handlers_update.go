package app

// handlers_update.go — Admin → Security & API → "About this install and updates": what this
// install runs, and what the release check (release_check.go) last learned.

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
)

// releaseNotesCap is how much of a release's notes the Admin page shows; the rest is a link.
const releaseNotesCap = 2000

// updateStatePayload is GET /api/admin/update's body, also returned by a manual check.
func updateStatePayload() (map[string]any, error) {
	st, err := latestRelease()
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"running": map[string]any{
			"version":     appVersion,
			"commit":      shortCommit(),
			"host_files":  os.Getenv("HOST_FILES_VERSION"),
			"app_version": os.Getenv("APP_VERSION"),
		},
		"check":        releaseCheckMode(),
		"checked_at":   st.CheckedAt,
		"attempted_at": st.AttemptedAt,
		"last_error":   st.LastError,
		"latest":       nil,
	}
	if st.LatestTag != "" {
		notes := []rune(st.LatestBody)
		truncated := len(notes) > releaseNotesCap
		if truncated {
			notes = notes[:releaseNotesCap]
		}
		out["latest"] = map[string]any{
			"tag":             st.LatestTag,
			"name":            st.LatestName,
			"url":             st.LatestURL,
			"published_at":    st.LatestPublishedAt,
			"notes":           string(notes),
			"notes_truncated": truncated,
			"level":           releaseLevel(appVersion, st.LatestTag),
		}
	}
	return out, nil
}

// getUpdateState is GET /api/admin/update. A read, so it stays open in demo mode.
func getUpdateState(w http.ResponseWriter, r *http.Request) {
	out, err := updateStatePayload()
	if err != nil {
		slog.Error("update state: read failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}

// checkForUpdateNow is POST /api/admin/update/check, the Admin page's "Check now". It goes
// through checkLatestRelease, so UPDATE_CHECK=off is refused here exactly as it is for the
// scheduled check, and at most one request a minute reaches GitHub from it.
func checkForUpdateNow(w http.ResponseWriter, r *http.Request) {
	if releaseCheckMode() != "on" {
		http.Error(w, "Release checks are off on this install (UPDATE_CHECK=off in .env).", http.StatusConflict)
		return
	}
	var recent bool
	if err := db.QueryRow(`SELECT COALESCE(attempted_at > datetime('now', '-1 minute'), 0)
		FROM release_state WHERE id = 1`).Scan(&recent); err != nil {
		slog.Error("update check: read failed", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if recent {
		http.Error(w, "Checked less than a minute ago — try again shortly.", http.StatusTooManyRequests)
		return
	}
	switch err := checkLatestRelease(r.Context()); {
	case errors.Is(err, errReleaseCheckBusy):
		http.Error(w, "A check is already running — try again shortly.", http.StatusTooManyRequests)
		return
	case errors.Is(err, errReleaseCheckOff):
		http.Error(w, "Release checks are off on this install (UPDATE_CHECK=off in .env).", http.StatusConflict)
		return
	}
	// Any other failure is recorded in last_error, which the payload carries.
	getUpdateState(w, r)
}

// updateAvailableFor is the newer release an admin's pages nudge about (PageData), or "":
// only for an administrator, only while checks are on, and only when a level can be derived —
// so a dev or edge build is never nudged.
func updateAvailableFor(isAdmin bool) string {
	if !isAdmin || releaseCheckMode() != "on" {
		return ""
	}
	var tag string
	if err := db.QueryRow(`SELECT COALESCE(latest_tag, '') FROM release_state WHERE id = 1`).Scan(&tag); err != nil {
		return ""
	}
	if releaseLevel(appVersion, tag) == "" {
		return ""
	}
	return tag
}
