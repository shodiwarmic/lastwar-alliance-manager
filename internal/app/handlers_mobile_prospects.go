// handlers_mobile_prospects.go - the mobile prospects store: the game-read fields of a
// recruiting prospect (name, server, source alliance, power, hero power, rank in
// alliance). Status, notes, recruiter, seat colour, R4 interest, first contact and type
// are officer judgement and stay web-only.

package app

import (
	"database/sql"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

type mobileProspect struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Server         string `json:"server"`
	SourceAlliance string `json:"source_alliance"`
	Power          *int64 `json:"power"`
	HeroPower      *int64 `json:"hero_power"`
	RankInAlliance string `json:"rank_in_alliance"`
	Status         string `json:"status,omitempty"`
	ProspectType   string `json:"prospect_type,omitempty"`
	Created        *bool  `json:"created,omitempty"`
}

func readMobileProspect(q rowQuerier, id int) (mobileProspect, error) {
	var p mobileProspect
	err := q.QueryRow(`SELECT id, name, COALESCE(server, ''), COALESCE(source_alliance, ''), power, hero_power,
		COALESCE(rank_in_alliance, ''), status, COALESCE(prospect_type, 'transfer') FROM prospects WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Server, &p.SourceAlliance, &p.Power, &p.HeroPower, &p.RankInAlliance, &p.Status, &p.ProspectType)
	return p, err
}

// GET /api/mobile/prospects (view_recruiting) — for matching a scanned profile.
func getMobileProspects(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, COALESCE(server, ''), COALESCE(source_alliance, ''), power, hero_power,
		COALESCE(rank_in_alliance, ''), status, COALESCE(prospect_type, 'transfer') FROM prospects ORDER BY name`)
	if err != nil {
		dbError(w, "getMobileProspects", err)
		return
	}
	defer rows.Close()
	out := []mobileProspect{}
	for rows.Next() {
		var p mobileProspect
		if err := rows.Scan(&p.ID, &p.Name, &p.Server, &p.SourceAlliance, &p.Power, &p.HeroPower, &p.RankInAlliance, &p.Status, &p.ProspectType); err != nil {
			dbError(w, "getMobileProspects scan", err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, out)
}

var serverNumberRe = regexp.MustCompile(`^[0-9]{1,6}$`)

// POST /api/mobile/prospects (manage_recruiting)
// With prospect_id, only the fields sent change (the name included — an in-game rename).
// Without one, a prospect with the same name (case-insensitive) and server (empty
// matching empty) is refused with 409 and its id; otherwise one is created with the web
// create's defaults. The duplicate check and the insert share one transaction — with
// one connection, that is the serialisation point.
func postMobileProspect(w http.ResponseWriter, r *http.Request) {
	user := getAuthUser(r)
	var req struct {
		ProspectID     *int    `json:"prospect_id"`
		Name           *string `json:"name"`
		Server         *string `json:"server"`
		SourceAlliance *string `json:"source_alliance"`
		Power          *int64  `json:"power"`
		HeroPower      *int64  `json:"hero_power"`
		RankInAlliance *string `json:"rank_in_alliance"`
	}
	if !decodeMobileJSON(w, r, &req) {
		return
	}
	trim := func(p *string) {
		if p != nil {
			*p = strings.TrimSpace(*p)
		}
	}
	trim(req.Name)
	trim(req.Server)
	trim(req.SourceAlliance)
	trim(req.RankInAlliance)
	switch {
	case req.Name != nil && *req.Name == "":
		http.Error(w, "name cannot be empty", http.StatusBadRequest)
		return
	case req.Server != nil && *req.Server != "" && !serverNumberRe.MatchString(*req.Server):
		http.Error(w, "server must be a server number", http.StatusBadRequest)
		return
	case req.RankInAlliance != nil && *req.RankInAlliance != "" && !validActiveRank(*req.RankInAlliance):
		http.Error(w, "rank_in_alliance must be R1–R5 or empty", http.StatusBadRequest)
		return
	case req.Power != nil && *req.Power <= 0, req.HeroPower != nil && *req.HeroPower <= 0:
		http.Error(w, "power figures must be greater than 0", http.StatusBadRequest)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		dbError(w, "postMobileProspect begin", err)
		return
	}
	defer tx.Rollback()

	var id int
	var action, details string
	created := false
	if req.ProspectID != nil {
		id = *req.ProspectID
		old, err := readMobileProspect(tx, id)
		if err == sql.ErrNoRows {
			http.Error(w, "Prospect not found", http.StatusNotFound)
			return
		} else if err != nil {
			dbError(w, "postMobileProspect read", err)
			return
		}
		cur := old
		if req.Name != nil {
			cur.Name = *req.Name
		}
		if req.Server != nil {
			cur.Server = *req.Server
		}
		if req.SourceAlliance != nil {
			cur.SourceAlliance = *req.SourceAlliance
		}
		if req.RankInAlliance != nil {
			cur.RankInAlliance = *req.RankInAlliance
		}
		if req.Power != nil {
			cur.Power = req.Power
		}
		if req.HeroPower != nil {
			cur.HeroPower = req.HeroPower
		}
		changes := prospectGameFieldChanges(
			prospectGameFields{old.Name, old.Server, old.SourceAlliance, old.RankInAlliance, old.Power, old.HeroPower},
			prospectGameFields{cur.Name, cur.Server, cur.SourceAlliance, cur.RankInAlliance, cur.Power, cur.HeroPower})
		if len(changes) > 0 {
			if _, err := tx.Exec(`UPDATE prospects SET name = ?, server = ?, source_alliance = ?, power = ?, hero_power = ?,
				rank_in_alliance = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				cur.Name, cur.Server, cur.SourceAlliance, cur.Power, cur.HeroPower, cur.RankInAlliance, id); err != nil {
				dbError(w, "postMobileProspect update", err)
				return
			}
			action, details = "updated", strings.Join(changes, "; ")+" · via mobile"
		}
	} else {
		if req.Name == nil {
			http.Error(w, "name is required to create a prospect", http.StatusBadRequest)
			return
		}
		server := ""
		if req.Server != nil {
			server = *req.Server
		}
		var dupID int
		err := tx.QueryRow(`SELECT id FROM prospects WHERE LOWER(name) = LOWER(?) AND COALESCE(server, '') = ? LIMIT 1`,
			*req.Name, server).Scan(&dupID)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"error": fmt.Sprintf("a prospect named %q on server %q already exists", *req.Name, server), "prospect_id": dupID})
			return
		} else if err != sql.ErrNoRows {
			dbError(w, "postMobileProspect duplicate check", err)
			return
		}
		p := Prospect{Name: *req.Name, Server: server, Power: req.Power, HeroPower: req.HeroPower,
			Status: "interested", ProspectType: "transfer"}
		if req.SourceAlliance != nil {
			p.SourceAlliance = *req.SourceAlliance
		}
		if req.RankInAlliance != nil {
			p.RankInAlliance = *req.RankInAlliance
		}
		if id, err = insertProspect(tx, p); err != nil {
			dbError(w, "postMobileProspect insert", err)
			return
		}
		created = true
		action, details = "created", "server: "+server
		if p.SourceAlliance != "" {
			details += ", from: " + p.SourceAlliance
		}
		details += " · via mobile"
	}
	if err := tx.Commit(); err != nil {
		dbError(w, "postMobileProspect commit", err)
		return
	}

	out, err := readMobileProspect(db, id)
	if err != nil {
		dbError(w, "postMobileProspect reread", err)
		return
	}
	// Linked as the web links it, after the commit: it writes through db.
	if created || req.SourceAlliance != nil {
		linkProspectSourceAlliance(id, out.SourceAlliance)
	}
	if action != "" {
		logActivity(user.ID, user.Username, action, "prospect", out.Name, false, details)
	}
	out.Status, out.ProspectType = "", ""
	out.Created = &created
	writeJSON(w, out)
}
