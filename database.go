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

	// WAL mode for concurrency — QueryRow lets us verify the mode was actually applied.
	var journalMode string
	if err = db.QueryRow("PRAGMA journal_mode=WAL;").Scan(&journalMode); err != nil {
		return fmt.Errorf("failed to configure WAL mode: %w", err)
	}
	if journalMode != "wal" {
		log.Printf("Warning: WAL mode not enabled (got %q); performance may be degraded", journalMode)
	}
	db.SetMaxOpenConns(1)

	// Run Goose Migrations
	goose.SetDialect("sqlite3")
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run database migrations: %v", err)
	}

	// Add is_sub to storm_assignments if missing
	var colExists int
	db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('storm_assignments') WHERE name='is_sub'`).Scan(&colExists)
	if colExists == 0 {
		db.Exec(`ALTER TABLE storm_assignments ADD COLUMN is_sub INTEGER NOT NULL DEFAULT 0`)
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
