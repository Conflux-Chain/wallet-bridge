package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCandidateAdmission(t *testing.T) {
	config := testHandler(t, func() {}).(*handler).config
	config.VerifyingUpgrade = strings.Repeat("a", 32)
	h, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/connections"}, {"POST", "/api/v1/transactions"},
		{"POST", "/api/v1/browser/transactions/" + strings.Repeat("1", 32) + "/result"},
		{"POST", "/api/v1/control/browser-grants"}, {"GET", "/api/v1/browser/activity"},
		{"GET", "/api/v1/status"}, {"GET", "/"},
	} {
		req := httptest.NewRequest(tc.method, Origin+tc.path, nil)
		req.Header.Set("Authorization", "Bearer "+serviceFixture)
		req.Header.Set("Origin", Origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 503 || !strings.Contains(w.Body.String(), "upgrade_in_progress") {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
}
