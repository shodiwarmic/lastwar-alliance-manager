package main

// handlers_jobs.go — HTTP surface for the background job runner (see jobs.go).
//
// Three endpoints, all permission-gated per job kind rather than by a single
// blanket permission: starting a NAP sweep is a manage_allies action, starting a
// prospect refresh is manage_recruiting, and neither should be reachable by
// someone who only has the other.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type jobStartRequest struct {
	Kind string `json:"kind"`
}

type jobCancelRequest struct {
	ID int64 `json:"id"`
}

// resolveJobKind validates the kind and checks the caller holds its permission.
// Returns false having already written the response.
func resolveJobKind(w http.ResponseWriter, r *http.Request, kind string) (jobKind, bool) {
	def, ok := jobRegistry[strings.TrimSpace(kind)]
	if !ok {
		http.Error(w, "Unknown job type", http.StatusBadRequest)
		return jobKind{}, false
	}
	user := getAuthUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return jobKind{}, false
	}
	if !userHasPermission(user, def.Permission) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return jobKind{}, false
	}
	return def, true
}

func startJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if _, ok := resolveJobKind(w, r, req.Kind); !ok {
		return
	}

	user := getAuthUser(r)
	id, err := startJob(req.Kind, jobActor{UserID: user.ID, Username: user.Username})
	if err != nil {
		// 409 with the running kind and elapsed time, not a bare error: with a
		// scheduled sweep holding the slot for tens of minutes, "it's busy" without
		// saying what or for how long is a dead end for the officer.
		if errors.Is(err, errJobBusy) {
			kind, since, ok := currentJobInfo()
			msg := "Another sync is already running."
			if ok {
				msg = jobKindLabel(kind) + " is already running (started " +
					humanizeJobElapsed(since) + " ago). Wait for it to finish, or cancel it."
			}
			http.Error(w, msg, http.StatusConflict)
			return
		}
		slog.Error("job start failed", "kind", req.Kind, "error", err)
		http.Error(w, "Could not start the job", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"id": id, "kind": req.Kind})
}

// currentJobHandler backs the progress poll. It returns the LATEST job of the
// kind, running or not, so a browser arriving after a run finished still sees the
// outcome instead of an empty panel.
func currentJobHandler(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if _, ok := resolveJobKind(w, r, kind); !ok {
		return
	}
	job, err := loadLatestJob(strings.TrimSpace(kind))
	if err != nil {
		slog.Error("job load failed", "kind", kind, "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if job == nil {
		writeJSON(w, map[string]any{"job": nil})
		return
	}
	writeJSON(w, map[string]any{"job": job})
}

func cancelJobHandler(w http.ResponseWriter, r *http.Request) {
	var req jobCancelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	kind, _, running := currentJobInfo()
	if !running {
		http.Error(w, "No job is running", http.StatusConflict)
		return
	}
	// Gate on the RUNNING job's kind: cancelling somebody else's sweep needs the
	// permission for that sweep, not merely for one you happen to hold.
	if _, ok := resolveJobKind(w, r, kind); !ok {
		return
	}
	if !cancelJob(req.ID) {
		http.Error(w, "That job is no longer running", http.StatusConflict)
		return
	}
	writeJSON(w, map[string]string{"status": "cancelling"})
}

func jobKindLabel(kind string) string {
	if def, ok := jobRegistry[kind]; ok && def.Label != "" {
		return def.Label
	}
	return "A sync"
}

// humanizeJobElapsed renders a duration for the busy message. Coarse on purpose —
// "23 minutes" tells an officer whether to wait; "23m14.7s" just reads like a log line.
func humanizeJobElapsed(d time.Duration) string {
	switch mins := int(d.Minutes()); {
	case mins < 1:
		return "less than a minute"
	case mins == 1:
		return "1 minute"
	case mins < 60:
		return strconv.Itoa(mins) + " minutes"
	default:
		hrs := mins / 60
		if hrs == 1 {
			return "about an hour"
		}
		return "about " + strconv.Itoa(hrs) + " hours"
	}
}
