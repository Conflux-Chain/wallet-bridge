//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func sendFixture(t *testing.T, s *Store) (app.TransactionReviewRef, app.TransactionSendInput, app.Session, time.Time) {
	t.Helper()
	ref, _, session, now := reviewFixture(t, s)
	applyReview(t, s, ref, app.ReviewCommand{Action: app.ClaimReview}, now, nil)
	return ref, app.TransactionSendInput{CallID: strings.Repeat("d", 32),
		Binding: app.ConnectionBinding{Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID}}, session, now
}

func grantAt(s *Store, ref app.TransactionReviewRef, input app.TransactionSendInput, now time.Time) (*app.Transaction, error) {
	return s.grantTransactionSend(context.Background(), ref, input, func() time.Time { return now })
}

func TestTransactionSendReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	ref, input, session, now := sendFixture(t, s)
	req, err := s.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := grantAt(s, ref, input, now)
	if err != nil || payload == nil || *payload != req.Transaction {
		t.Fatal("first permission must carry the frozen payload", payload, err)
	}
	s.Close() // The first caller lost its response.
	s = openTestStore(t, dir)
	later := req.ExpiresAt.Add(time.Second)
	counts, err := s.MaintainConnectionState(ctx, later, true)
	if err != nil || counts.Sessions != 0 || counts.TransactionRequests != 0 {
		t.Fatal("recovery expired an outstanding send", counts, err)
	}
	assertSessionUnchanged(t, s, session)
	active, err := s.GetActiveWalletRequest(ctx)
	if err != nil || active == nil || active.ID != req.ID || active.Phase != app.ExecutionInterruptedCalling {
		t.Fatal("recovery lost the wallet call", active, err)
	}
	other := ref
	other.ID = strings.Repeat("b", 32)
	applyReview(t, s, other, app.ReviewCommand{Action: app.RecoverReview, PreviousID: ref.ID}, later, app.ErrTransactionReviewConflict)
	applyReview(t, s, ref, app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: input.CallID}, later, app.ErrTransactionReviewConflict)
	if current, err := s.AccessSession(ctx, session.ID, later); err != nil || current.ClosedAt != nil {
		t.Fatal("client access reclaimed a protected Session", current, err)
	}
	if _, err := s.CloseSession(ctx, session.ID, later); err != nil {
		t.Fatal(err)
	}
	got, err := s.CancelTransactionRequest(ctx, req.ID, later)
	if err != nil || got.State != app.TransactionAwaitingWallet || got.FinishedAt != nil || got.HandedOffAt == nil || !got.HandedOffAt.Equal(now) {
		t.Fatal("close/cancel after handoff", got, err)
	}
	if payload, err := grantAt(s, ref, input, later); err != nil || payload != nil {
		t.Fatal("lost response produced a second permission", payload, err)
	}
	input.CallID = strings.Repeat("c", 32)
	if payload, err := grantAt(s, ref, input, later); !errors.Is(err, app.ErrTransactionReviewConflict) || payload != nil {
		t.Fatal("new call ID after handoff", payload, err)
	}
}

func TestTransactionSendGuards(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, input, _, now := sendFixture(t, s)
	denied := func(owner app.TransactionReviewRef, send app.TransactionSendInput, want error) {
		t.Helper()
		if payload, err := grantAt(s, owner, send, now); payload != nil || !errors.Is(err, want) {
			t.Fatalf("got payload %v and error %v, want %v", payload, err, want)
		}
	}
	other := ref
	other.ID = strings.Repeat("e", 32)
	denied(other, input, app.ErrTransactionReviewConflict)
	_, otherGrant, _ := executionFixture(t, s, 2)
	other = ref
	other.BrowserGrantID = otherGrant.BrowserGrantID
	denied(other, input, app.ErrTransactionReviewConflict)
	for _, field := range []string{"wallet", "account", "chain"} {
		mismatch := input
		switch field {
		case "wallet":
			mismatch.Binding.Wallet.RDNS += ".other"
		case "account":
			mismatch.Binding.Account = "0x" + strings.Repeat("f", 40)
		case "chain":
			mismatch.Binding.ChainID += "1"
		}
		denied(ref, mismatch, app.ErrInvalidSession)
	}
	prep := app.ReviewCommand{Action: app.BeginPreparationCall, CallID: strings.Repeat("c", 32)}
	applyReview(t, s, ref, prep, now, nil)
	denied(ref, input, app.ErrTransactionReviewConflict)
	applyReview(t, s, ref, app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: prep.CallID}, now, nil)
	denied(ref, input, app.ErrTransactionReviewConflict) // stale click from before preparation
	input.PreviousCallID = prep.CallID
	if payload, err := grantAt(s, ref, input, now); err != nil || payload == nil {
		t.Fatal("handoff after the confirmed preparation", payload, err)
	}
	if err := s.RevokeBrowserGrant(context.Background(), ref.BrowserGrantID, now); err != nil {
		t.Fatal(err)
	}
	denied(ref, input, app.ErrInvalidBrowserCredential)
}

func TestTransactionSendUpgrade(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	old := openConnectionSchema(t, dir, 8)
	_, session := seedMigrationSession(t, old, 0)
	_, conn, at := executionFixture(t, old, 1)
	ref := app.TransactionReviewRef{TransactionID: session.ID, ID: conn.ID, BrowserGrantID: conn.BrowserGrantID}
	prepID := strings.Repeat("c", 32)
	_, err := old.db.Exec(`INSERT INTO transaction_requests
(id, session_id, client_name, intent, from_address, to_address, chain_id, value, data, gas, gas_price, max_fee_per_gas, max_priority_fee_per_gas, created_at, expires_at)
SELECT id, id, 'CLI', '', account, account, chain_id, '0x0', '0x', '', '', '', '', ?, ? FROM sessions WHERE id = ?`,
		at.UnixMilli(), at.Add(app.TransactionRequestLifetime).UnixMilli(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.db.Exec(`INSERT INTO wallet_executions (transaction_id, id, browser_grant_id, phase, last_seen_at, call_id)
VALUES (?, ?, ?, 'calling', ?, ?)`, ref.TransactionID, ref.ID, ref.BrowserGrantID, at.UnixMilli(), prepID)
	if err != nil {
		t.Fatal(err)
	}
	broken := append([]string{}, migrations[:9]...)
	broken[8] += "INVALID SQL"
	if err := migrate(ctx, old.db, broken); err == nil {
		t.Fatal("failed upgrade committed")
	}
	var version int
	if err := old.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 8 {
		t.Fatal("failed upgrade advanced schema", version, err)
	}
	old.Close()
	s := openTestStore(t, dir)
	got := applyReview(t, s, ref, app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: prepID}, at, nil)
	if got.Review.Phase != app.ExecutionReady {
		t.Fatal("upgrade lost the pending preparation", got)
	}
	input := app.TransactionSendInput{CallID: strings.Repeat("d", 32), PreviousCallID: prepID,
		Binding: app.ConnectionBinding{Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID}}
	if payload, err := grantAt(s, ref, input, at); err != nil || payload == nil || payload.To != session.Account {
		t.Fatal("upgraded review did not retain its transaction", payload, err)
	}
}

func TestTransactionSendDecisionOrder(t *testing.T) {
	ctx := context.Background()
	for _, first := range []string{"cancel", "close", "deadline", "send before deadline"} {
		t.Run(first, func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			ref, input, session, created := sendFixture(t, s)
			deadline := created.Add(app.TransactionRequestLifetime)
			at := deadline.Add(-time.Millisecond)
			// Keep the original review live through the last eligible millisecond.
			for seen := created.Add(time.Second); seen.Before(deadline); seen = seen.Add(app.ConnectionExecutionHeartbeatTimeout / 2) {
				applyReview(t, s, ref, app.ReviewCommand{Action: app.HeartbeatReview}, seen, nil)
			}
			state := app.TransactionCanceled
			var err error
			switch first {
			case "cancel":
				_, err = s.CancelTransactionRequest(ctx, ref.TransactionID, at)
			case "close":
				_, err = s.CloseSession(ctx, session.ID, at)
			case "deadline":
				at, state = deadline, app.TransactionExpired
			case "send before deadline":
				state = app.TransactionAwaitingWallet
			}
			if err != nil {
				t.Fatal(err)
			}
			payload, err := grantAt(s, ref, input, at)
			if state == app.TransactionAwaitingWallet {
				if err != nil || payload == nil {
					t.Fatal("last millisecond should allow handoff", payload, err)
				}
			} else if !errors.Is(err, app.ErrTransactionReviewConflict) || payload != nil {
				t.Fatal("late handoff", payload, err)
			}
			req, err := s.CancelTransactionRequest(ctx, ref.TransactionID, deadline)
			if err != nil || req.State != state {
				t.Fatal("wrong decision", req, err)
			}
		})
	}
}

func TestTransactionSendConcurrent(t *testing.T) {
	ctx := context.Background()
	for _, rival := range []string{"replay", "cancel", "close"} {
		t.Run(rival, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			first, second := openTestStore(t, dir), openTestStore(t, dir)
			ref, input, session, now := sendFixture(t, first)
			start := make(chan struct{})
			type result struct {
				payload *app.Transaction
				err     error
			}
			done := make(chan result, 1)
			go func() {
				<-start
				payload, err := grantAt(first, ref, input, now)
				done <- result{payload, err}
			}()
			close(start)
			if rival == "replay" {
				payload, err := grantAt(second, ref, input, now)
				send := <-done
				if err != nil || send.err != nil || (payload == nil) == (send.payload == nil) {
					t.Fatal("concurrent retries must return one permission", payload, err, send)
				}
				return
			}
			var err error
			if rival == "close" {
				_, err = second.CloseSession(ctx, session.ID, now)
			} else {
				_, err = second.CancelTransactionRequest(ctx, ref.TransactionID, now)
			}
			send := <-done
			if err != nil {
				t.Fatal(err)
			}
			req, err := second.GetTransactionRequest(ctx, ref.TransactionID, now)
			if err != nil {
				t.Fatal(err)
			}
			switch req.State {
			case app.TransactionAwaitingWallet:
				if send.err != nil || send.payload == nil || *send.payload != req.Transaction {
					t.Fatal("committed handoff lost its permission", send)
				}
			case app.TransactionCanceled:
				if !errors.Is(send.err, app.ErrTransactionReviewConflict) || send.payload != nil {
					t.Fatal("canceled request acquired a permission", send)
				}
			default:
				t.Fatal("neither decision took effect", req.State)
			}
		})
	}
}

func TestTransactionSendCommitFailure(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, input, _, now := sendFixture(t, s)
	_, err := s.db.Exec(`CREATE TABLE send_guard (id TEXT REFERENCES transaction_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_send AFTER UPDATE OF phase ON wallet_executions BEGIN INSERT INTO send_guard VALUES ('missing'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if payload, err := grantAt(s, ref, input, now); err == nil || payload != nil {
		t.Fatal("failed commit granted permission", payload, err)
	}
	req, err := s.GetTransactionRequest(context.Background(), ref.TransactionID, now)
	if err != nil || req.State != app.TransactionPending || req.HandedOffAt != nil {
		t.Fatal("failed commit left a handoff", req, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_send"); err != nil {
		t.Fatal(err)
	}
	if payload, err := grantAt(s, ref, input, now); err != nil || payload == nil {
		t.Fatal("rolled back attempt consumed the permission", payload, err)
	}
}
