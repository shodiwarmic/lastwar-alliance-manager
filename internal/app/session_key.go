package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// sessionKeyMaterial is SESSION_KEY resolved once at boot, in the two forms its consumers
// have always used. Before it existed the variable had three readers that each parsed it
// their own way (the cookie store hex-decoded or padded it, the CSRF block sliced it raw,
// the token code took the string), and the length check ran only after the session store,
// migrations and the scheduler had already started.
type sessionKeyMaterial struct {
	// raw is the variable verbatim. The mobile and WOPI JWTs have always been signed with
	// this string, not with the decoded bytes: install.sh writes a 64-character hex key,
	// and switching the token secret to its 32-byte decoding would invalidate every
	// outstanding scanner and collector token and every open Collabora session on upgrade.
	raw string
	// store is the 32-byte cookie-store key: the hex decoding of a 64-character hex key,
	// otherwise the raw bytes (truncated to 32).
	store []byte
}

// sessionKeys is written by loadSessionKey before any request can be served and is
// read-only afterwards.
var sessionKeys sessionKeyMaterial

var errSessionKeyMissing = errors.New("SESSION_KEY is not set; PRODUCTION=true refuses to start with an ephemeral key, which would log every user out on each restart")

// resolveSessionKey turns the raw variable into key material. In production an unset key
// is refused; in development an ephemeral one is generated (sessions and tokens then last
// until the next restart). A key shorter than MinSessionKeyLen is refused everywhere, as
// it always has been.
func resolveSessionKey(raw string, production bool) (sessionKeyMaterial, error) {
	if raw == "" {
		if production {
			return sessionKeyMaterial{}, errSessionKeyMissing
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return sessionKeyMaterial{}, fmt.Errorf("generate ephemeral session key: %w", err)
		}
		raw = hex.EncodeToString(b)
		slog.Warn("No SESSION_KEY environment variable set; using generated key (not persistent across restarts)")
	} else if len(raw) < MinSessionKeyLen {
		return sessionKeyMaterial{}, fmt.Errorf("SESSION_KEY must be at least %d characters (got %d)", MinSessionKeyLen, len(raw))
	}

	key, err := hex.DecodeString(raw)
	if err != nil || len(key) != 32 {
		key = []byte(raw)
	}
	return sessionKeyMaterial{raw: raw, store: key[:32]}, nil
}

// loadSessionKey resolves SESSION_KEY into sessionKeys. Main calls it first, before
// anything opens the database; tests reach it through initSessionStore.
func loadSessionKey() error {
	k, err := resolveSessionKey(os.Getenv("SESSION_KEY"), isProduction())
	if err != nil {
		return err
	}
	sessionKeys = k
	return nil
}

// tokenSecret is the HMAC secret for the mobile and WOPI JWTs — the raw SESSION_KEY
// string, byte-for-byte what it has always been.
func tokenSecret() []byte { return []byte(sessionKeys.raw) }
