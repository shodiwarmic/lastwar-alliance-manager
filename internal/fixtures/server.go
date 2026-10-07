// Package fixtures is the demo's stand-in for the two services an install talks to that
// the demo cannot run: the OCR service and Collabora. One handler, one URL:
//
//   - GET  /health                  — what the OCR service reports about itself (contract v1)
//   - POST /process-batch           — a canned leaderboard for the requested category, naming
//     the demo alliance's members (internal/demo's manifest), whatever image was uploaded
//   - GET|POST /browser/dist/cool.html — a picture of the document the app asked to open,
//     in the requested theme, where a real install shows the editor
//
// It needs no database and no app code: everything comes from demo.Manifest(), a pure
// function of the seed. Configured by flags in cmd/demo-fixtures, not environment
// variables.
package fixtures

import (
	"encoding/json"
	"net/http"

	"lastwar-alliance/internal/demo"
)

// Version and Commit name the build, stamped by Dockerfile.fixtures with -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

// Config is the service's configuration.
type Config struct {
	// FrameAncestors is the CSP frame-ancestors value for the Collabora page: the app's
	// origin, so only the demo app may embed it. Empty means 'none'.
	FrameAncestors string
}

type server struct {
	cfg      Config
	manifest demo.ManifestData
}

// Handler returns the service's routes.
func Handler(cfg Config) http.Handler {
	s := &server{cfg: cfg, manifest: demo.Manifest()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /process-batch", s.processBatch)
	mux.HandleFunc("/browser/dist/cool.html", s.collabora)
	mux.Handle("GET /img/", http.StripPrefix("/img/", http.HandlerFunc(s.image)))
	return mux
}

// Categories is every category the service reads: the twenty-three rankings and the
// three post-event mails.
func Categories() []string {
	return append(append([]string{}, demo.RankingCategories...), demo.MailCategories...)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"version":         Version,
		"commit":          Commit,
		"schema_versions": []int{1},
		"categories":      Categories(),
		"engine":          "demo-canned",
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
