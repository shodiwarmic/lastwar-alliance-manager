// dbguard.go - Wraps the single-connection *sql.DB so that a self-deadlock at the
// pool fails loudly instead of hanging the whole process.
//
// database.go sets db.SetMaxOpenConns(1). Issuing a second db.-level statement while
// a rows cursor (or a transaction) is still open on the same goroutine waits forever
// for a connection that will never be freed: no error, no log, no panic — the process
// just stops serving. See the "One DB connection" gotcha in CLAUDE.md.
//
// The guard puts a statement ceiling on every call that enters the pool. The context
// handed to QueryContext/ExecContext/BeginTx governs both the wait for a free pooled
// connection and the execution of that one statement — the timer is armed for the
// duration of the call and disarmed the moment it returns, so a long-lived cursor or
// transaction is never killed by it. A call that blocks past the ceiling returns an
// error wrapping errDBAcquireTimeout, and the handler's deferred rows.Close() /
// tx.Rollback() then frees the connection: one 500 and a stack trace instead of a
// dead server.

package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

// guardedDB is a *sql.DB with a statement ceiling on the calls that enter the pool.
// Every other method (Prepare, Conn, Ping, Close, SetMaxOpenConns, …) is an embedded
// pass-through, and the return types of the wrapped methods are unchanged, so the
// 600-odd existing call sites — and the rowQuerier / historyQuerier /
// mobileRosterQuerier interfaces that *sql.Tx also satisfies — compile as they are.
type guardedDB struct{ *sql.DB }

// dbAcquireCeiling bounds a single db.-level statement: the wait for the one pooled
// connection plus the execution of the statement itself. It has to clear the longest
// legitimate holder of that connection — every transaction in this app is sub-second
// (imports, commits, archive, prune), as is every single statement against a SQLite
// database measured in megabytes. A statement that genuinely runs this long is itself
// a defect, and failing it with a stack trace beats hanging on it.
//
// Lowered to 5s outside production by initDB; tests shrink it further.
var dbAcquireCeiling = 10 * time.Second

// errDBAcquireTimeout is what every ceiling breach wraps.
var errDBAcquireTimeout = errors.New("db: statement ceiling exceeded")

// armed returns a context the ceiling can cancel and a disarm func reporting whether
// it DID. The timer callback records `fired` BEFORE cancelling, so the flag — not
// timer.Stop()'s return value, which is also false when the callback merely raced a
// normal return — is the causal predicate. A cancellation arriving from the parent
// leaves fired false and is passed through untouched.
func (g *guardedDB) armed(parent context.Context) (context.Context, func() bool) {
	var fired atomic.Bool
	ctx, cancel := context.WithCancel(parent)
	t := time.AfterFunc(dbAcquireCeiling, func() {
		fired.Store(true)
		cancel()
	})
	return ctx, func() bool {
		t.Stop()
		return fired.Load()
	}
}

// timedOut logs the breach once and returns the error the caller sees. The goroutine
// holding the cursor or transaction is the goroutine that is blocked, so its stack
// names both ends of the deadlock.
func (g *guardedDB) timedOut(query string, cause error) error {
	slog.Error("database call exceeded the statement ceiling — likely a query issued while a cursor or transaction is still open",
		"ceiling", dbAcquireCeiling,
		"query", firstSQLLine(query),
		"caller", callSite(),
		"cause", cause,
		"stack", string(debug.Stack()),
	)
	return fmt.Errorf("%w after %s: %s", errDBAcquireTimeout, dbAcquireCeiling, firstSQLLine(query))
}

// callSite names the first frame outside this file — the guard's own wrappers call
// each other, so a fixed runtime.Caller depth would always report dbguard.go.
func callSite() string {
	for skip := 2; skip < 12; skip++ {
		_, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		if strings.HasSuffix(file, "dbguard.go") {
			continue
		}
		return fmt.Sprintf("%s:%d", file, line)
	}
	return "unknown"
}

// firstSQLLine keeps the log line readable for the many multi-line queries here.
func firstSQLLine(query string) string {
	for _, line := range strings.Split(query, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return strings.TrimSpace(query)
}

func (g *guardedDB) Query(query string, args ...any) (*sql.Rows, error) {
	return g.QueryContext(context.Background(), query, args...)
}

func (g *guardedDB) QueryContext(parent context.Context, query string, args ...any) (*sql.Rows, error) {
	ctx, disarm := g.armed(parent)
	rows, err := g.DB.QueryContext(ctx, query, args...)
	if disarm() {
		// The ceiling fired during the call. Anything handed back is tethered to a
		// cancelled context — database/sql's awaitDone would close it under the
		// caller — so close it here and report the ceiling. The call did take the
		// full ceiling, which makes this the correct verdict either way.
		if rows != nil {
			rows.Close()
		}
		return nil, g.timedOut(query, err)
	}
	return rows, err
}

// QueryRow cannot fabricate a *sql.Row carrying our error: sql.Row has no public
// constructor, and the *sql.Row return type is what lets *sql.Tx satisfy the same
// rowQuerier / historyQuerier interfaces. So when the ceiling fires, the returned
// Row's Scan reports context.Canceled — never sql.ErrNoRows, so the caller takes its
// ordinary unexpected-error path — and the log line written at the moment the ceiling
// fires is the diagnostic.
func (g *guardedDB) QueryRow(query string, args ...any) *sql.Row {
	return g.QueryRowContext(context.Background(), query, args...)
}

func (g *guardedDB) QueryRowContext(parent context.Context, query string, args ...any) *sql.Row {
	ctx, disarm := g.armed(parent)
	row := g.DB.QueryRowContext(ctx, query, args...)
	if disarm() {
		g.timedOut(query, ctx.Err())
	}
	return row
}

func (g *guardedDB) Exec(query string, args ...any) (sql.Result, error) {
	return g.ExecContext(context.Background(), query, args...)
}

func (g *guardedDB) ExecContext(parent context.Context, query string, args ...any) (sql.Result, error) {
	ctx, disarm := g.armed(parent)
	res, err := g.DB.ExecContext(ctx, query, args...)
	if disarm() {
		return nil, g.timedOut(query, err)
	}
	return res, err
}

func (g *guardedDB) Begin() (*sql.Tx, error) {
	return g.BeginTx(context.Background(), nil)
}

func (g *guardedDB) BeginTx(parent context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	ctx, disarm := g.armed(parent)
	tx, err := g.DB.BeginTx(ctx, opts)
	if disarm() {
		if tx != nil {
			tx.Rollback()
		}
		return nil, g.timedOut("BEGIN", err)
	}
	return tx, err
}
