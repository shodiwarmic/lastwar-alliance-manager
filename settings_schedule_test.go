package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// putSettings drives updateSettings directly. The handler is normally wrapped by
// requirePermission in main.go; calling it here exercises the validation branch
// without a session, which is all these cases need.
func putSettings(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(string(b)))
	// updateSettings reads the actor for the admin-only password block, so a
	// request that gets past validation needs one. In production authMiddleware
	// guarantees it; here we inject the same value it would.
	req = req.WithContext(context.WithValue(req.Context(), authUserKey,
		&AuthUser{ID: 1, Username: "tester", IsAdmin: true}))
	rr := httptest.NewRecorder()
	updateSettings(rr, req)
	return rr
}

func setupSettingsTestDB(t *testing.T) {
	t.Helper()
	prev := db
	t.Setenv("DATABASE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("STORAGE_PATH", t.TempDir())
	t.Setenv("SESSION_KEY", "test-session-key-at-least-32-chars-long")
	if err := initDB(); err != nil {
		t.Fatalf("initDB: %v", err)
	}
	invalidateLastRankScheduleCache()
	t.Cleanup(func() {
		if db != nil {
			db.Close()
		}
		db = prev
		invalidateLastRankScheduleCache()
	})
}

// baseSettings carries the unrelated fields updateSettings validates, so these
// cases fail on the scheduler band rather than on something incidental.
func baseSettings() map[string]any {
	return map[string]any{
		"vs_flag_days_threshold": 2,
		"nap_size":               10,
		"nap_import_limit":       15,
	}
}

// The band depends on the interval, and the handler must compute it from the
// SUBMITTED interval rather than a constant.
func TestUpdateSettingsRejectsMaxAgeOutsideItsBand(t *testing.T) {
	setupSettingsTestDB(t)

	// 21h is legal at a 6h interval (band is 19–23).
	s := baseSettings()
	s["lastrank_auto_sync_interval_hours"] = 6
	s["lastrank_enrich_max_age_hours"] = 21
	if rr := putSettings(t, s); rr.Code != http.StatusOK {
		t.Errorf("6h/21h rejected with %d: %s", rr.Code, rr.Body.String())
	}

	// The SAME 21h is illegal at 3h (band is 22–23): every other tick would enrich.
	s = baseSettings()
	s["lastrank_auto_sync_interval_hours"] = 3
	s["lastrank_enrich_max_age_hours"] = 21
	rr := putSettings(t, s)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("3h/21h accepted with %d — the band is not being derived from the interval", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "3h interval") {
		t.Errorf("rejection message should name the interval so the operator can fix it; got %q", rr.Body.String())
	}

	// Too high: a long run's own duration could push a member past the next slot.
	s = baseSettings()
	s["lastrank_auto_sync_interval_hours"] = 6
	s["lastrank_enrich_max_age_hours"] = 24
	if rr := putSettings(t, s); rr.Code != http.StatusBadRequest {
		t.Errorf("24h max age accepted with %d, want 400", rr.Code)
	}
}

// An interval that doesn't divide the day makes slots drift across midnight.
func TestUpdateSettingsRejectsNonDivisorInterval(t *testing.T) {
	setupSettingsTestDB(t)
	s := baseSettings()
	s["lastrank_auto_sync_interval_hours"] = 5
	s["lastrank_enrich_max_age_hours"] = 21
	rr := putSettings(t, s)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("a 5h interval was accepted with %d, want 400", rr.Code)
	}
}

func TestUpdateSettingsRejectsBadAnchorHour(t *testing.T) {
	setupSettingsTestDB(t)
	s := baseSettings()
	s["lastrank_auto_sync_interval_hours"] = 6
	s["lastrank_enrich_max_age_hours"] = 21
	s["lastrank_auto_sync_hour"] = 24
	if rr := putSettings(t, s); rr.Code != http.StatusBadRequest {
		t.Errorf("anchor hour 24 accepted with %d, want 400", rr.Code)
	}
}

// A save must round-trip AND drop the scheduler's cached config, or a changed
// cadence would not take effect until the TTL expired.
func TestUpdateSettingsPersistsAndInvalidatesScheduleCache(t *testing.T) {
	setupSettingsTestDB(t)

	// Prime the cache with the default.
	if got := loadLastRankScheduleConfig().EnrichMaxAgeHours; got != 21 {
		t.Fatalf("primed max age = %d, want 21", got)
	}

	s := baseSettings()
	s["lastrank_auto_sync_enabled"] = true
	s["lastrank_auto_sync_hour"] = 3
	s["lastrank_auto_sync_interval_hours"] = 12
	s["lastrank_enrich_max_age_hours"] = 20
	s["prospect_auto_refresh_enabled"] = true
	if rr := putSettings(t, s); rr.Code != http.StatusOK {
		t.Fatalf("save failed with %d: %s", rr.Code, rr.Body.String())
	}

	cfg := loadLastRankScheduleConfig()
	if !cfg.Enabled || cfg.Hour != 3 || cfg.IntervalHours != 12 || cfg.EnrichMaxAgeHours != 20 || !cfg.ProspectEnabled {
		t.Errorf("config after save = %+v — the cache was not invalidated, or the columns are out of lockstep", cfg)
	}
}

// getSettings' SELECT and Scan lists are positional and ~45 entries long; nothing
// else catches a mismatch until the settings page 500s at runtime.
func TestGetSettingsScansEveryColumn(t *testing.T) {
	setupSettingsTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr := httptest.NewRecorder()
	getSettings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getSettings returned %d: %s — SELECT/Scan are likely out of lockstep", rr.Code, rr.Body.String())
	}
	var out Settings
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.LastRankAutoSyncIntervalHours != 6 || out.LastRankEnrichMaxAgeHours != 21 {
		t.Errorf("scheduler defaults did not survive the round trip: interval=%d age=%d",
			out.LastRankAutoSyncIntervalHours, out.LastRankEnrichMaxAgeHours)
	}
}
