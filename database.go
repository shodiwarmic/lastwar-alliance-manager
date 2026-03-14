package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func initDB() error {
	var err error

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "./alliance.db"
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	// WAL mode for concurrency
	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		log.Printf("Warning: Failed to enable WAL mode: %v", err)
	}
	db.SetMaxOpenConns(1)

	// Run Goose Migrations
	goose.SetDialect("sqlite3")
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run database migrations: %v", err)
	}

	// Ensure physical file directory exists
	os.MkdirAll(getStoragePath(), 0755)

	return nil
}

func loadSettings() (Settings, error) {
	var settings Settings
	err := db.QueryRow(`SELECT id, award_first_points, award_second_points, award_third_points, 
		recommendation_points, recent_conductor_penalty_days, above_average_conductor_penalty, r4r5_rank_boost,
		first_time_conductor_boost, schedule_message_template, daily_message_template 
		FROM settings WHERE id = 1`).Scan(
		&settings.ID,
		&settings.AwardFirstPoints,
		&settings.AwardSecondPoints,
		&settings.AwardThirdPoints,
		&settings.RecommendationPoints,
		&settings.RecentConductorPenaltyDays,
		&settings.AboveAverageConductorPenalty,
		&settings.R4R5RankBoost,
		&settings.FirstTimeConductorBoost,
		&settings.ScheduleMessageTemplate,
		&settings.DailyMessageTemplate,
	)
	return settings, err
}
