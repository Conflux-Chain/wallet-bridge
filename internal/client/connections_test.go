//go:build windows || darwin

package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func connectionClientFixture(t *testing.T, wrap func(http.Handler) http.Handler) (*Client, *app.ConnectionService, *store.Store) {
	t.Helper()
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	token, err := credentials.Read(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), c.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	core, err := app.NewConnectionService(db)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := app.NewBrowserAuthorization(db)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := api.New(api.Config{Version: "test", StartedAt: time.Now(), ServiceToken: token,
		Stop: func() {}, BrowserAuthorization: grants, Connections: core, Transactions: db})
	if err != nil {
		t.Fatal(err)
	}
	if wrap != nil {
		handler = wrap(handler)
	}
	useServer(t, c, handler)
	return c, core, db
}

func TestConnectionClientRetryAfterCommit(t *testing.T) {
	var attempts atomic.Int32
	c, _, _ := connectionClientFixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == connectionsPath && attempts.Add(1) == 1 {
				recorded := httptest.NewRecorder()
				next.ServeHTTP(recorded, r)
				if recorded.Code != http.StatusOK {
					t.Errorf("create returned %d before response loss", recorded.Code)
				}
				panic(http.ErrAbortHandler) // close the socket after SQLite commits
			}
			next.ServeHTTP(w, r)
		})
	})
	ctx := t.Context()
	input := api.ConnectionCreateRequest{ID: NewConnectionID(), ClientName: "Codex", ChainID: "9007199254740993", Intent: strings.Repeat("<&>\u2028", 1300)}
	if _, err := c.CreateConnection(ctx, input); codeOf(t, err) != "request_failed" || attempts.Load() != 1 {
		t.Fatalf("lost response: %v, attempts=%d", err, attempts.Load())
	}
	original, err := c.GetConnection(ctx, input.ID)
	if err != nil || original.State != "pending" || original.Intent != input.Intent || original.ChainID != "0x20000000000001" {
		t.Fatalf("cannot recover original request: %v", err)
	}
	if replay, err := c.CreateConnection(ctx, input); err != nil || replay != original {
		t.Fatalf("replay changed the original: %v", err)
	}
	changed := input
	changed.ClientName = "WorkBuddy"
	if _, err := c.CreateConnection(ctx, changed); codeOf(t, err) != "connection_conflict" {
		t.Fatal(err)
	}
	canceled, err := c.CancelConnection(ctx, input.ID)
	if err != nil || canceled.State != "canceled" {
		t.Fatal("cancel:", err)
	}
	if replay, err := c.CreateConnection(ctx, input); err != nil || replay != canceled {
		t.Fatal("replay after cancellation:", err)
	}
}

func TestConnectionClientSessionHistory(t *testing.T) {
	ctx := t.Context()
	c, core, _ := connectionClientFixture(t, nil)
	input := api.ConnectionCreateRequest{ID: NewConnectionID(), ClientName: "Fluent test", ChainID: "71"}
	if _, err := c.CreateConnection(ctx, input); err != nil {
		t.Fatal(err)
	}
	binding := app.ConnectionBinding{Wallet: app.WalletDescriptor{Kind: app.WalletEIP6963, Name: "Test wallet", RDNS: "test.wallet"},
		Account: "0x" + strings.Repeat("ab", 20), ChainID: "0x47"}
	if _, err := core.Complete(ctx, input.ID, binding); err != nil {
		t.Fatal(err)
	}
	connected, err := c.GetConnection(ctx, input.ID)
	if err != nil || connected.State != "succeeded" {
		t.Fatalf("connection result: %v", err)
	}
	active, err := c.GetSession(ctx, connected.SessionID)
	if err != nil || active.State != "active" || active.ConnectionID != input.ID ||
		active.Account != binding.Account || active.ChainID != binding.ChainID ||
		active.Wallet != (api.WalletResponse{Kind: "eip6963", Name: binding.Wallet.Name, RDNS: binding.Wallet.RDNS}) {
		t.Fatalf("Session binding: %+v, %v", active, err)
	}
	closed, err := c.CloseSession(ctx, active.ID)
	if err != nil || closed.State != "closed" || closed.ClosedAt == "" {
		t.Fatal("close Session:", err)
	}
	if history, err := c.GetSession(ctx, active.ID); err != nil || history != closed {
		t.Fatal("closed history:", err)
	}
	if replay, err := c.CreateConnection(ctx, input); err != nil || replay != connected {
		t.Fatal("original connection after closure:", err)
	}
	if _, err := c.GetSession(ctx, NewConnectionID()); codeOf(t, err) != "session_not_found" {
		t.Fatal(err)
	}
	if _, err := c.GetConnection(ctx, NewConnectionID()); codeOf(t, err) != "connection_not_found" {
		t.Fatal(err)
	}
}

func TestConnectionClientChecksBeforeBearer(t *testing.T) {
	for _, minor := range []int{2, 3} {
		c := newTestClient(t)
		if err := credentials.Ensure(c.dir); err != nil {
			t.Fatal(err)
		}
		useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/info" || r.Header.Get("Authorization") != "" {
				t.Error("incompatible service received an authenticated request")
			}
			info := signedInfo(t, c, r.URL.Query().Get("challenge"))
			info.APIVersion.Minor = minor
			if minor == 2 {
				info.Capabilities = append(info.Capabilities, connectionCapability)
			}
			json.NewEncoder(w).Encode(sign(info))
		}))
		if _, err := c.GetConnection(t.Context(), NewConnectionID()); codeOf(t, err) != "incompatible_service" {
			t.Fatal(err)
		}
	}
}

func TestConnectionClientCanceledAndInvalidInput(t *testing.T) {
	c := newTestClient(t)
	if _, err := c.GetSession(t.Context(), "../status"); codeOf(t, err) != "bad_request" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.GetConnection(ctx, NewConnectionID()); codeOf(t, err) != "canceled" || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConnectionClientRejectsInvalidResponses(t *testing.T) {
	id := NewConnectionID()
	const stamp = "2026-01-01T00:00:00Z"
	pending := api.ConnectionResponse{ID: id, ChainID: "0x1", ClientName: "CLI", State: "pending", CreatedAt: stamp, ExpiresAt: stamp}
	active := api.SessionResponse{ID: id, ConnectionID: NewConnectionID(), ChainID: "0x1", Account: "0x" + strings.Repeat("ab", 20),
		Wallet: api.WalletResponse{Kind: "legacy", Name: "Test wallet"}, State: "active", CreatedAt: stamp, LastAccessAt: stamp}
	wrongID := pending
	wrongID.ID = NewConnectionID()
	missingSession := pending
	missingSession.State, missingSession.FinishedAt = "succeeded", stamp
	for _, tc := range []struct {
		name, action string
		body         any
		status       int
		want         string
	}{
		{"other resource", "get", wrongID, 200, "request_failed"},
		{"success without Session", "get", missingSession, 200, "request_failed"},
		{"cancel still pending", "cancel", pending, 200, "request_failed"},
		{"close still active", "close", active, 200, "request_failed"},
		{"storage failure", "get", api.ErrorResponse{Code: "storage_error", Message: "private-detail"}, 500, "storage_error"},
		{"mismatched status", "get", api.ErrorResponse{Code: "connection_conflict", Message: "private-detail"}, 500, "request_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := browserClientFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == "/api/v1/info" {
					return false
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(tc.body)
				return true
			})
			var err error
			switch tc.action {
			case "get":
				_, err = c.GetConnection(t.Context(), id)
			case "cancel":
				_, err = c.CancelConnection(t.Context(), id)
			case "close":
				_, err = c.CloseSession(t.Context(), id)
			}
			if codeOf(t, err) != tc.want || strings.Contains(err.Error(), "private-detail") {
				t.Fatal(err)
			}
		})
	}
}
