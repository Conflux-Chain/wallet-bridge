//go:build windows || darwin

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

type browserFixture struct {
	core     *app.ConnectionService
	database *store.Store
	handler  http.Handler
	dir      string
}

func newBrowserFixture(t *testing.T, dir string) browserFixture {
	t.Helper()
	if dir == "" {
		dir = filepath.Join(t.TempDir(), "data")
	}
	database, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	core, err := app.NewBrowserAuthorization(database)
	if err != nil {
		t.Fatal(err)
	}
	connections, err := app.NewConnectionService(database)
	if err != nil {
		t.Fatal(err)
	}
	return browserFixture{core: connections, database: database, handler: testHandlerWithCores(t, func() { t.Error("browser test stopped the service") }, core, connections), dir: dir}
}

func (f browserFixture) request(t *testing.T, method, path, body string, headers map[string]string, expected int) *httptest.ResponseRecorder {
	t.Helper()
	base, query, _ := strings.Cut(path, "?")
	r := httptest.NewRequest(method, Origin+base, strings.NewReader(body))
	r.URL.RawQuery = query
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if key == "Host" {
			r.Host = value
		} else {
			r.Header.Set(key, value)
		}
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != expected {
		t.Fatalf("%s %s returned %d, want %d", method, base, w.Code, expected)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("authorization response was cacheable or enabled CORS")
	}
	for _, secret := range []string{serviceFixture, legacyControlFixture} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("response disclosed a local credential")
		}
	}
	return w
}

func responseJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal("invalid response JSON", err)
	}
	return value
}

func controlHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer " + serviceFixture}
}
func browserHeaders(cookie *http.Cookie) map[string]string {
	return map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "Origin": Origin}
}

func (f browserFixture) ticket(t *testing.T) string {
	t.Helper()
	w := f.request(t, "POST", "/api/v1/control/browser-grants", "{}", controlHeaders(), 201)
	link := responseJSON[BrowserTicketResponse](t, w)
	u, err := url.Parse(link.URL)
	if err != nil || u.Scheme+"://"+u.Host != Origin || u.Path != "/" || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "authorize=") {
		t.Fatal("invalid authorization link format")
	}
	value := strings.TrimPrefix(u.Fragment, "authorize=")
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		t.Fatal("invalid ticket encoding")
	}
	expires, err := time.Parse(time.RFC3339Nano, link.ExpiresAt)
	if err != nil || time.Until(expires) < 110*time.Second || time.Until(expires) > app.BrowserTicketLifetime {
		t.Fatal("invalid link expiration")
	}
	return value
}

func (f browserFixture) exchange(t *testing.T, ticket string) (*http.Cookie, BrowserGrantResponse) {
	t.Helper()
	w := f.request(t, "POST", "/api/v1/browser/authorize", `{"ticket":"`+ticket+`"}`, map[string]string{"Origin": Origin}, 200)
	grant := responseJSON[BrowserGrantResponse](t, w)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("exchange did not set exactly one cookie")
	}
	cookie := cookies[0]
	if cookie.Name != browserCookieName || cookie.Path != browserCookiePath || cookie.Domain != "" || !cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode || cookie.Secure || cookie.MaxAge <= 0 || cookie.MaxAge > int(app.BrowserGrantLifetime/time.Second) {
		t.Fatal("incorrect cookie protection or expiration")
	}
	expires, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil || !cookie.Expires.Equal(expires.Truncate(time.Second)) || grant.State != "active" || !validResourceID(grant.ID) {
		t.Fatal("cookie and grant metadata disagree")
	}
	if strings.Contains(w.Body.String(), cookie.Value) || strings.Contains(w.Body.String(), ticket) {
		t.Fatal("exchange JSON disclosed a secret")
	}
	return cookie, grant
}

func TestBrowserHTTPExchangeAndRoles(t *testing.T) {
	f := newBrowserFixture(t, "")
	ticket := f.ticket(t)
	cookie, grant := f.exchange(t, ticket)
	w := f.request(t, "GET", "/api/v1/status", "", browserHeaders(cookie), 200)
	status := responseJSON[StatusResponse](t, w)
	if status.Browser == nil || status.Browser.ID != grant.ID || status.Browser.ExpiresAt != grant.ExpiresAt || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("status lost browser metadata or renewed its cookie")
	}
	for _, path := range []string{"/api/v1/control/browser-grants", "/api/v1/control/browser-grants/revoke", "/api/v1/control/stop"} {
		method, body := "POST", "{}"
		if path == "/api/v1/control/browser-grants" {
			method, body = "GET", ""
		}
		f.request(t, method, path, body, browserHeaders(cookie), 401)
		f.request(t, method, path, body, map[string]string{"Authorization": "Bearer " + legacyControlFixture}, 401)
	}
	f.request(t, "POST", "/api/v1/control/browser-grants", "{}", browserHeaders(cookie), 401)
	f.request(t, "POST", "/api/v1/control/browser-grants", "{}", controlHeaders(), 201)
	f.request(t, "POST", "/api/v1/browser/logout", "{}", map[string]string{"Origin": Origin, "Authorization": "Bearer " + serviceFixture}, 401)
	f.request(t, "POST", "/api/v1/browser/authorize", `{"ticket":"`+ticket+`"}`, map[string]string{"Origin": Origin}, 401)
	for _, headers := range []map[string]string{
		{"Cookie": cookie.Name + "=" + cookie.Value, "Authorization": "Bearer invalid"},
		{"Cookie": cookie.Name + "=" + cookie.Value + "; " + cookie.Name + "=" + cookie.Value},
	} {
		f.request(t, "GET", "/api/v1/status", "", headers, 401)
	}
	f.request(t, "GET", "/api/v1/status", "", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "Origin": "https://example.com"}, 403)
	f.request(t, "GET", "/api/v1/status", "", map[string]string{"Cookie": cookie.Name + "=" + cookie.Value, "Host": "localhost:18765"}, 403)
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(cookie), 200)
}

func TestBrowserHTTPRejectsUnsafeExchangeWithoutConsumingTicket(t *testing.T) {
	f := newBrowserFixture(t, "")
	ticket := f.ticket(t)
	body := `{"ticket":"` + ticket + `"}`
	for _, tc := range []struct {
		name, method, path, body string
		headers                  map[string]string
		status                   int
	}{
		{"missing Origin", "POST", "/api/v1/browser/authorize", body, nil, 403},
		{"foreign Origin", "POST", "/api/v1/browser/authorize", body, map[string]string{"Origin": "https://example.com"}, 403},
		{"null Origin", "POST", "/api/v1/browser/authorize", body, map[string]string{"Origin": "null"}, 403},
		{"query", "POST", "/api/v1/browser/authorize?token=ignored", body, nil, 400},
		{"unknown field", "POST", "/api/v1/browser/authorize", `{"ticket":"` + ticket + `","extra":true}`, nil, 400},
		{"null", "POST", "/api/v1/browser/authorize", "null", nil, 400},
		{"array", "POST", "/api/v1/browser/authorize", "[]", nil, 400},
		{"trailing", "POST", "/api/v1/browser/authorize", body + "{}", nil, 400},
		{"missing body", "POST", "/api/v1/browser/authorize", "", nil, 400},
		{"missing ticket", "POST", "/api/v1/browser/authorize", "{}", nil, 401},
		{"too large", "POST", "/api/v1/browser/authorize", strings.Repeat(" ", 1025), nil, 413},
		{"media type", "POST", "/api/v1/browser/authorize", body, map[string]string{"Content-Type": "text/plain"}, 415},
		{"method", "GET", "/api/v1/browser/authorize", "", nil, 405},
		{"preflight", "OPTIONS", "/api/v1/browser/authorize", "", nil, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{"Origin": Origin}
			if strings.Contains(tc.name, "Origin") {
				delete(headers, "Origin")
			}
			for key, value := range tc.headers {
				headers[key] = value
			}
			w := f.request(t, tc.method, tc.path, tc.body, headers, tc.status)
			if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), ticket) {
				t.Fatal("rejected exchange set a cookie or disclosed its ticket")
			}
		})
	}
	r := httptest.NewRequest("POST", Origin+"/api/v1/browser/authorize", strings.NewReader(body))
	r.Header["Origin"] = []string{Origin, Origin}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("duplicate Origin accepted")
	}
	f.exchange(t, ticket)
}

func TestBrowserHTTPRevocationAndHistory(t *testing.T) {
	f := newBrowserFixture(t, "")
	first, firstGrant := f.exchange(t, f.ticket(t))
	second, _ := f.exchange(t, f.ticket(t))
	third, _ := f.exchange(t, f.ticket(t))
	for range 2 {
		f.request(t, "POST", "/api/v1/control/browser-grants/revoke", `{"id":"`+firstGrant.ID+`"}`, controlHeaders(), 200)
	}
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(first), 401)
	for _, headers := range []map[string]string{nil, {"Cookie": second.Name + "=" + second.Value, "Origin": "null"}} {
		f.request(t, "POST", "/api/v1/browser/logout", "{}", headers, 403)
	}
	w := f.request(t, "POST", "/api/v1/browser/logout", "{}", browserHeaders(second), 200)
	deleted := w.Result().Cookies()
	if len(deleted) != 1 || deleted[0].Name != browserCookieName || deleted[0].Path != browserCookiePath || deleted[0].MaxAge != -1 || !deleted[0].HttpOnly {
		t.Fatal("logout failed to expire the management cookie")
	}
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(second), 401)
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(third), 200)
	pending := f.ticket(t)
	f.request(t, "POST", "/api/v1/control/browser-grants/revoke", `{"all":true}`, controlHeaders(), 200)
	f.request(t, "POST", "/api/v1/browser/authorize", `{"ticket":"`+pending+`"}`, map[string]string{"Origin": Origin}, 401)
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(third), 401)
	list := f.request(t, "GET", "/api/v1/control/browser-grants", "", controlHeaders(), 200)
	grants := responseJSON[BrowserGrantListResponse](t, list)
	if len(grants.Grants) != 3 {
		t.Fatal("revocation deleted history")
	}
	for _, grant := range grants.Grants {
		if grant.State != "revoked" || grant.RevokedAt == "" {
			t.Fatal("revocation metadata missing")
		}
	}
	for _, secret := range []string{first.Value, second.Value, third.Value, pending, "token_hash"} {
		if strings.Contains(list.Body.String(), secret) {
			t.Fatal("grant listing disclosed a secret or digest")
		}
	}
	for _, body := range []string{"{}", `{"all":false}`, `{"id":"bad"}`, `{"id":"` + firstGrant.ID + `","all":true}`, `{"all":true,"force":true}`} {
		f.request(t, "POST", "/api/v1/control/browser-grants/revoke", body, controlHeaders(), 400)
	}
	f.request(t, "POST", "/api/v1/control/browser-grants/revoke", `{"id":"`+strings.Repeat("f", 32)+`"}`, controlHeaders(), 404)
}

func TestBrowserHTTPRestartExpiryAndStorageFailure(t *testing.T) {
	f := newBrowserFixture(t, "")
	cookie, grant := f.exchange(t, f.ticket(t))
	pending := f.ticket(t)
	f.database.Close()
	f = newBrowserFixture(t, f.dir)
	w := f.request(t, "GET", "/api/v1/status", "", browserHeaders(cookie), 200)
	status := responseJSON[StatusResponse](t, w)
	if status.Browser == nil || status.Browser.ExpiresAt != grant.ExpiresAt {
		t.Fatal("restart renewed or lost browser authorization")
	}
	f.request(t, "POST", "/api/v1/browser/authorize", `{"ticket":"`+pending+`"}`, map[string]string{"Origin": Origin}, 401)
	var secret [32]byte
	secret[0] = 7
	issued := time.Now().UTC().Truncate(time.Millisecond).Add(-31 * 24 * time.Hour)
	expired := app.BrowserGrant{ID: strings.Repeat("1", 32), IssuedAt: issued, ExpiresAt: issued.Add(app.BrowserGrantLifetime)}
	if err := f.database.CreateBrowserGrant(context.Background(), expired, sha256.Sum256(secret[:])); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", "/api/v1/status", "", browserHeaders(&http.Cookie{Name: browserCookieName, Value: hex.EncodeToString(secret[:])}), 401)
	f.database.Close()
	w = f.request(t, "GET", "/api/v1/status", "", browserHeaders(cookie), 500)
	if strings.Contains(w.Body.String(), f.dir) || strings.Contains(w.Body.String(), cookie.Value) || w.Header().Get("Set-Cookie") != "" {
		t.Fatal("storage failure disclosed internal data or renewed a cookie")
	}
	if responseJSON[ErrorResponse](t, w).Code != "authorization_error" {
		t.Fatal("unstable storage failure code")
	}
}

func TestBrowserHTTPPaginationAndTicketLimit(t *testing.T) {
	f := newBrowserFixture(t, "")
	for range app.MaxBrowserTickets {
		f.ticket(t)
	}
	f.request(t, "POST", "/api/v1/control/browser-grants", "{}", controlHeaders(), 429)
	issued := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Hour)
	for index := range 52 {
		at := issued.Add(time.Duration(index) * time.Second)
		grant := app.BrowserGrant{ID: fmt.Sprintf("%032x", index+1), IssuedAt: at, ExpiresAt: at.Add(app.BrowserGrantLifetime)}
		if err := f.database.CreateBrowserGrant(context.Background(), grant, sha256.Sum256([]byte(grant.ID))); err != nil {
			t.Fatal(err)
		}
	}
	w := f.request(t, "GET", "/api/v1/control/browser-grants", "", controlHeaders(), 200)
	page := responseJSON[BrowserGrantListResponse](t, w)
	if len(page.Grants) != 50 || page.NextAfter == "" || w.Body.Len() >= 16<<10 {
		t.Fatal("listing did not bound its page size")
	}
	w = f.request(t, "GET", "/api/v1/control/browser-grants?after="+page.NextAfter, "", controlHeaders(), 200)
	next := responseJSON[BrowserGrantListResponse](t, w)
	if len(next.Grants) != 2 || next.NextAfter != "" || page.Grants[49].ID == next.Grants[0].ID {
		t.Fatal("pagination lost or repeated records")
	}
	for _, query := range []string{"limit=0", "limit=51", "limit=x", "limit=1&limit=2", "after=", "after=" + strings.Repeat("f", 32), "unknown=1", "limit=%ZZ"} {
		f.request(t, "GET", "/api/v1/control/browser-grants?"+query, "", controlHeaders(), 400)
	}
}
