//go:build windows || darwin

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
)

func transactionInput(t *testing.T, core *app.ConnectionService) (api.TransactionCreateRequest, app.ConnectionBinding) {
	t.Helper()
	binding := app.ConnectionBinding{Account: "0x" + strings.Repeat("ab", 20), ChainID: "0x47",
		Wallet: app.WalletDescriptor{Kind: app.WalletEIP6963, Name: "Test wallet", RDNS: "test.wallet"}}
	conn, err := core.Create(t.Context(), app.ConnectionInput{ID: NewRequestID(), ClientName: "Fixture", ChainID: binding.ChainID})
	if err != nil {
		t.Fatal(err)
	}
	conn, err = core.Complete(t.Context(), conn.ID, binding)
	if err != nil {
		t.Fatal(err)
	}
	address, _ := json.Marshal(binding.Account)
	return api.TransactionCreateRequest{ID: NewRequestID(), SessionID: conn.SessionID, ClientName: "CLI",
		Transaction: map[string]json.RawMessage{"from": address, "to": address, "chainId": json.RawMessage(`"71"`)}}, binding
}

func TestTransactionClientRetryAfterCommit(t *testing.T) {
	var attempts atomic.Int32
	c, core, _ := connectionClientFixture(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == transactionsPath && attempts.Add(1) == 1 {
				recorder := httptest.NewRecorder()
				next.ServeHTTP(recorder, r)
				if recorder.Code != http.StatusOK {
					t.Errorf("create before response loss: HTTP %d", recorder.Code)
				}
				panic(http.ErrAbortHandler)
			}
			next.ServeHTTP(w, r)
		})
	})
	input, _ := transactionInput(t, core)
	input.Intent = strings.Repeat("<", 200_000)
	ctx := t.Context()
	if _, err := c.CreateTransaction(ctx, input); codeOf(t, err) != "request_failed" || attempts.Load() != 1 {
		t.Fatalf("lost response: %v, attempts=%d", err, attempts.Load())
	}
	first, err := c.GetTransaction(ctx, input.ID)
	if err != nil || first.State != "pending" || first.Intent != input.Intent || first.Transaction.Value != "0x0" || first.Transaction.Data != "0x" {
		t.Fatal("read original transaction:", err)
	}
	if got, err := c.CreateTransaction(ctx, input); err != nil || got != first {
		t.Fatal("create replay:", err)
	}
	changed := input
	changed.Intent = "different"
	if _, err := c.CreateTransaction(ctx, changed); codeOf(t, err) != "transaction_conflict" {
		t.Fatal(err)
	}
	for _, field := range []string{"nonce", "type"} {
		input.Transaction[field] = json.RawMessage("0")
		if _, err := c.CreateTransaction(ctx, input); codeOf(t, err) != "bad_request" || !strings.Contains(err.Error(), "Omit "+field) {
			t.Fatal(err)
		}
		delete(input.Transaction, field)
	}
	canceled, err := c.CancelTransaction(ctx, input.ID)
	if err != nil || canceled.State != "canceled" {
		t.Fatal("cancel:", err)
	}
	if _, err := c.CloseSession(ctx, input.SessionID); err != nil {
		t.Fatal(err)
	}
	if got, err := c.CreateTransaction(ctx, input); err != nil || got != canceled {
		t.Fatal("closed Session replay:", err)
	}
	input.ID = NewRequestID()
	if _, err := c.CreateTransaction(ctx, input); codeOf(t, err) != "session_closed" {
		t.Fatal(err)
	}
	if _, err := c.GetTransaction(ctx, input.ID); codeOf(t, err) != "transaction_not_found" {
		t.Fatal(err)
	}
}

func TestTransactionClientLateHash(t *testing.T) {
	c, core, db := connectionClientFixture(t, nil)
	input, binding := transactionInput(t, core)
	ctx := t.Context()
	if _, err := c.CreateTransaction(ctx, input); err != nil {
		t.Fatal(err)
	}
	auth, err := app.NewBrowserAuthorization(db)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := auth.Issue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := auth.Authorize(ctx, ticket.Token)
	if err != nil {
		t.Fatal(err)
	}
	ref := app.TransactionReviewRef{TransactionID: input.ID, ID: NewRequestID(), BrowserGrantID: grant.Grant.ID}
	if _, err := db.ApplyTransactionReview(ctx, ref, app.ReviewCommand{Action: app.ClaimReview}); err != nil {
		t.Fatal(err)
	}
	call := app.TransactionSendRef{TransactionReviewRef: ref, CallID: NewRequestID()}
	if tx, err := db.GrantTransactionSend(ctx, ref, app.TransactionSendInput{CallID: call.CallID, Binding: binding}); err != nil || tx == nil {
		t.Fatal("handoff:", err)
	}
	if got, err := c.CancelTransaction(ctx, input.ID); err != nil || got.State != "awaiting_wallet" {
		t.Fatal("cancel after handoff:", err)
	}
	if _, err := db.RecordTransactionError(ctx, call, 4900); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetTransaction(ctx, input.ID); err != nil || got.State != "uncertain" || got.TxHash != "" {
		t.Fatal("uncertain:", err)
	}
	hash := "0x" + strings.Repeat("de", 32)
	if _, err := db.RecordTransactionHash(ctx, call, hash); err != nil {
		t.Fatal(err)
	}
	if got, err := c.GetTransaction(ctx, input.ID); err != nil || got.State != "sent" || got.TxHash != hash || got.Transaction.ChainID != binding.ChainID {
		t.Fatal("late hash:", err)
	}
}

func TestTransactionClientCompatibility(t *testing.T) {
	for _, minor := range []int{7, 8} {
		c := newTestClient(t)
		if err := credentials.Ensure(c.dir); err != nil {
			t.Fatal(err)
		}
		useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/info" || r.Header.Get("Authorization") != "" {
				t.Error("incompatible service received authenticated request")
			}
			info := signedInfo(t, c, r.URL.Query().Get("challenge"))
			info.APIVersion.Minor = minor
			if minor == 7 {
				info.Capabilities = append(info.Capabilities, transactionCapability)
			}
			json.NewEncoder(w).Encode(sign(info))
		}))
		if _, err := c.GetTransaction(t.Context(), NewRequestID()); codeOf(t, err) != "incompatible_service" {
			t.Fatal(err)
		}
	}
}

func TestTransactionClientRejectsInvalidResponse(t *testing.T) {
	const stamp = "2026-01-01T00:00:00Z"
	id, account := NewRequestID(), "0x"+strings.Repeat("ab", 20)
	pending := api.TransactionResponse{ID: id, SessionID: NewRequestID(), ClientName: "CLI", State: "pending", CreatedAt: stamp, ExpiresAt: stamp,
		Transaction: api.TransactionPayload{From: account, To: account, ChainID: "0x1", Data: "0x", Value: "0x0"}}
	for _, tc := range []struct {
		name   string
		change func(*api.TransactionResponse)
	}{
		{"different request", func(r *api.TransactionResponse) { r.ID = NewRequestID() }},
		{"missing amount", func(r *api.TransactionResponse) { r.Transaction.Value = "" }},
		{"sent without hash", func(r *api.TransactionResponse) { r.State, r.HandedOffAt, r.FinishedAt = "sent", stamp, stamp }},
		{"uncertain with hash", func(r *api.TransactionResponse) {
			r.State, r.HandedOffAt, r.FinishedAt, r.TxHash = "uncertain", stamp, stamp, "0x"+strings.Repeat("12", 32)
		}},
		{"cancel still pending", func(*api.TransactionResponse) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := pending
			tc.change(&reply)
			c, _ := browserClientFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == "/api/v1/info" {
					return false
				}
				json.NewEncoder(w).Encode(reply)
				return true
			})
			var err error
			if tc.name == "cancel still pending" {
				_, err = c.CancelTransaction(t.Context(), id)
			} else {
				_, err = c.GetTransaction(t.Context(), id)
			}
			if codeOf(t, err) != "request_failed" {
				t.Fatal(err)
			}
		})
	}
}
