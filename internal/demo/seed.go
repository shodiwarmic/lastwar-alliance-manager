// Package demo generates a complete, plausible, entirely fictional install — the
// database, the sample files, and a manifest — for the public demo (DEMO_MODE), the
// README screenshots, and browser tests.
//
// Real data supplies the shape, never the rows: the distributions in params.go are
// coarse aggregates, and everything else (names, the alliance, its files) is composed
// here from a fixed seed. The package never imports internal/app; the app imports it.
//
// It inserts through explicit column lists, so a migration that renames or drops a column
// fails the generator (and its test) instead of producing a stale database.
package demo

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"lastwar-alliance/internal/gametime"
)

// Options configures one seed.
type Options struct {
	// UploadsDir receives the sample files (the app's STORAGE_PATH).
	UploadsDir string
	// Today is the anchor: every date is computed relative to it. Zero means "now", the
	// demo's mode, which re-dates the alliance on every reset. A fixed date (the
	// screenshots' --today) freezes it, so a page retaken months later matches the set.
	Today time.Time
	// Password for all six accounts. Empty generates a random one (returned in Result).
	Password string
	// WorkerURL, when set, becomes settings.cv_worker_url with the OCR backend in local
	// mode — the demo points it at the fixtures service.
	WorkerURL string
}

// Result reports what a seed created.
type Result struct {
	Password string
	Accounts []Account
}

// ErrNotEmpty means the database already has accounts; Seed never writes over an install.
var ErrNotEmpty = errors.New("demo seed: the database already has users")

// SeedIfEmpty seeds a migrated database that has no users yet, and does nothing (false,
// nil) to one that has. It is the server's boot entry point in demo mode.
func SeedIfEmpty(db *sql.DB, o Options) (bool, Result, error) {
	n, err := userCount(db)
	if err != nil {
		return false, Result{}, err
	}
	if n > 0 {
		return false, Result{}, nil
	}
	res, err := Seed(db, o)
	return err == nil, res, err
}

func userCount(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// SeedFile creates (or reuses) a database file, applies the migrations in migrationsDir
// with goose, and seeds it. cmd/demo-seed's entry point; the server never calls it — at
// boot it already holds the one connection and has migrated, so it calls Seed.
func SeedFile(path, migrationsDir string, o Options) (Result, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return Result{}, err
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return Result{}, err
	}
	goose.SetDialect("sqlite3")
	if err := goose.Up(conn, migrationsDir); err != nil {
		return Result{}, fmt.Errorf("migrate: %w", err)
	}
	return Seed(conn, o)
}

// Seed writes the alliance into a migrated database with no users. It runs no migrations
// and opens nothing: the caller's handle is used, so the server never has a second pool
// on its file.
func Seed(db *sql.DB, o Options) (Result, error) {
	if n, err := userCount(db); err != nil {
		return Result{}, err
	} else if n > 0 {
		return Result{}, ErrNotEmpty
	}
	if o.UploadsDir == "" {
		return Result{}, errors.New("demo seed: UploadsDir is required")
	}
	if err := os.MkdirAll(o.UploadsDir, 0755); err != nil {
		return Result{}, err
	}
	if o.Password == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return Result{}, err
		}
		o.Password = hex.EncodeToString(b)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(o.Password), bcrypt.DefaultCost)
	if err != nil {
		return Result{}, err
	}

	s := newSeeder(db, o, string(hash))
	for _, area := range []struct {
		name string
		fn   func(*seeder)
	}{
		{"settings", (*seeder).settings},
		{"members", (*seeder).members},
		{"users", (*seeder).users},
		{"aliases and skills", (*seeder).aliases},
		{"history", (*seeder).history},
		{"vs points", (*seeder).vsPoints},
		{"vs league", (*seeder).vsLeague},
		{"seasons", (*seeder).seasons},
		{"schedule", (*seeder).schedule},
		{"participation", (*seeder).participation},
		{"accountability", (*seeder).strikes},
		{"train", (*seeder).train},
		{"storm", (*seeder).storm},
		{"recruiting", (*seeder).prospects},
		{"allies", (*seeder).allies},
		{"polls", (*seeder).polls},
		{"dyno", (*seeder).dyno},
		{"officer command", (*seeder).officerCommand},
		{"files", (*seeder).files},
		{"lastrank queue", (*seeder).lastRankQueue},
		{"activity", (*seeder).activity},
	} {
		if err := s.inTx(area.fn); err != nil {
			return Result{}, fmt.Errorf("demo seed: %s: %w", area.name, err)
		}
	}
	return Result{Password: o.Password, Accounts: s.ds.accounts()}, nil
}

// seeder carries one seed's state. Every area runs in its own transaction on tx; the
// first failing statement is kept in err and the rest of that area is skipped.
type seeder struct {
	db   *sql.DB
	tx   *sql.Tx
	err  error
	o    Options
	ds   *dataset
	hash string
	// ref is the reference instant every timestamp is computed back from; today is its
	// game date. Live mode: now. Frozen mode: the anchor date at 20:00 game time.
	ref   time.Time
	today time.Time // midnight UTC of the game date, for date arithmetic
	// filled as areas run, for the ones after
	scheduleIDs map[string][]scheduledEvent
}

type scheduledEvent struct {
	ID        int64
	Date      string
	DaysAgo   int
	TaskForce string
}

func newSeeder(db *sql.DB, o Options, hash string) *seeder {
	s := &seeder{db: db, o: o, ds: build(), hash: hash, scheduleIDs: map[string][]scheduledEvent{}}
	if o.Today.IsZero() {
		s.ref = time.Now().UTC().Truncate(time.Second)
	} else {
		y, m, d := o.Today.Date()
		s.ref = time.Date(y, m, d, 20, 0, 0, 0, gametime.Loc).UTC()
	}
	t, _ := time.Parse(gametime.DateLayout, gametime.DateOf(s.ref))
	s.today = t
	return s
}

func (s *seeder) inTx(fn func(*seeder)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	s.tx, s.err = tx, nil
	fn(s)
	if s.err != nil {
		tx.Rollback()
		return s.err
	}
	return tx.Commit()
}

// exec runs one statement on the area's transaction and returns its last insert id.
func (s *seeder) exec(q string, args ...any) int64 {
	if s.err != nil {
		return 0
	}
	res, err := s.tx.Exec(q, args...)
	if err != nil {
		s.err = fmt.Errorf("%w\n  in: %.120s", err, q)
		return 0
	}
	id, _ := res.LastInsertId()
	return id
}

// queryInt reads one integer on the area's transaction.
func (s *seeder) queryInt(q string, args ...any) int64 {
	if s.err != nil {
		return 0
	}
	var v int64
	if err := s.tx.QueryRow(q, args...).Scan(&v); err != nil {
		s.err = fmt.Errorf("%w\n  in: %.120s", err, q)
	}
	return v
}

// ts is the SQLite timestamp `days` before the reference instant (fractions allowed).
func (s *seeder) ts(days float64) string {
	return s.ref.Add(-time.Duration(days * float64(24*time.Hour))).Format(gametime.SQLiteLayout)
}

// date is the game date `days` before the anchor (negative = after).
func (s *seeder) date(days int) string {
	return s.today.AddDate(0, 0, -days).Format(gametime.DateLayout)
}

// todayWeekday is the anchor's weekday, Monday = 0 … Sunday = 6.
func (s *seeder) todayWeekday() int { return (int(s.today.Weekday()) + 6) % 7 }

// monday is the VS week Monday `weeksAgo` weeks before the anchor's week.
func (s *seeder) monday(weeksAgo int) string {
	return gametime.MondayOf(s.today).AddDate(0, 0, -7*weeksAgo).Format(gametime.DateLayout)
}

// adminID and the per-rank account ids, as accounts() numbers them.
const (
	adminUser = 1
	r5User    = 2
	r4User    = 3
	r3User    = 4
)
