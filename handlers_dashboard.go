package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// allowedCards returns the ordered list of card IDs a user may see based on their permissions.
func allowedCards(data PageData) []DashboardCard {
	cards := []DashboardCard{
		{ID: "health", Visible: true},
	}
	if data.Permissions.ViewVSPoints {
		cards = append(cards, DashboardCard{ID: "vs", Visible: true})
	}
	if data.Permissions.ViewSchedule {
		cards = append(cards, DashboardCard{ID: "schedule", Visible: true})
	}
	if data.Permissions.ViewAllies {
		cards = append(cards, DashboardCard{ID: "diplomacy", Visible: true})
	}
	if data.Rank == "R4" || data.Rank == "R5" || data.IsAdmin {
		cards = append(cards, DashboardCard{ID: "leader-flags", Visible: true})
	}
	return cards
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	data := getPageData(r, "Dashboard - Alliance Manager", "dashboard")
	if !data.IsAuthenticated {
		http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
		return
	}

	var vsMin int
	if err := db.QueryRow("SELECT COALESCE(vs_minimum_points, 2500000) FROM settings WHERE id = 1").Scan(&vsMin); err != nil {
		slog.Error("dashboard: failed to read vs_minimum_points", "error", err)
		vsMin = 2500000
	}
	data.VSMinimumPoints = vsMin

	renderTemplate(w, r, "dashboard.html", data)
}

func getDashboardPrefs(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	data := getPageData(r, "", "")
	allowed := allowedCards(data)

	// Build a set of allowed IDs for fast lookup
	allowedSet := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		allowedSet[c.ID] = true
	}

	// Load stored prefs
	var raw string
	err := db.QueryRow("SELECT prefs FROM user_dashboard_prefs WHERE user_id = ?", userID).Scan(&raw)
	if err != nil {
		// No saved prefs — return defaults
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DashboardPrefsResponse{Prefs: allowed, Available: allowed})
		return
	}

	var stored []DashboardCard
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		slog.Error("dashboard: failed to parse stored prefs", "error", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DashboardPrefsResponse{Prefs: allowed, Available: allowed})
		return
	}

	// Merge: keep stored cards that are still allowed, then append any newly allowed cards not in prefs
	seen := make(map[string]bool, len(stored))
	merged := make([]DashboardCard, 0, len(allowed))
	for _, c := range stored {
		if allowedSet[c.ID] {
			merged = append(merged, c)
			seen[c.ID] = true
		}
	}
	for _, c := range allowed {
		if !seen[c.ID] {
			merged = append(merged, c)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DashboardPrefsResponse{Prefs: merged, Available: allowed})
}

func saveDashboardPrefs(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "session")
	userID, _ := session.Values["user_id"].(int)

	var submitted []DashboardCard
	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate: reject any card ID not in the user's allowed pool
	data := getPageData(r, "", "")
	allowed := allowedCards(data)
	allowedSet := make(map[string]bool, len(allowed))
	for _, c := range allowed {
		allowedSet[c.ID] = true
	}
	for _, c := range submitted {
		if !allowedSet[c.ID] {
			http.Error(w, "Invalid card ID: "+c.ID, http.StatusBadRequest)
			return
		}
	}

	raw, err := json.Marshal(submitted)
	if err != nil {
		slog.Error("dashboard: failed to marshal prefs", "error", err)
		http.Error(w, "Failed to save preferences", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(`INSERT INTO user_dashboard_prefs (user_id, prefs, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET prefs = excluded.prefs, updated_at = excluded.updated_at`,
		userID, string(raw))
	if err != nil {
		slog.Error("dashboard: failed to upsert prefs", "error", err)
		http.Error(w, "Failed to save preferences", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "Preferences saved"})
}
