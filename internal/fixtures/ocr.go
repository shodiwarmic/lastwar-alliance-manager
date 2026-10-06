package fixtures

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"lastwar-alliance/internal/demo"
)

// maxBatchBytes matches Cloud Run's 32 MiB request cap, which the real service sits behind.
const maxBatchBytes = 32 << 20

// cannedNote is what the diagnostics say about every image, and it reaches the app's
// activity-log line: nothing here was read from a picture.
const cannedNote = "Demo: a canned board for this category, not read from the image."

type section struct {
	Image         string  `json:"image"`
	Category      string  `json:"category"`
	Confidence    float64 `json:"confidence"`
	Method        string  `json:"method"`
	PlayersFound  int     `json:"players_found"`
	Note          string  `json:"note"`
	MailTimestamp *string `json:"mail_timestamp,omitempty"`
}

type player struct {
	PlayerName string `json:"player_name"`
	Score      int64  `json:"score"`
	Rank       int    `json:"rank"`
}

func refuse(w http.ResponseWriter, msg, code string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg, "code": code})
}

// processBatch answers any upload with the canned board for its category, in the v1
// envelope. The demo runs the app in local OCR mode, which always names the category —
// a missing one is refused in the service's own words, as the real local service does.
func (s *server) processBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBatchBytes)
	if err := r.ParseMultipartForm(maxBatchBytes); err != nil {
		refuse(w, "The upload could not be read as images.", "bad_request")
		return
	}
	if v := r.FormValue("schema_version"); v != "" && v != "1" {
		refuse(w, "This service speaks contract version 1; the request asked for "+v+".", "schema_not_supported")
		return
	}
	category := r.FormValue("category")
	if category == "" {
		refuse(w, "Pick the screen these images show — the demo's OCR does not detect it.", "category_required")
		return
	}
	if !slices.Contains(Categories(), category) {
		refuse(w, "This service does not read "+strconv.Quote(category)+".", "category_not_supported")
		return
	}
	images := r.MultipartForm.File["images"]
	if len(images) == 0 {
		refuse(w, "No images were uploaded.", "bad_request")
		return
	}

	board := s.manifest.Boards[category]
	rows := make([]player, len(board))
	for i, b := range board {
		rows[i] = player{PlayerName: b.Name, Score: b.Score, Rank: b.Rank}
	}
	// A mail carries its date-time line. "Now" is always within the import's three-day
	// window after some seeded occurrence, which runs every other day or more often.
	var ts *string
	if slices.Contains(demo.MailCategories, category) {
		now := time.Now().UTC().Format("2006-01-02 15:04:05")
		ts = &now
	}
	sections := make([]section, len(images))
	for i, img := range images {
		sections[i] = section{Image: img.Filename, Category: category, Confidence: 1,
			Method: "demo_canned", Note: cannedNote, MailTimestamp: ts}
	}
	sections[0].PlayersFound = len(rows)

	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version": 1,
		"results":        map[string][]player{category: rows},
		"diagnostics": map[string]any{
			"schema_version":    1,
			"engine":            "demo-canned",
			"image_count":       len(images),
			"batch_count":       1,
			"category_override": category,
			"sections":          sections,
		},
	})
}
