package app

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// healthHandler is GET /api/health: 200 {"status":"ok"} when a query round-trips through
// the guarded pool, 503 {"status":"degraded"} otherwise, and nothing else — no version, no
// counts, so it is safe without a session and tells a stranger nothing. A wedged connection
// surfaces as a 503 after dbAcquireCeiling rather than as a hang, because db.QueryRow is the
// guarded pool's.
//
// This is process-and-query liveness: the open connection keeps answering SELECT 1 even if
// the database file has become unwritable, so it does not claim the database takes writes.
// It is what the image's HEALTHCHECK probes (via the healthcheck subcommand below), what
// install.sh waits on, and what the host update agent reads to decide an update worked.
//
// Unauthenticated and exempt from the setup gate (setupGate): a fresh install must report
// healthy before its first administrator exists.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var one int
	if err := db.QueryRow("SELECT 1").Scan(&one); err != nil {
		slog.Warn("Health check failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"status": "degraded"})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// healthcheckURL is where the healthcheck subcommand probes: this container's own listener.
// The environment is the container's (Docker runs the probe inside it), so PORT is already
// what the server read; no .env is loaded.
func healthcheckURL() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return "http://127.0.0.1:" + port + "/api/health"
}

// runHealthcheck is `alliance-manager healthcheck`, the image's HEALTHCHECK command: GET url
// and return the process exit status, 0 on a 200 and 1 otherwise, saying why on out. The
// probe is the binary itself because the runtime image (debian:bookworm-slim) has neither
// curl nor wget, and adding one for this would grow every install's image.
func runHealthcheck(url string, out io.Writer) int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(out, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(out, "healthcheck:", resp.Status)
		return 1
	}
	return 0
}
