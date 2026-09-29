// setup.go — first-run setup: the app ships with no account, and the first administrator is
// created by whoever can read the setup key from the install's data directory.
//
// It replaced a seeded admin/admin123 whose password README published: an instance exposed
// before its first login was exposed with a known credential. Reading data/setup-key needs a
// shell on the host, which is the proof of ownership a published password could never be.

package app

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/gorilla/csrf"
	"golang.org/x/crypto/bcrypt"
)

// Identity field limits, matching the Settings page's inputs.
const (
	setupAllianceNameMax = 60
	setupAllianceTagMax  = 10
)

// setupKeyTTL bounds how long a key printed at install time stays redeemable. An install
// nobody claims in a day gets a fresh key on its next restart, which is also how an
// operator regenerates one on purpose: delete the file and restart.
const setupKeyTTL = 24 * time.Hour

// installUnclaimed is set while the users table is empty. Only an empty users table is
// unambiguously first-run: an install whose last admin was deactivated or renamed is NOT
// first-run, and recovering it is deliberately manual (see the last-admin guards in
// handlers_admin.go). It is read on every request, so it is an atomic rather than a query.
var installUnclaimed atomic.Bool

// databasePath resolves DATABASE_PATH and its fallback in one place, so the setup key can
// never land in one directory while the database lands in another.
func databasePath() string {
	if p := os.Getenv("DATABASE_PATH"); p != "" {
		return p
	}
	return "./alliance.db"
}

// setupKeyPath is data/setup-key on a Docker install (DATABASE_PATH=/app/data/alliance.db,
// bind-mounted from the install directory's ./data).
func setupKeyPath() string {
	return filepath.Join(filepath.Dir(databasePath()), "setup-key")
}

// initSetupState decides at boot whether this install is unclaimed and, if it is, makes sure
// a live setup key exists. The key's VALUE is never logged: under Docker the log is kept by
// `docker logs` for the life of the container, readable by anyone who can read the logs.
func initSetupState() error {
	var users int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return err
	}
	path := setupKeyPath()

	if users > 0 {
		installUnclaimed.Store(false)
		// A backup restored over a freshly set-up data/ directory can leave a key behind
		// that no longer guards anything.
		if err := os.Remove(path); err == nil {
			slog.Info("Removed a leftover setup key: this install already has accounts", "path", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("Could not remove a leftover setup key", "path", path, "error", err)
		}
		return nil
	}

	installUnclaimed.Store(true)
	if err := ensureSetupKey(path); err != nil {
		return err
	}
	slog.Warn("Setup required: no accounts exist. Open /setup and enter the setup key from this file "+
		"(on a Docker install, data/setup-key in the install directory — read it with sudo cat)",
		"path", path)
	return nil
}

// ensureSetupKey keeps a key younger than setupKeyTTL — so a restart does not invalidate the
// key install.sh just printed — and otherwise writes a new one, mode 0600.
func ensureSetupKey(path string) error {
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < setupKeyTTL {
		return nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	// Written beside the target and renamed over it, so a reader never sees a half-written key.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".setup-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(hex.EncodeToString(buf) + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// setupGate sends every request to /setup while the install is unclaimed. Static files stay
// reachable (the setup page needs its CSS and JS, and they are served unauthenticated
// anyway), and the mobile API gets a JSON error its client can show rather than an HTML
// redirect. Once claimed it costs one atomic load.
func setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !installUnclaimed.Load() {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		switch {
		case p == "/setup" || p == "/api/setup":
			next.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/mobile/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "setup required"})
		default:
			if _, _, ok := staticFileFor(p); ok {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/setup", http.StatusFound)
		}
	})
}

type setupPageData struct {
	CSRFToken         template.HTML
	PwdMinLength      int
	PwdRequireUpper   bool
	PwdRequireLower   bool
	PwdRequireNumber  bool
	PwdRequireSpecial bool
}

// GET /setup — the first-run page (unauthenticated). Once claimed there is nothing to set up.
func showSetupPage(w http.ResponseWriter, r *http.Request) {
	if !installUnclaimed.Load() {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	data := setupPageData{CSRFToken: csrf.TemplateField(r)}
	db.QueryRow(`SELECT pwd_min_length, pwd_require_upper, pwd_require_lower, pwd_require_number, pwd_require_special
		FROM settings WHERE id = 1`).Scan(
		&data.PwdMinLength, &data.PwdRequireUpper, &data.PwdRequireLower, &data.PwdRequireNumber, &data.PwdRequireSpecial,
	)

	noStoreHTML(w)
	t, err := parseTemplates("templates/setup.html")
	if err != nil {
		slog.Error("failed to parse setup template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := t.Execute(w, data); err != nil {
		slog.Error("failed to execute setup template", "error", err)
	}
}

// POST /api/setup — redeem the setup key and create the first administrator.
func claimSetup(w http.ResponseWriter, r *http.Request) {
	// The key is 256 bits, so this is not what stops a guess — it is what keeps the
	// endpoint from being a free CPU sink (bcrypt below) for anyone who finds it.
	if !getLoginLimiter(getClientIP(r)).Allow() {
		slog.Warn("setup claim rate limit exceeded", "ip", getClientIP(r))
		http.Error(w, "Too many attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}
	if !installUnclaimed.Load() {
		http.Error(w, "This install is already set up.", http.StatusConflict)
		return
	}

	var req struct {
		SetupKey        string `json:"setup_key"`
		Username        string `json:"username"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
		AllianceName    string `json:"alliance_name"`
		AllianceTag     string `json:"alliance_tag"`
		ServerNumber    int    `json:"server_number"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	keyPath := setupKeyPath()
	info, err := os.Stat(keyPath)
	if err != nil {
		http.Error(w, "No setup key is available. Restart the app to issue one.", http.StatusGone)
		return
	}
	if time.Since(info.ModTime()) >= setupKeyTTL {
		http.Error(w, "The setup key has expired. Restart the app to issue a new one.", http.StatusGone)
		return
	}
	want, err := os.ReadFile(keyPath)
	if err != nil {
		slog.Error("failed to read setup key", "path", keyPath, "error", err)
		http.Error(w, "Could not read the setup key.", http.StatusInternalServerError)
		return
	}
	// Trimmed on both sides: the file ends in a newline, and a pasted key often carries one.
	want = bytes.TrimSpace(want)
	got := bytes.TrimSpace([]byte(req.SetupKey))
	if len(want) == 0 || subtle.ConstantTimeCompare(want, got) != 1 {
		http.Error(w, "The setup key is incorrect.", http.StatusUnauthorized)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if !usernameRe.MatchString(req.Username) {
		http.Error(w, "Username must be 3–30 characters: letters, numbers, . _ - only", http.StatusBadRequest)
		return
	}
	if req.Password != req.ConfirmPassword {
		http.Error(w, "Passwords do not match", http.StatusBadRequest)
		return
	}
	if err := validatePasswordPolicy(req.Password, 0); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// The alliance's identity, captured here so a new install does not start life as a
	// blank "Alliance". The tag matters beyond display: it is half of the Rule 2 test
	// (isOwnAlliance) that keeps our own alliance out of the external registry, so an
	// install that never set it could file itself as a VS opponent. The server number
	// stays optional — it only powers the NAP tab and search defaults.
	req.AllianceName = strings.TrimSpace(req.AllianceName)
	req.AllianceTag = strings.TrimSpace(req.AllianceTag)
	if req.AllianceName == "" || utf8.RuneCountInString(req.AllianceName) > setupAllianceNameMax {
		http.Error(w, fmt.Sprintf("Alliance name is required (up to %d characters)", setupAllianceNameMax), http.StatusBadRequest)
		return
	}
	if req.AllianceTag == "" || utf8.RuneCountInString(req.AllianceTag) > setupAllianceTagMax {
		http.Error(w, fmt.Sprintf("Alliance tag is required (up to %d characters)", setupAllianceTagMax), http.StatusBadRequest)
		return
	}
	if req.ServerNumber < 0 {
		http.Error(w, "Server number must be a positive number, or left blank", http.StatusBadRequest)
		return
	}

	// Hashed before the transaction: bcrypt takes long enough that holding the single
	// connection across it would stall every other request for the duration.
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("failed to hash password during setup", "error", err)
		http.Error(w, "Failed to process password", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		slog.Error("failed to begin setup transaction", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// The flag above is a fast path; this count, inside the transaction, is what makes the
	// key single-use when two claims race.
	var users int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		slog.Error("failed to count users during setup", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if users > 0 {
		http.Error(w, "This install is already set up.", http.StatusConflict)
		return
	}

	// is_active defaults to 1 (migration 061), but it is written here anyway: this is the
	// one account nobody can reactivate from the UI.
	res, err := tx.Exec(`INSERT INTO users (username, password, is_admin, is_active, force_password_change)
		VALUES (?, ?, 1, 1, 0)`, req.Username, string(hashed))
	if err != nil {
		slog.Error("failed to insert the initial administrator", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	userID, err := res.LastInsertId()
	if err != nil {
		slog.Error("failed to read the initial administrator's id", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if _, err := tx.Exec(`INSERT INTO password_history (user_id, password_hash) VALUES (?, ?)`, userID, string(hashed)); err != nil {
		slog.Error("failed to insert password history during setup", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	// The three identity columns only — not updateSettings' positional lists, which carry
	// every other setting and would need values this page has no business supplying.
	if _, err := tx.Exec(`UPDATE settings SET alliance_name = ?, alliance_tag = ?, our_server_id = NULLIF(?, 0) WHERE id = 1`,
		req.AllianceName, req.AllianceTag, req.ServerNumber); err != nil {
		slog.Error("failed to save alliance identity during setup", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit setup", "error", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	installUnclaimed.Store(false)
	// The gate is already open, and initSetupState removes a leftover key on the next boot,
	// so a failed delete is logged rather than failing a claim that has already succeeded.
	if err := os.Remove(keyPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("Could not delete the redeemed setup key", "path", keyPath, "error", err)
	}

	logActivity(int(userID), req.Username, "created", "user", req.Username, true, "initial administrator via setup key")
	identity := []string{"alliance_name: → " + req.AllianceName, "alliance_tag: → " + req.AllianceTag}
	if req.ServerNumber > 0 {
		identity = append(identity, fmt.Sprintf("our_server_id: → %d", req.ServerNumber))
	}
	logActivity(int(userID), req.Username, "updated", "settings", "alliance identity", true, strings.Join(identity, "; "))
	trackLogin(int(userID), req.Username, r, true)

	session, _ := store.Get(r, "session")
	session.Values["authenticated"] = true
	session.Values["username"] = req.Username
	session.Values["user_id"] = int(userID)
	session.Values["is_admin"] = true
	session.Save(r, w)

	// Settings next: game limits (HQ cap, member cap, VS minimum, the event level ceilings)
	// are worth setting before the first import, and the page says so.
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"redirect": "/settings"})
}
