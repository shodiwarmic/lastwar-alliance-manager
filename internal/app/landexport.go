// landexport.go — Cpt Hedge's map-planner export (private-docs 276, issue-assets/269). Pure:
// it reads the bytes of a grids-export file and reports what it holds and what is wrong with
// it; nothing here touches the database.
//
// The file is stored as it came (a Files row); this reading is what the snapshot card shows and
// what a proposed-path import starts from. Unknown fields are ignored here and kept in the file.
// Alliance ids, names and colours are the planner's own labels, never this app's alliances, and
// an alliance's areaIds are claims drawn on a map — they never become holdings or targets.

package app

import (
	"encoding/json"
	"fmt"
	"sort"
)

type hedgeExport struct {
	Version    string      `json:"version"`
	ExportDate string      `json:"exportDate"`
	Grids      []hedgeGrid `json:"grids"`
}

type hedgeGrid struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Alliances []hedgeAlliance `json:"alliances"`
	PathSteps []hedgePathStep `json:"pathSteps"`
	Selected  *string         `json:"selectedAllianceId"`
}

type hedgeAlliance struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	AreaIDs []string `json:"areaIds"`
	Color   string   `json:"color"`
}

type hedgePathStep struct {
	AreaID     string  `json:"areaId"`
	AllianceID string  `json:"allianceId"`
	Step       float64 `json:"step"`
}

// hedgePathView is one alliance's proposed path in a grid, steps resolved and in order.
type hedgePathView struct {
	GridID       string          `json:"grid_id"`
	GridName     string          `json:"grid_name"`
	AllianceID   string          `json:"alliance_id"`
	AllianceName string          `json:"alliance_name"`
	Steps        []hedgeStepView `json:"steps"`
	Problems     []string        `json:"problems"`
}

type hedgeStepView struct {
	Step   int    `json:"step"`
	AreaID string `json:"area_id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Level  *int   `json:"level"`
	Known  bool   `json:"known"`
}

// hedgeSummary is what a snapshot card shows of its file.
type hedgeSummary struct {
	Valid      bool            `json:"valid"`
	Problems   []string        `json:"problems"`
	ExportDate string          `json:"export_date"`
	GridNames  []string        `json:"grid_names"`
	Alliances  int             `json:"alliances"`
	Claims     int             `json:"claims"`
	Paths      []hedgePathView `json:"paths"`
}

// readHedgeExport validates an export. A file that is not even JSON of the right shape comes
// back Valid false with its problems; the caller still stores it. seasonNumber 4 resolves
// area IDs through the S4 lookup; any other season stores without resolution.
func readHedgeExport(b []byte, seasonNumber int) hedgeSummary {
	out := hedgeSummary{Problems: []string{}, GridNames: []string{}, Paths: []hedgePathView{}}
	var doc hedgeExport
	if err := json.Unmarshal(b, &doc); err != nil {
		out.Problems = append(out.Problems, "not a map export: "+err.Error())
		return out
	}
	if doc.Version != "1.0" {
		out.Problems = append(out.Problems, fmt.Sprintf("version %q is not the 1.0 this app reads", doc.Version))
	}
	if doc.ExportDate == "" {
		out.Problems = append(out.Problems, "no exportDate")
	}
	if len(doc.Grids) == 0 {
		out.Problems = append(out.Problems, "no grids")
	}
	out.ExportDate = doc.ExportDate
	resolve := seasonNumber == 4
	gridIDs := map[string]bool{}
	for gi, g := range doc.Grids {
		label := g.Name
		if label == "" {
			label = fmt.Sprintf("grid %d", gi+1)
		}
		out.GridNames = append(out.GridNames, label)
		if g.ID == "" || gridIDs[g.ID] {
			out.Problems = append(out.Problems, label+": missing or repeated grid id")
		}
		gridIDs[g.ID] = true
		alliances := map[string]hedgeAlliance{}
		claimedBy := map[string]string{}
		for _, a := range g.Alliances {
			if a.ID == "" || alliances[a.ID].ID != "" {
				out.Problems = append(out.Problems, label+": missing or repeated alliance id")
			}
			alliances[a.ID] = a
			out.Alliances++
			for _, id := range a.AreaIDs {
				out.Claims++
				if resolve {
					if _, ok := landAreaByID(4, id); !ok {
						out.Problems = append(out.Problems, fmt.Sprintf("%s: %s claims area %s, which is not on the Season 4 map", label, a.Name, id))
					}
				}
				if other, ok := claimedBy[id]; ok && other != a.ID {
					out.Problems = append(out.Problems, fmt.Sprintf("%s: area %s is claimed by two alliances", label, id))
				}
				claimedBy[id] = a.ID
			}
		}
		if g.Selected != nil && *g.Selected != "" && alliances[*g.Selected].ID == "" {
			out.Problems = append(out.Problems, label+": the selected alliance is not in the grid")
		}
		// Paths, one per alliance, in step order.
		byAlliance := map[string][]hedgePathStep{}
		var order []string
		for _, st := range g.PathSteps {
			if _, ok := byAlliance[st.AllianceID]; !ok {
				order = append(order, st.AllianceID)
			}
			byAlliance[st.AllianceID] = append(byAlliance[st.AllianceID], st)
		}
		for _, aid := range order {
			steps := byAlliance[aid]
			sort.SliceStable(steps, func(i, j int) bool { return steps[i].Step < steps[j].Step })
			pv := hedgePathView{GridID: g.ID, GridName: label, AllianceID: aid, AllianceName: alliances[aid].Name,
				Steps: []hedgeStepView{}, Problems: []string{}}
			if alliances[aid].ID == "" {
				pv.Problems = append(pv.Problems, "the path names an alliance that is not in the grid")
			}
			seen := map[int]bool{}
			maxStep := 0
			for _, st := range steps {
				n := int(st.Step)
				if float64(n) != st.Step || n < 1 {
					pv.Problems = append(pv.Problems, fmt.Sprintf("step %v is not a whole number from 1", st.Step))
				} else if seen[n] {
					pv.Problems = append(pv.Problems, fmt.Sprintf("step %d appears twice", n))
				}
				seen[n] = true
				if n > maxStep {
					maxStep = n
				}
				sv := hedgeStepView{Step: n, AreaID: st.AreaID, Name: "Area " + st.AreaID, Kind: "city"}
				if resolve {
					if a, ok := landAreaByID(4, st.AreaID); ok {
						lv := a.Level
						sv.Name, sv.Kind, sv.Level, sv.Known = a.Name, a.Kind, &lv, true
					} else {
						pv.Problems = append(pv.Problems, fmt.Sprintf("step %d: area %s is not on the Season 4 map", n, st.AreaID))
					}
				}
				pv.Steps = append(pv.Steps, sv)
			}
			for n := 1; n <= maxStep; n++ {
				if !seen[n] {
					pv.Problems = append(pv.Problems, fmt.Sprintf("step %d is missing", n))
				}
			}
			out.Paths = append(out.Paths, pv)
		}
	}
	out.Valid = len(out.Problems) == 0
	return out
}
