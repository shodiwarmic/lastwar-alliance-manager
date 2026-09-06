package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain runs the whole package's tests from the repository root — the same working
// directory `go run ./cmd/server` has, and the same one the Dockerfile gives the binary.
//
// Every DB-backed test resolves `migrations/` against the cwd: initDB calls
// goose.Up(db.DB, "migrations"), all nine setup*TestDB helpers call initDB, and
// season_reward_tiers_test.go calls goose.UpTo(conn, "migrations", …) directly. Templates
// (parseTemplates) and static assets (buildAssetHashes) resolve the same way. `go test`
// sets the cwd to the package directory, which since the move to internal/app is three
// levels below all of those.
//
// Chdir rather than absolute paths: the alternative means changing goose.Up,
// parseTemplates and staticDir to take a root, i.e. a runtime change in a PR whose whole
// point is that it makes none. It is safe here because it happens once, before any test
// runs — every test then sees a stable cwd, no test calls t.Parallel, and the package's
// init() functions only call registerJobKind, so nothing touches the filesystem earlier.
func TestMain(m *testing.M) {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: chdir:", err)
		os.Exit(1)
	}
	// Assert loudly rather than let the helper quietly supply the wrong directory: a
	// missing migrations/ would otherwise surface as an unrelated schema failure in
	// every DB-backed test.
	for _, want := range []string{"go.mod", "migrations", "templates", "static"} {
		if _, err := os.Stat(want); err != nil {
			fmt.Fprintf(os.Stderr, "TestMain: %s not found in %s — tests must run from the repository root\n", want, root)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above the working directory")
		}
		dir = parent
	}
}
