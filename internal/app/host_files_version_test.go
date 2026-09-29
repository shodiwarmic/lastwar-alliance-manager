package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Admin page shows the host files' release beside the image's, from HOST_FILES_VERSION —
// the marker scripts/manage.sh writes to .env and compose passes into the container.
func TestPageDataCarriesTheHostFilesVersion(t *testing.T) {
	setupNameMatchTestDB(t) // getPageData also reads settings
	if store == nil {
		initSessionStore()
	}
	t.Setenv("HOST_FILES_VERSION", "v2.0.0")
	data := getPageData(httptest.NewRequest(http.MethodGet, "/admin", nil), "Admin", "admin")
	if data.HostFilesVersion != "v2.0.0" {
		t.Errorf("HostFilesVersion = %q, want v2.0.0", data.HostFilesVersion)
	}

	t.Setenv("HOST_FILES_VERSION", "")
	data = getPageData(httptest.NewRequest(http.MethodGet, "/admin", nil), "Admin", "admin")
	if data.HostFilesVersion != "" {
		t.Errorf("an unmigrated install reports HostFilesVersion %q", data.HostFilesVersion)
	}
}
