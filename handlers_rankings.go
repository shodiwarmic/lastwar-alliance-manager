package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// loadRecommendations loads recommendation counts for all members (active only)
// A recommendation is active if the member hasn't been assigned as conductor/backup after it was created
func loadRecommendations() (map[int]int, error) {
	rows, err := db.Query(`
		SELECT r.member_id, COUNT(*) as rec_count
		FROM recommendations r
		WHERE NOT EXISTS (
			SELECT 1 FROM train_schedules ts
			WHERE (ts.conductor_id = r.member_id OR (ts.backup_id = r.member_id AND ts.conductor_showed_up = 0))
			AND ts.date >= date(r.created_at)
		)
		GROUP BY r.member_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recommendationMap := make(map[int]int)
	for rows.Next() {
		var memberID, count int
		if err := rows.Scan(&memberID, &count); err != nil {
			return nil, err
		}
		recommendationMap[memberID] = count
	}
	return recommendationMap, nil
}

// loadAwards loads award scores for all members (active only)
// An award is active if the member hasn't been assigned as conductor/backup after the award week
func loadAwards(settings Settings) (map[int]int, error) {
	rows, err := db.Query(`
		SELECT a.member_id, a.rank
		FROM awards a
		WHERE NOT EXISTS (
			SELECT 1 FROM train_schedules ts
			WHERE (ts.conductor_id = a.member_id OR (ts.backup_id = a.member_id AND ts.conductor_showed_up = 0))
			AND ts.date >= a.week_date
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	awardScoreMap := make(map[int]int)
	for rows.Next() {
		var memberID, rank int
		if err := rows.Scan(&memberID, &rank); err != nil {
			return nil, err
		}
		switch rank {
		case 1:
			awardScoreMap[memberID] += settings.AwardFirstPoints
		case 2:
			awardScoreMap[memberID] += settings.AwardSecondPoints
		case 3:
			awardScoreMap[memberID] += settings.AwardThirdPoints
		}
	}
	return awardScoreMap, nil
}

// loadConductorStats loads conductor statistics for all members
func loadConductorStats() (map[int]ConductorStat, float64, error) {
	rows, err := db.Query(`
		SELECT conductor_id, COUNT(*) as conductor_count, MAX(date) as last_date
		FROM train_schedules
		GROUP BY conductor_id
	`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	conductorStats := make(map[int]ConductorStat)
	totalConductorCount := 0
	memberCount := 0

	for rows.Next() {
		var memberID, count int
		var lastDate sql.NullString
		if err := rows.Scan(&memberID, &count, &lastDate); err != nil {
			return nil, 0, err
		}
		var lastDatePtr *string
		if lastDate.Valid {
			lastDatePtr = &lastDate.String
		}
		conductorStats[memberID] = ConductorStat{
			Count:    count,
			LastDate: lastDatePtr,
		}
		totalConductorCount += count
		memberCount++
	}

	// Load backup usage dates (when conductor didn't show up)
	backupRows, err := db.Query(`
		SELECT backup_id, MAX(date) as last_backup_used
		FROM train_schedules
		WHERE conductor_showed_up = 0
		GROUP BY backup_id
	`)
	if err != nil {
		return nil, 0, err
	}
	defer backupRows.Close()

	for backupRows.Next() {
		var memberID int
		var lastBackupUsed sql.NullString
		if err := backupRows.Scan(&memberID, &lastBackupUsed); err != nil {
			return nil, 0, err
		}
		var lastBackupUsedPtr *string
		if lastBackupUsed.Valid {
			lastBackupUsedPtr = &lastBackupUsed.String
		}

		// Update existing stat or create new one
		if stat, exists := conductorStats[memberID]; exists {
			stat.LastBackupUsed = lastBackupUsedPtr
			conductorStats[memberID] = stat
		} else {
			conductorStats[memberID] = ConductorStat{
				Count:          0,
				LastDate:       nil,
				LastBackupUsed: lastBackupUsedPtr,
			}
		}
	}

	var avgConductorCount float64
	if memberCount > 0 {
		avgConductorCount = float64(totalConductorCount) / float64(memberCount)
	}

	return conductorStats, avgConductorCount, nil
}

// buildRankingContext creates a complete ranking context for calculations
func buildRankingContext(referenceDate time.Time) (*RankingContext, error) {
	settings, err := loadSettings()
	if err != nil {
		return nil, err
	}

	recommendationMap, err := loadRecommendations()
	if err != nil {
		return nil, err
	}

	// Get all non-expired awards (stacks up over multiple weeks)
	awardScoreMap, err := loadAwards(settings)
	if err != nil {
		return nil, err
	}

	conductorStats, avgConductorCount, err := loadConductorStats()
	if err != nil {
		return nil, err
	}

	return &RankingContext{
		Settings:          settings,
		RecommendationMap: recommendationMap,
		AwardScoreMap:     awardScoreMap,
		ConductorStats:    conductorStats,
		AvgConductorCount: avgConductorCount,
		ReferenceDate:     referenceDate,
	}, nil
}

// calculateMemberScore calculates the ranking score for a member
func calculateMemberScore(member Member, ctx *RankingContext) int {
	score := 0

	// Add recommendation points with non-linear scaling (diminishing returns)
	recCount := ctx.RecommendationMap[member.ID]
	if recCount > 0 {
		// Formula: 5 + 5 * sqrt(recCount) rounded to nearest int
		recPoints := 5.0 + 5.0*math.Sqrt(float64(recCount))
		score += int(math.Round(recPoints))
	}

	// Add award points
	score += ctx.AwardScoreMap[member.ID]

	// Add rank boost for R4/R5 members (exponential based on days since last conductor)
	if member.Rank == "R4" || member.Rank == "R5" {
		baseBoost := float64(ctx.Settings.R4R5RankBoost)

		// Calculate days since last conductor/backup duty
		var daysSinceLastDuty int = 0
		if stats, exists := ctx.ConductorStats[member.ID]; exists {
			var mostRecentDate *time.Time

			if stats.LastDate != nil {
				if lastDate, err := parseDate(*stats.LastDate); err == nil {
					mostRecentDate = &lastDate
				}
			}

			// Check if backup usage was more recent
			if stats.LastBackupUsed != nil {
				if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
					if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
						mostRecentDate = &backupDate
					}
				}
			}

			if mostRecentDate != nil {
				daysSinceLastDuty = int(ctx.ReferenceDate.Sub(*mostRecentDate).Hours() / 24)
			}
		}

		// Exponential formula: base_boost * 2^(days/7)
		multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
		score += int(math.Round(baseBoost * multiplier))
	}

	// Add first time conductor boost if member has never been conductor and has some points
	if stats, exists := ctx.ConductorStats[member.ID]; !exists || stats.Count == 0 {
		// Only give boost if they have some positive score (awards, recommendations, or rank boost)
		if score > 0 {
			score += ctx.Settings.FirstTimeConductorBoost
		}
	}

	// Apply conductor-based penalties
	if stats, exists := ctx.ConductorStats[member.ID]; exists {
		// Penalize if above average conductor count
		if float64(stats.Count) > ctx.AvgConductorCount {
			score -= ctx.Settings.AboveAverageConductorPenalty
		}

		// Penalize recent conductors - check both conductor date and backup used date
		var mostRecentDate *time.Time

		if stats.LastDate != nil {
			if lastDate, err := parseDate(*stats.LastDate); err == nil {
				mostRecentDate = &lastDate
			}
		}

		// If they stepped in as backup, check if that's more recent
		if stats.LastBackupUsed != nil {
			if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
				if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
					mostRecentDate = &backupDate
				}
			}
		}

		// Apply penalty based on most recent duty (conductor or backup usage)
		if mostRecentDate != nil {
			daysSince := int(ctx.ReferenceDate.Sub(*mostRecentDate).Hours() / 24)
			penalty := ctx.Settings.RecentConductorPenaltyDays - daysSince
			if penalty > 0 {
				score -= penalty
			}
		}
	}

	return score
}

// Get member rankings with detailed score breakdown
func getMemberRankings(w http.ResponseWriter, r *http.Request) {
	// Always include all awards (active and inactive) - filtering is done on client side

	// Build ranking context using current date
	now := time.Now()
	ctx, err := buildRankingContext(now)
	if err != nil {
		http.Error(w, "Failed to load ranking context: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all members
	rows, err := db.Query("SELECT id, name, rank FROM members ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	// Load all award details with expired flag
	awardQuery := `
		SELECT 
			a.member_id, 
			a.award_type, 
			a.rank, 
			a.week_date,
			CASE 
				WHEN EXISTS (
					SELECT 1 FROM train_schedules ts
					WHERE (ts.conductor_id = a.member_id OR (ts.backup_id = a.member_id AND ts.conductor_showed_up = 0))
					AND ts.date >= a.week_date
				) THEN 1
				ELSE 0
			END as expired
		FROM awards a
		ORDER BY a.week_date DESC, a.rank ASC
	`

	awardRows, err := db.Query(awardQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer awardRows.Close()

	memberAwards := make(map[int][]AwardDetail)
	for awardRows.Next() {
		var memberID, rank, expired int
		var awardType, weekDate string
		if err := awardRows.Scan(&memberID, &awardType, &rank, &weekDate, &expired); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		points := 0
		switch rank {
		case 1:
			points = ctx.Settings.AwardFirstPoints
		case 2:
			points = ctx.Settings.AwardSecondPoints
		case 3:
			points = ctx.Settings.AwardThirdPoints
		}
		memberAwards[memberID] = append(memberAwards[memberID], AwardDetail{
			AwardType: awardType,
			Rank:      rank,
			Points:    points,
			WeekDate:  weekDate,
			Expired:   expired == 1,
		})
	}

	// Calculate rankings for each member
	var rankings []MemberRanking
	for _, member := range members {
		ranking := MemberRanking{
			Member:         member,
			AwardDetails:   memberAwards[member.ID],
			ConductorCount: 0,
		}

		// Calculate award points
		ranking.AwardPoints = ctx.AwardScoreMap[member.ID]

		// Calculate recommendation points
		recCount := ctx.RecommendationMap[member.ID]
		ranking.RecommendationCount = recCount
		ranking.RecommendationPoints = recCount * ctx.Settings.RecommendationPoints

		// Apply rank boost for R4/R5 members
		if member.Rank == "R4" || member.Rank == "R5" {
			baseBoost := float64(ctx.Settings.R4R5RankBoost)

			var daysSinceLastDuty int = 0
			if stats, exists := ctx.ConductorStats[member.ID]; exists {
				var mostRecentDate *time.Time

				if stats.LastDate != nil {
					if lastDate, err := parseDate(*stats.LastDate); err == nil {
						mostRecentDate = &lastDate
					}
				}

				if stats.LastBackupUsed != nil {
					if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
						if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
							mostRecentDate = &backupDate
						}
					}
				}

				if mostRecentDate != nil {
					daysSinceLastDuty = int(now.Sub(*mostRecentDate).Hours() / 24)
				}
			}

			multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
			ranking.RankBoost = int(math.Round(baseBoost * multiplier))
		}

		// Apply first time conductor boost
		if stats, exists := ctx.ConductorStats[member.ID]; !exists || stats.Count == 0 {
			baseScore := ranking.AwardPoints + ranking.RecommendationPoints + ranking.RankBoost
			if baseScore > 0 {
				ranking.FirstTimeConductorBoost = ctx.Settings.FirstTimeConductorBoost
			}
		}

		// Get conductor stats
		if stats, exists := ctx.ConductorStats[member.ID]; exists {
			ranking.ConductorCount = stats.Count
			ranking.LastConductorDate = stats.LastDate

			if float64(stats.Count) > ctx.AvgConductorCount {
				ranking.AboveAveragePenalty = ctx.Settings.AboveAverageConductorPenalty
			}

			var mostRecentDate *time.Time

			if stats.LastDate != nil {
				if lastDate, err := parseDate(*stats.LastDate); err == nil {
					mostRecentDate = &lastDate
				}
			}

			if stats.LastBackupUsed != nil {
				if backupDate, err := parseDate(*stats.LastBackupUsed); err == nil {
					if mostRecentDate == nil || backupDate.After(*mostRecentDate) {
						mostRecentDate = &backupDate
					}
				}
			}

			if mostRecentDate != nil {
				daysSince := int(now.Sub(*mostRecentDate).Hours() / 24)
				ranking.DaysSinceLastConductor = &daysSince
				penalty := ctx.Settings.RecentConductorPenaltyDays - daysSince
				if penalty > 0 {
					ranking.RecentConductorPenalty = penalty
				}
			}
		}

		// Calculate total score
		ranking.TotalScore = calculateMemberScore(member, ctx)

		rankings = append(rankings, ranking)
	}

	// Sort by total score (highest first)
	for i := 0; i < len(rankings); i++ {
		for j := i + 1; j < len(rankings); j++ {
			if rankings[j].TotalScore > rankings[i].TotalScore {
				rankings[i], rankings[j] = rankings[j], rankings[i]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rankings":                rankings,
		"settings":                ctx.Settings,
		"average_conductor_count": ctx.AvgConductorCount,
	})
}

// Get member point accumulation timelines over specified months
func getMemberTimelines(w http.ResponseWriter, r *http.Request) {
	monthsParam := r.URL.Query().Get("months")
	months := 3
	if monthsParam != "" {
		if m, err := strconv.Atoi(monthsParam); err == nil && m > 0 {
			months = m
		}
	}

	now := time.Now()
	startDate := now.AddDate(0, -months, 0)

	memberRows, err := db.Query("SELECT id, name, rank FROM members ORDER BY name")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer memberRows.Close()

	var members []Member
	for memberRows.Next() {
		var m Member
		if err := memberRows.Scan(&m.ID, &m.Name, &m.Rank); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	var settings Settings
	err = db.QueryRow(`SELECT award_first_points, award_second_points, award_third_points, 
		recommendation_points, r4r5_rank_boost, first_time_conductor_boost,
		recent_conductor_penalty_days, above_average_conductor_penalty 
		FROM settings WHERE id = 1`).Scan(
		&settings.AwardFirstPoints, &settings.AwardSecondPoints,
		&settings.AwardThirdPoints, &settings.RecommendationPoints,
		&settings.R4R5RankBoost, &settings.FirstTimeConductorBoost,
		&settings.RecentConductorPenaltyDays, &settings.AboveAverageConductorPenalty)
	if err != nil {
		http.Error(w, "Failed to load settings: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var totalConductors, memberCount int
	db.QueryRow(`
		SELECT COUNT(DISTINCT conductor_id), 
		       (SELECT COUNT(*) FROM members WHERE eligible = 1)
		FROM train_schedules
	`).Scan(&totalConductors, &memberCount)
	avgConductorCount := 0.0
	if memberCount > 0 {
		avgConductorCount = float64(totalConductors) / float64(memberCount)
	}

	timelines := make(map[int]map[string]interface{})

	for _, member := range members {
		conductorDates := []string{}
		conductorRows, err := db.Query(`
			SELECT date FROM train_schedules 
			WHERE (conductor_id = ? OR (backup_id = ? AND conductor_showed_up = 0))
			AND date >= ?
			ORDER BY date ASC
		`, member.ID, member.ID, formatDateString(startDate))
		if err != nil {
			continue
		}
		for conductorRows.Next() {
			var dateStr string
			if err := conductorRows.Scan(&dateStr); err == nil {
				conductorDates = append(conductorDates, dateStr)
			}
		}
		conductorRows.Close()

		type PointEvent struct {
			Date   string
			Awards int
			Recs   int
		}
		eventMap := make(map[string]*PointEvent)

		awardRows, err := db.Query(`
			SELECT week_date, rank FROM awards 
			WHERE member_id = ? AND week_date >= ?
			ORDER BY week_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for awardRows.Next() {
				var weekDate string
				var rank int
				if err := awardRows.Scan(&weekDate, &rank); err == nil {
					points := 0
					switch rank {
					case 1:
						points = settings.AwardFirstPoints
					case 2:
						points = settings.AwardSecondPoints
					case 3:
						points = settings.AwardThirdPoints
					}
					if eventMap[weekDate] == nil {
						eventMap[weekDate] = &PointEvent{Date: weekDate}
					}
					eventMap[weekDate].Awards += points
				}
			}
			awardRows.Close()
		}

		recRows, err := db.Query(`
			SELECT DATE(created_at) as rec_date, COUNT(*) as count
			FROM recommendations 
			WHERE member_id = ? AND DATE(created_at) >= ?
			GROUP BY DATE(created_at)
			ORDER BY rec_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for recRows.Next() {
				var recDate string
				var count int
				if err := recRows.Scan(&recDate, &count); err == nil {
					points := int(5 * math.Sqrt(float64(count)))
					if eventMap[recDate] == nil {
						eventMap[recDate] = &PointEvent{Date: recDate}
					}
					eventMap[recDate].Recs += points
				}
			}
			recRows.Close()
		}

		var events []PointEvent
		for _, event := range eventMap {
			events = append(events, *event)
		}
		sort.Slice(events, func(i, j int) bool {
			return events[i].Date < events[j].Date
		})

		powerHistoryMap := make(map[string]int)
		powerRows, err := db.Query(`
			SELECT DATE(recorded_at) as power_date, 
			       MAX(power) as max_power
			FROM power_history 
			WHERE member_id = ? AND DATE(recorded_at) >= ?
			GROUP BY DATE(recorded_at)
			ORDER BY power_date ASC
		`, member.ID, formatDateString(startDate))
		if err == nil {
			for powerRows.Next() {
				var powerDate string
				var maxPower int
				if err := powerRows.Scan(&powerDate, &maxPower); err == nil {
					powerHistoryMap[powerDate] = maxPower
				}
			}
			powerRows.Close()
		}

		weekLabels := []string{}
		pointsWithReset := []int{}
		pointsCumulative := []int{}
		awardsWithReset := []int{}
		awardsCumulative := []int{}
		recsWithReset := []int{}
		recsCumulative := []int{}
		rankBoostWithReset := []int{}
		rankBoostCumulative := []int{}
		firstTimeBoostWithReset := []int{}
		firstTimeBoostCumulative := []int{}
		recentPenaltyWithReset := []int{}
		recentPenaltyCumulative := []int{}
		aboveAvgPenaltyWithReset := []int{}
		aboveAvgPenaltyCumulative := []int{}
		powerValues := []int{}

		currentPoints := 0
		cumulativePoints := 0
		currentAwards := 0
		cumulativeAwards := 0
		currentRecs := 0
		cumulativeRecs := 0
		currentRankBoost := 0
		cumulativeRankBoost := 0
		currentFirstTimeBoost := 0
		cumulativeFirstTimeBoost := 0
		currentRecentPenalty := 0
		cumulativeRecentPenalty := 0
		currentAboveAvgPenalty := 0
		cumulativeAboveAvgPenalty := 0
		conductorIdx := 0
		conductorCountSoFar := 0

		currentDate := getMondayOfWeek(startDate)
		for currentDate.Before(now) || currentDate.Equal(now) {
			weekStart := currentDate
			weekEnd := currentDate.AddDate(0, 0, 6)
			weekStartStr := formatDateString(weekStart)
			weekEndStr := formatDateString(weekEnd)

			weekLabel := fmt.Sprintf("%s - %s",
				weekStart.Format("Jan 2"),
				weekEnd.Format("Jan 2"))
			weekLabels = append(weekLabels, weekLabel)

			weekHasReset := false
			for conductorIdx < len(conductorDates) && conductorDates[conductorIdx] <= weekEndStr {
				if conductorDates[conductorIdx] >= weekStartStr {
					weekHasReset = true
					conductorCountSoFar++
				}
				conductorIdx++
			}

			weekAwards := 0
			weekRecs := 0
			for _, event := range events {
				if event.Date >= weekStartStr && event.Date <= weekEndStr {
					weekAwards += event.Awards
					weekRecs += event.Recs
				}
			}
			weekPoints := weekAwards + weekRecs

			currentPoints += weekPoints
			cumulativePoints += weekPoints
			currentAwards += weekAwards
			cumulativeAwards += weekAwards
			currentRecs += weekRecs
			cumulativeRecs += weekRecs

			weekRankBoost := 0
			if member.Rank == "R4" || member.Rank == "R5" {
				baseBoost := float64(settings.R4R5RankBoost)
				daysSinceLastDuty := 0
				for i := conductorIdx - 1; i >= 0; i-- {
					if i < len(conductorDates) {
						if lastConductorDate, err := time.Parse("2006-01-02", conductorDates[i]); err == nil {
							daysSinceLastDuty = int(weekEnd.Sub(lastConductorDate).Hours() / 24)
							break
						}
					}
				}
				multiplier := math.Pow(2, float64(daysSinceLastDuty)/7.0)
				weekRankBoost = int(math.Round(baseBoost * multiplier))
			}
			currentRankBoost += weekRankBoost
			cumulativeRankBoost += weekRankBoost

			weekFirstTimeBoost := 0
			if conductorCountSoFar == 0 {
				if currentAwards > 0 || currentRecs > 0 || currentRankBoost > 0 {
					weekFirstTimeBoost = settings.FirstTimeConductorBoost
				}
			}
			currentFirstTimeBoost += weekFirstTimeBoost
			cumulativeFirstTimeBoost += weekFirstTimeBoost

			weekRecentPenalty := 0
			if conductorCountSoFar > 0 {
				for i := conductorIdx - 1; i >= 0; i-- {
					if i < len(conductorDates) {
						if lastConductorDate, err := time.Parse("2006-01-02", conductorDates[i]); err == nil {
							daysSince := int(weekEnd.Sub(lastConductorDate).Hours() / 24)
							penalty := settings.RecentConductorPenaltyDays - daysSince
							if penalty > 0 {
								weekRecentPenalty = penalty
							}
							break
						}
					}
				}
			}
			currentRecentPenalty += weekRecentPenalty
			cumulativeRecentPenalty += weekRecentPenalty

			weekAboveAvgPenalty := 0
			if float64(conductorCountSoFar) > avgConductorCount {
				weekAboveAvgPenalty = settings.AboveAverageConductorPenalty
			}
			currentAboveAvgPenalty += weekAboveAvgPenalty
			cumulativeAboveAvgPenalty += weekAboveAvgPenalty

			if weekHasReset {
				currentPoints = 0
				currentAwards = 0
				currentRecs = 0
				currentRankBoost = 0
				currentFirstTimeBoost = 0
				currentRecentPenalty = 0
				currentAboveAvgPenalty = 0
			}

			weekMaxPower := 0
			for powerDate, power := range powerHistoryMap {
				if powerDate >= weekStartStr && powerDate <= weekEndStr {
					if power > weekMaxPower {
						weekMaxPower = power
					}
				}
			}

			pointsWithReset = append(pointsWithReset, currentPoints)
			pointsCumulative = append(pointsCumulative, cumulativePoints)
			awardsWithReset = append(awardsWithReset, currentAwards)
			awardsCumulative = append(awardsCumulative, cumulativeAwards)
			recsWithReset = append(recsWithReset, currentRecs)
			recsCumulative = append(recsCumulative, cumulativeRecs)
			rankBoostWithReset = append(rankBoostWithReset, currentRankBoost)
			rankBoostCumulative = append(rankBoostCumulative, cumulativeRankBoost)
			firstTimeBoostWithReset = append(firstTimeBoostWithReset, currentFirstTimeBoost)
			firstTimeBoostCumulative = append(firstTimeBoostCumulative, cumulativeFirstTimeBoost)
			recentPenaltyWithReset = append(recentPenaltyWithReset, currentRecentPenalty)
			recentPenaltyCumulative = append(recentPenaltyCumulative, cumulativeRecentPenalty)
			aboveAvgPenaltyWithReset = append(aboveAvgPenaltyWithReset, currentAboveAvgPenalty)
			aboveAvgPenaltyCumulative = append(aboveAvgPenaltyCumulative, cumulativeAboveAvgPenalty)
			powerValues = append(powerValues, weekMaxPower)

			currentDate = currentDate.AddDate(0, 0, 7)
		}

		conductorWeekLabels := []string{}
		for _, condDate := range conductorDates {
			if parsedDate, err := time.Parse("2006-01-02", condDate); err == nil {
				monday := getMondayOfWeek(parsedDate)
				weekEnd := monday.AddDate(0, 0, 6)
				weekLabel := fmt.Sprintf("%s - %s",
					monday.Format("Jan 2"),
					weekEnd.Format("Jan 2"))
				conductorWeekLabels = append(conductorWeekLabels, weekLabel)
			}
		}

		timelines[member.ID] = map[string]interface{}{
			"dates":                        weekLabels,
			"points_with_reset":            pointsWithReset,
			"points_cumulative":            pointsCumulative,
			"awards_with_reset":            awardsWithReset,
			"awards_cumulative":            awardsCumulative,
			"recommendations_with_reset":   recsWithReset,
			"recommendations_cumulative":   recsCumulative,
			"rank_boost_with_reset":        rankBoostWithReset,
			"rank_boost_cumulative":        rankBoostCumulative,
			"first_time_boost_with_reset":  firstTimeBoostWithReset,
			"first_time_boost_cumulative":  firstTimeBoostCumulative,
			"recent_penalty_with_reset":    recentPenaltyWithReset,
			"recent_penalty_cumulative":    recentPenaltyCumulative,
			"above_avg_penalty_with_reset": aboveAvgPenaltyWithReset,
			"above_avg_penalty_cumulative": aboveAvgPenaltyCumulative,
			"conductor_dates":              conductorWeekLabels,
			"power":                        powerValues,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(timelines)
}
