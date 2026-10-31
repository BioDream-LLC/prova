package authweb

import (
	"net/http/httptest"
	"testing"

	"github.com/biodream-llc/prova/internal/auth"
)

// TestReadOnlyPOSTs: a viewer may use the endpoints that only compute; every other POST needs an
// operator, and accounts need an admin.
func TestReadOnlyPOSTs(t *testing.T) {
	for path, want := range map[string]auth.Role{
		"/api/validate": auth.Viewer, "/api/transform": auth.Viewer, "/api/counterparty": auth.Viewer, "/api/cardcrypto": auth.Viewer,
		"/api/runs": auth.Operator, "/api/suites/run": auth.Operator, "/api/load": auth.Operator, "/api/load/replay": auth.Operator,
		"/api/users": auth.Admin, "/api/guidelines": auth.Admin, "/api/catalogue/run": auth.Operator,
	} {
		if got := need(httptest.NewRequest("POST", path, nil)); got != want {
			t.Errorf("POST %s needs %s, want %s", path, got, want)
		}
	}
	if got := need(httptest.NewRequest("GET", "/api/runs", nil)); got != auth.Viewer {
		t.Errorf("GET needs %s", got)
	}
	if got := need(httptest.NewRequest("GET", "/api/guidelines", nil)); got != auth.Viewer {
		t.Errorf("GET /api/guidelines needs %s", got)
	}
	if got := need(httptest.NewRequest("DELETE", "/api/guidelines/epc", nil)); got != auth.Admin {
		t.Errorf("DELETE /api/guidelines needs %s", got)
	}
}
