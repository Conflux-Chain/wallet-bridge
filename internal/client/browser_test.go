//go:build windows || darwin

package client

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
)

func browserClientFixture(t *testing.T, intercept func(http.ResponseWriter, *http.Request) bool) (*Client, *app.BrowserAuthorization) {
	t.Helper()
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	clientToken, _ := credentials.Read(c.dir, credentials.Client)
	controlToken, _ := credentials.Read(c.dir, credentials.Control)
	core := testBrowserAuthorization(t)
	handler, err := api.New(api.Config{Version: "test", ClientToken: clientToken, ControlToken: controlToken, StartedAt: time.Now(), Stop: func() {}, BrowserAuthorization: core})
	if err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/control/browser-grants") && r.Header.Get("Authorization") != "Bearer "+controlToken {
			t.Error("browser management did not use the control credential")
		}
		if intercept != nil && intercept(w, r) {
			return
		}
		handler.ServeHTTP(w, r)
	}))
	return c, core
}

func TestBrowserClientAuthorizationLifecycle(t *testing.T) {
	c, core := browserClientFixture(t, nil)
	ctx := context.Background()
	link, err := c.BrowserLink(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ticket := strings.TrimPrefix(link.URL, api.Origin+"/#authorize=")
	first, err := core.Authorize(ctx, ticket)
	if err != nil {
		t.Fatal(err)
	}
	secondTicket, err := core.Issue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := core.Authorize(ctx, secondTicket.Token)
	if err != nil {
		t.Fatal(err)
	}
	page, err := c.ListBrowsers(ctx, 1, "")
	if err != nil || len(page.Grants) != 1 || page.NextAfter == "" {
		t.Fatalf("first page: %v", err)
	}
	next, err := c.ListBrowsers(ctx, 1, page.NextAfter)
	if err != nil || len(next.Grants) != 1 || next.Grants[0].ID == page.Grants[0].ID || next.NextAfter != "" {
		t.Fatalf("next page: %v", err)
	}
	if result, err := c.RevokeBrowser(ctx, first.Grant.ID, false); err != nil || result.State != "revoked" {
		t.Fatalf("single revoke: %v", err)
	}
	if _, err := core.Authenticate(ctx, first.Token); err == nil {
		t.Fatal("revoked browser remained authorized")
	}
	if _, err := core.Authenticate(ctx, second.Token); err != nil {
		t.Fatal("single revocation affected another browser")
	}
	if _, err := c.RevokeBrowser(ctx, strings.Repeat("f", 32), false); codeOf(t, err) != "browser_grant_not_found" {
		t.Fatal(err)
	}
	pending, err := core.Issue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.RevokeBrowser(ctx, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Authenticate(ctx, second.Token); err == nil {
		t.Fatal("revoke-all left a browser authorized")
	}
	if _, err := core.Authorize(ctx, pending.Token); err == nil {
		t.Fatal("revoke-all left a link usable")
	}
	page, err = c.ListBrowsers(ctx, 50, "")
	if err != nil || len(page.Grants) != 2 {
		t.Fatalf("history: %v", err)
	}
	for _, grant := range page.Grants {
		if grant.State != "revoked" {
			t.Fatal("history lost revocation state")
		}
	}
}

func TestBrowserClientChecksCapabilityBeforeControl(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(c.dir, "control.key")); err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("incompatible service received a credential")
		}
		json.NewEncoder(w).Encode(sign(signedInfo(t, c, r.URL.Query().Get("challenge"))))
	}))
	if _, err := c.ListBrowsers(context.Background(), 50, ""); codeOf(t, err) != "incompatible_service" {
		t.Fatal(err)
	}
}

func TestBrowserClientRejectsInvalidInputWithoutStarting(t *testing.T) {
	c := newTestClient(t)
	c.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	ctx := context.Background()
	if _, err := c.ListBrowsers(ctx, 50, ""); codeOf(t, err) != "service_not_running" {
		t.Fatal(err)
	}
	if _, err := c.RevokeBrowser(ctx, "", true); codeOf(t, err) != "service_not_running" {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
		t.Fatal("management created a data directory")
	}
	c.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("invalid input reached the network")
		return nil, nil
	}
	if _, err := c.ListBrowsers(ctx, 51, ""); codeOf(t, err) != "bad_request" {
		t.Fatal(err)
	}
	if _, err := c.ListBrowsers(ctx, 50, "bad"); codeOf(t, err) != "bad_request" {
		t.Fatal(err)
	}
	if _, err := c.RevokeBrowser(ctx, "", false); codeOf(t, err) != "bad_request" {
		t.Fatal(err)
	}
	if _, err := c.RevokeBrowser(ctx, strings.Repeat("a", 32), true); codeOf(t, err) != "bad_request" {
		t.Fatal(err)
	}
}

func TestBrowserClientRejectsUnsafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, code string
		status                 int
	}{
		{"external URL", "/api/v1/control/browser-grants", `{"url":"https://example.com/#authorize=` + strings.Repeat("a", 64) + `","expiresAt":"2099-01-01T00:00:00Z"}`, "request_failed", 201},
		{"query secret", "/api/v1/control/browser-grants", `{"url":"` + api.Origin + `/?authorize=` + strings.Repeat("a", 64) + `","expiresAt":"2099-01-01T00:00:00Z"}`, "request_failed", 201},
		{"invalid metadata", "/api/v1/control/browser-grants", `{"grants":[{"id":"terminal-control","state":"active"}]}`, "request_failed", 200},
		{"remote message", "/api/v1/control/browser-grants/revoke", `{"code":"authorization_error","message":"hidden-secret"}`, "authorization_error", 500},
		{"unknown code", "/api/v1/control/browser-grants/revoke", `{"code":"hidden-secret","message":"hidden-secret"}`, "request_failed", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := browserClientFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != tc.path {
					return false
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
				return true
			})
			var err error
			switch tc.name {
			case "external URL", "query secret":
				_, err = c.BrowserLink(context.Background())
			case "invalid metadata":
				_, err = c.ListBrowsers(context.Background(), 50, "")
			default:
				_, err = c.RevokeBrowser(context.Background(), "", true)
			}
			if codeOf(t, err) != tc.code || strings.Contains(err.Error(), "hidden-secret") || strings.Contains(err.Error(), "example.com") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}

func TestBrowserClientDoesNotReplayLostRevocation(t *testing.T) {
	var attempts atomic.Int32
	c, _ := browserClientFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/control/browser-grants/revoke" {
			return false
		}
		attempts.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return true
		}
		conn.Close()
		return true
	})
	if _, err := c.RevokeBrowser(context.Background(), "", true); codeOf(t, err) != "request_failed" || attempts.Load() != 1 {
		t.Fatalf("lost revoke retried: %v, attempts=%d", err, attempts.Load())
	}
}

func TestBrowserClientRejectsClosedProofConnection(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("control credential crossed a closed connection")
		}
		info := signedInfo(t, c, r.URL.Query().Get("challenge"))
		info.Capabilities = append(info.Capabilities, "browser.authorization")
		w.Header().Set("Connection", "close")
		json.NewEncoder(w).Encode(sign(info))
	}))
	if _, err := c.RevokeBrowser(context.Background(), "", true); codeOf(t, err) != "identity_unverified" {
		t.Fatal(err)
	}
}
