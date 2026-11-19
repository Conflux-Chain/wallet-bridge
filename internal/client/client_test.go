//go:build windows || darwin

package client

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	c.interval = time.Millisecond
	c.ensureTimeout = 300 * time.Millisecond
	c.stopTimeout = 300 * time.Millisecond
	return c
}

func testBrowserAuthorization(t *testing.T) *app.BrowserAuthorization {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	core, err := app.NewBrowserAuthorization(database)
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func useServer(t *testing.T, c *Client, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" || address != api.Address {
			t.Error("fixed direct address changed")
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp4", server.Listener.Addr().String())
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected structured error, got %v", err)
	}
	return failure.Code
}

func TestStatusAndStopUseSeparateCredentials(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	clientToken, _ := credentials.Read(c.dir, credentials.Client)
	controlToken, _ := credentials.Read(c.dir, credentials.Control)
	var stopped atomic.Bool
	handler, err := api.New(api.Config{Version: "test", ClientToken: clientToken, ControlToken: controlToken, StartedAt: time.Now(), Stop: func() { stopped.Store(true) }, BrowserAuthorization: testBrowserAuthorization(t)})
	if err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/status" && r.Header.Get("Authorization") != "Bearer "+clientToken {
			t.Error("status used a different credential")
		}
		if r.URL.Path == "/api/v1/control/stop" && r.Header.Get("Authorization") != "Bearer "+controlToken {
			t.Error("stop used a different credential")
		}
		handler.ServeHTTP(w, r)
	}))
	// Environment proxies must not affect the dedicated local transport.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	status, err := c.Status(context.Background())
	if err != nil || status.State != "running" || status.Info.Version != "test" || status.Info.Proof != "" {
		t.Fatalf("status failed: %v", err)
	}
	status, err = c.Stop(context.Background())
	if err != nil || status.State != "stopped" || !stopped.Load() {
		t.Fatalf("stop failed: %v", err)
	}
}

func TestRefusedQueriesDoNotCreateFiles(t *testing.T) {
	c := newTestClient(t)
	c.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	for _, method := range []func(context.Context) (Status, error){c.Status, c.Stop} {
		status, err := method(context.Background())
		if err != nil || status.State != "stopped" {
			t.Fatalf("stopped query failed: %v", err)
		}
	}
	if _, err := os.Stat(c.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("query created a data directory")
	}
	lock, err := platform.AcquireServiceLock(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := c.Status(context.Background()); codeOf(t, err) != "service_unavailable" {
		t.Fatal("held lock reported stopped")
	}
}

func signedInfo(t *testing.T, c *Client, challenge string) api.InfoResponse {
	t.Helper()
	token, err := credentials.Read(c.dir, credentials.Client)
	if err != nil {
		t.Fatal(err)
	}
	return api.InfoResponse{Name: "wallet-bridge", Version: "test", APIVersion: api.APIVersion{Major: 1}, Capabilities: []string{"service.status", "service.stop"}, Ready: true, Challenge: challenge, Proof: token}
}

func sign(info api.InfoResponse) api.InfoResponse {
	key, _ := hex.DecodeString(info.Proof)
	info.Proof = ""
	data, _ := json.Marshal(info)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("wallet-bridge.info.v1\n"))
	mac.Write(data)
	info.Proof = hex.EncodeToString(mac.Sum(nil))
	return info
}

func TestUntrustedOrIncompatibleServiceNeverGetsBearer(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"forged proof", "identity_unverified"}, {"changed metadata", "identity_unverified"}, {"replayed nonce", "identity_unverified"},
		{"major", "incompatible_service"}, {"capabilities", "incompatible_service"}, {"not ready", "service_unavailable"},
		{"redirect", "identity_unverified"}, {"oversize", "identity_unverified"}, {"header limit", "service_unavailable"}, {"missing credentials", "identity_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			if tc.name != "missing credentials" {
				if err := credentials.Ensure(c.dir); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int32
			useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "" {
					t.Error("Bearer sent to an unverified or incompatible service")
				}
				if tc.name == "missing credentials" {
					return
				}
				info := signedInfo(t, c, r.URL.Query().Get("challenge"))
				switch tc.name {
				case "major":
					info.APIVersion.Major = 2
				case "capabilities":
					info.Capabilities = nil
				case "not ready":
					info.Ready = false
				case "replayed nonce":
					info.Challenge = strings.Repeat("00", 32)
				case "redirect":
					w.Header().Set("Location", api.Origin+"/redirected")
					w.WriteHeader(302)
					return
				case "oversize":
					w.Write([]byte(strings.Repeat("x", 17<<10)))
					return
				case "header limit":
					w.Header().Set("X-Large", strings.Repeat("x", 9<<10))
				}
				info = sign(info)
				if tc.name == "forged proof" {
					info.Proof = strings.Repeat("00", 32)
				}
				if tc.name == "changed metadata" {
					info.APIVersion.Major = 2
				}
				json.NewEncoder(w).Encode(info)
			}))
			_, err := c.Status(context.Background())
			if codeOf(t, err) != tc.code {
				t.Fatalf("error=%v, want %s", err, tc.code)
			}
			if requests.Load() > 1 {
				t.Fatal("redirect followed or credentials attempted")
			}
		})
	}
}

func TestClosedProofConnectionCannotLeakBearer(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("Bearer crossed a closed proof connection")
		}
		w.Header().Set("Connection", "close")
		json.NewEncoder(w).Encode(sign(signedInfo(t, c, r.URL.Query().Get("challenge"))))
	}))
	_, err := c.Status(context.Background())
	if codeOf(t, err) != "identity_unverified" || requests.Load() != 1 {
		t.Fatalf("closed connection handling failed: %v", err)
	}
}
