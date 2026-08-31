package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
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
		slog.Warn("WAL mode not enabled; performance may be degraded", "journal_mode", journalMode)
	}
	db.SetMaxOpenConns(1)

	// Run Goose Migrations
	goose.SetDialect("sqlite3")
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run database migrations: %v", err)
	}

	// Ensure a default admin account exists on first startup.
	var adminCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'admin'`).Scan(&adminCount); err != nil {
		return fmt.Errorf("failed to check for default admin user: %w", err)
	}

	if adminCount == 0 {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash default admin password: %w", err)
		}

		if _, err := db.Exec(`
			INSERT INTO users (username, password, is_admin, force_password_change)
			VALUES (?, ?, 1, 1)
		`, "admin", string(hashedPassword)); err != nil {
			return fmt.Errorf("failed to create default admin user: %w", err)
		}

		slog.Info("Created default admin account")
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
