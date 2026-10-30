// land_lookup.go — Season 4's map areas, for naming Land targets by their map ID
// (private-docs 274, 276).
//
// The lookup is the community map's area list (ID → name, level, logical position), verified
// against Cpt Hedge exports and screenshots in private-docs issue-assets/269. It is EMBEDDED,
// not read from disk: the image ships the binary, templates/, static/ and migrations/ only.
// It gives positions, not adjacency, so nothing here computes connectivity.

package app

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed data/season4_area_lookup.json
var season4LookupJSON []byte

// landArea is one map area.
type landArea struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Code     string `json:"code"`
	Level    int    `json:"level"`
	Kind     string `json:"kind"` // city | stronghold | capitol | trade_post | landmark
	Position struct {
		Kind       string `json:"kind"`
		ColumnBand string `json:"column_band"`
		RowBand    string `json:"row_band"`
	} `json:"position"`
}

var (
	season4Once  sync.Once
	season4Areas []landArea
	season4ByID  map[string]landArea
)

func loadSeason4Lookup() {
	season4Once.Do(func() {
		var doc struct {
			Areas []landArea `json:"areas"`
		}
		if err := json.Unmarshal(season4LookupJSON, &doc); err != nil {
			panic("season4_area_lookup.json: " + err.Error())
		}
		season4ByID = map[string]landArea{}
		for i := range doc.Areas {
			doc.Areas[i].Kind = landKindForCode(doc.Areas[i].Code)
			season4ByID[doc.Areas[i].ID] = doc.Areas[i]
		}
		season4Areas = doc.Areas
	})
}

func landKindForCode(code string) string {
	switch code {
	case "SH":
		return "stronghold"
	case "CASTLE":
		return "capitol"
	case "TP":
		return "trade_post"
	case "HM", "DT":
		return "landmark"
	default:
		return "city"
	}
}

// landLookupFor returns the season's map areas, or nil when the app has no lookup for that
// season number (a typed name is used instead).
func landLookupFor(seasonNumber int) []landArea {
	if seasonNumber != 4 {
		return nil
	}
	loadSeason4Lookup()
	return season4Areas
}

// landAreaByID resolves one area ID for a season, ok false when unknown or no lookup.
func landAreaByID(seasonNumber int, id string) (landArea, bool) {
	if seasonNumber != 4 {
		return landArea{}, false
	}
	loadSeason4Lookup()
	a, ok := season4ByID[id]
	return a, ok
}
