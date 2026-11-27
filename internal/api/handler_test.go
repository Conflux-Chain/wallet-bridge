package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

var serviceFixture = strings.Repeat("11", 32)
var legacyControlFixture = strings.Repeat("22", 32)

func testHandler(t *testing.T, stop func()) http.Handler {
	t.Helper()
	// These basic boundary tests never access storage. Browser behavior tests
	// supply a real SQLite-backed core below instead of this unused dependency.
	authorization, err := app.NewBrowserAuthorization(struct{ app.BrowserGrantStore }{})
	if err != nil {
		t.Fatal(err)
	}
	connections, err := app.NewConnectionService(struct{ app.ConnectionServiceStore }{})
	if err != nil {
		t.Fatal(err)
	}
	return testHandlerWithCores(t, stop, authorization, connections, struct{ TransactionRequests }{})
}

func testHandlerWithCores(t *testing.T, stop func(), authorization *app.BrowserAuthorization, connections *app.ConnectionService, transactions TransactionRequests) http.Handler {
	t.Helper()
	h, err := New(Config{
		Version: "test", ServiceToken: serviceFixture,
		StartedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Stop: stop,
		Assets:               fstest.MapFS{"index.html": {Data: []byte("<h1>Wallet Bridge</h1>")}, "assets/app.js": {Data: []byte("console.log('bridge')")}},
		BrowserAuthorization: authorization,
		Connections:          connections,
		Transactions:         transactions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestHTTPBoundaryAndSharedAccess(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, token, host, origin, body, contentType, code string
		status                                                           int
		stop                                                             bool
	}{
		{name: "public info", path: "/api/v1/info", status: 200},
		{name: "missing token", path: "/api/v1/status", status: 401, code: "unauthenticated"},
		{name: "query is not authentication", path: "/api/v1/status?token=" + serviceFixture, status: 401, code: "unauthenticated"},
		{name: "wrong token", path: "/api/v1/status", token: "incorrect", status: 401, code: "unauthenticated"},
		{name: "service status", path: "/api/v1/status", token: serviceFixture, status: 200},
		{name: "legacy control rejected", path: "/api/v1/status", token: legacyControlFixture, status: 401, code: "unauthenticated"},
		{name: "wrong host", path: "/api/v1/info", host: "localhost:18765", status: 403, code: "invalid_host"},
		{name: "wrong origin", path: "/api/v1/status", token: serviceFixture, origin: "https://example.com", status: 403, code: "invalid_origin"},
		{name: "null origin", path: "/api/v1/info", origin: "null", status: 403, code: "invalid_origin"},
		{name: "same origin", path: "/api/v1/status", token: serviceFixture, origin: Origin, status: 200},
		{name: "stop without authentication", method: "POST", path: "/api/v1/control/stop", status: 401, code: "unauthenticated"},
		{name: "legacy control cannot stop", method: "POST", path: "/api/v1/control/stop", token: legacyControlFixture, status: 401, code: "unauthenticated"},
		{name: "service stop", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, status: 202, stop: true},
		{name: "JSON stop", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: "{ \n }", contentType: "application/json", status: 202, stop: true},
		{name: "unknown fields", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: `{"force":true}`, contentType: "application/json", status: 400, code: "bad_request"},
		{name: "trailing JSON", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: "{}{}", contentType: "application/json", status: 400, code: "bad_request"},
		{name: "null JSON", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: "null", contentType: "application/json", status: 400, code: "bad_request"},
		{name: "large body", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: strings.Repeat(" ", 1025), status: 413, code: "request_too_large"},
		{name: "wrong media type", method: "POST", path: "/api/v1/control/stop", token: serviceFixture, body: "{}", contentType: "text/plain", status: 415, code: "unsupported_media_type"},
		{name: "stop query rejected", method: "POST", path: "/api/v1/control/stop?force=true", token: serviceFixture, status: 400, code: "bad_request"},
		{name: "stop method", path: "/api/v1/control/stop", token: serviceFixture, status: 405, code: "method_not_allowed"},
		{name: "preflight", method: "OPTIONS", path: "/api/v1/status", origin: Origin, status: 405, code: "method_not_allowed"},
		{name: "unknown API", path: "/api/v1/missing", status: 404, code: "not_found"},
		{name: "page", path: "/", status: 200},
		{name: "JavaScript", path: "/assets/app.js", status: 200},
		{name: "page HEAD", method: "HEAD", path: "/", status: 200},
		{name: "no directory listing", path: "/assets", status: 404, code: "not_found"},
		{name: "no traversal", path: "/../client.key", status: 404, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stopped := false
			h := testHandler(t, func() { stopped = true })
			verb := tc.method
			if verb == "" {
				verb = "GET"
			}
			req := httptest.NewRequest(verb, Origin+tc.path, strings.NewReader(tc.body))
			if tc.host != "" {
				req.Host = tc.host
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.status, response.Body.String())
			}
			if tc.code != "" {
				var body ErrorResponse
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != tc.code {
					t.Fatalf("unexpected error response: %s", response.Body.String())
				}
			}
			if stopped != tc.stop {
				t.Fatal("unexpected stop callback")
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("CORS was enabled")
			}
			if verb == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if strings.Contains(response.Body.String(), serviceFixture) || strings.Contains(response.Body.String(), legacyControlFixture) {
				t.Fatal("response exposed a credential")
			}
		})
	}
}

func TestRepeatedSecurityHeadersRejected(t *testing.T) {
	for _, header := range []string{"Origin", "Authorization"} {
		req := httptest.NewRequest("GET", Origin+"/api/v1/status", nil)
		req.Header.Set("Authorization", "Bearer "+serviceFixture)
		if header == "Origin" {
			req.Header[header] = []string{Origin, Origin}
		} else {
			req.Header[header] = []string{"Bearer " + serviceFixture, "Bearer " + serviceFixture}
		}
		response := httptest.NewRecorder()
		testHandler(t, func() {}).ServeHTTP(response, req)
		if response.Code != 401 && response.Code != 403 {
			t.Fatalf("duplicate %s accepted", header)
		}
	}
}

func TestInfoProofBindsMetadata(t *testing.T) {
	h := testHandler(t, func() {})
	challenge := strings.Repeat("ab", 32)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("GET", Origin+"/api/v1/info?challenge="+challenge, nil))
	var info InfoResponse
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil || response.Code != 200 {
		t.Fatal("info response invalid", err)
	}
	proof := info.Proof
	data := []byte(`{"name":"wallet-bridge","version":"test","apiVersion":{"major":1,"minor":17},"capabilities":["service.status","service.stop","browser.authorization","service.auth.shared","connections.lifecycle","browser.connections.coordination","browser.connections.decisions","browser.wallet-operation","transactions.lifecycle","browser.transactions.read","browser.transactions.coordination","browser.transactions.decisions","browser.pending-requests","browser.activity","browser.activity.remove","service.upgrade-stop","service.upgrade-verify","browser.open-request"],"ready":true,"challenge":"` + challenge + `"}`)
	key, _ := hex.DecodeString(serviceFixture)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("wallet-bridge.info.v1\n"))
	mac.Write(data)
	if proof != hex.EncodeToString(mac.Sum(nil)) || info.Challenge != challenge || !info.Ready || info.APIVersion.Major != 1 {
		t.Fatal("service proof or metadata mismatch")
	}
	for _, query := range []string{"challenge=short", "challenge=" + strings.Repeat("AB", 32), "challenge=" + challenge + "&challenge=" + challenge, "token=secret", "challenge=%ZZ"} {
		response := httptest.NewRecorder()
		req := httptest.NewRequest("GET", Origin+"/api/v1/info", nil)
		req.URL.RawQuery = query
		h.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("invalid challenge query accepted: %q", query)
		}
	}
}
