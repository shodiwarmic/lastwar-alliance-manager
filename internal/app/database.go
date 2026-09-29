package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

func initDB() error {
	// No account is seeded here any more: an empty users table is first-run, and
	// initSetupState (setup.go) issues a setup key for it instead of a known password.
	conn, err := sql.Open("sqlite", databasePath())
	if err != nil {
		return err
	}
	// Every db.-level call goes through the statement ceiling from here on; see
	// dbguard.go. Outside production the ceiling is halved — a self-deadlock should
	// surface quickly in development, where nothing is waiting on a real user.
	if !isProduction() {
		dbAcquireCeiling = 5 * time.Second
	}
	db = &guardedDB{DB: conn}

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
	// Migrations run on the raw handle, before any request-serving goroutine exists:
	// a schema change legitimately outlasts the statement ceiling.
	if err := goose.Up(db.DB, "migrations"); err != nil {
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
