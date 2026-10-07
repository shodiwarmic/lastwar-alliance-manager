// demo-seed writes the fictional demo alliance (internal/demo) into a fresh database:
//
//	go run ./cmd/demo-seed --db /tmp/demo.db --uploads /tmp/up --today 2026-10-05
//
// Run it from the repository root, like the server: the migrations are read from
// ./migrations. It refuses a database that already has users.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lastwar-alliance/internal/demo"
	"lastwar-alliance/internal/gametime"
)

func main() {
	dbPath := flag.String("db", "", "database file to create (required)")
	uploads := flag.String("uploads", "", "directory for the sample files — the server's STORAGE_PATH (required)")
	today := flag.String("today", "", "freeze the anchor date, YYYY-MM-DD (default: today's game date)")
	password := flag.String("password", "", "password for all six accounts (default: random, printed)")
	manifest := flag.String("manifest", "", "where to write manifest.json (default: beside the database)")
	worker := flag.String("worker-url", "", "OCR service URL to store, e.g. the fixtures service (sets local OCR mode)")
	flag.Parse()
	if *dbPath == "" || *uploads == "" {
		flag.Usage()
		os.Exit(2)
	}

	o := demo.Options{UploadsDir: *uploads, Password: *password, WorkerURL: *worker}
	if *today != "" {
		t, err := time.ParseInLocation(gametime.DateLayout, *today, gametime.Loc)
		if err != nil {
			fmt.Fprintln(os.Stderr, "--today must be YYYY-MM-DD:", err)
			os.Exit(2)
		}
		o.Today = t
	}
	res, err := demo.SeedFile(*dbPath, "migrations", o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mpath := *manifest
	if mpath == "" {
		mpath = filepath.Join(filepath.Dir(*dbPath), "manifest.json")
	}
	b, _ := json.MarshalIndent(demo.Manifest(), "", "  ")
	if err := os.WriteFile(mpath, b, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "write manifest:", err)
		os.Exit(1)
	}

	fmt.Printf("Seeded %s (%s alliance [%s], server %d)\n", *dbPath, demo.AllianceName, demo.AllianceTag, demo.ServerID)
	fmt.Printf("Manifest: %s\n", mpath)
	fmt.Println("Accounts (all share one password):")
	for _, a := range res.Accounts {
		who := a.Rank
		if a.Member != "" {
			who += ", " + a.Member
		}
		fmt.Printf("  %-11s %s\n", a.Username, who)
	}
	fmt.Printf("Password: %s\n", res.Password)
}
