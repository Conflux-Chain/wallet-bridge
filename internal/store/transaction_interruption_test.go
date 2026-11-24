//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func TestTransactionInterruptionRecovery(t *testing.T) {
	ctx := context.Background()
	for _, cause := range []string{"page", "heartbeat", "restart", "revocation", "previous version"} {
		t.Run(cause, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			ref, session, sent := resultFixture(t, s)
			at := sent.Add(time.Second)
			switch cause {
			case "page":
				applyReview(t, s, ref.TransactionReviewRef, app.ReviewCommand{Action: app.InterruptReview}, at, nil)
			case "heartbeat":
				at = sent.Add(app.ConnectionExecutionHeartbeatTimeout)
				if _, err := s.MaintainConnectionState(ctx, at.Add(-time.Millisecond), false); err != nil {
					t.Fatal(err)
				}
				req, err := s.GetTransactionRequest(ctx, ref.TransactionID, at)
				if err != nil || req.State != app.TransactionAwaitingWallet {
					t.Fatal("interrupted before heartbeat deadline", req, err)
				}
			case "revocation":
				if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, at); err != nil {
					t.Fatal(err)
				}
			case "previous version":
				// v10 could persist the interrupted phase without a result row.
				if _, err := s.db.Exec("UPDATE wallet_executions SET phase='interrupted_calling' WHERE transaction_id=?", ref.TransactionID); err != nil {
					t.Fatal(err)
				}
			}
			if cause != "page" {
				restart := cause == "restart" || cause == "previous version"
				if restart {
					s.Close()
					s = openTestStore(t, dir)
				}
				if _, err := s.MaintainConnectionState(ctx, at, restart); err != nil {
					t.Fatal(err)
				}
			}
			req, err := s.GetTransactionRequest(ctx, ref.TransactionID, at)
			if err != nil || req.State != app.TransactionUncertain || req.FinishedAt == nil || !req.FinishedAt.Equal(at) {
				t.Fatal("missing interruption result", req, err)
			}
			later := at.Add(app.SessionIdleLifetime)
			if counts, err := s.MaintainConnectionState(ctx, later, false); err != nil || counts.Sessions != 0 {
				t.Fatal("idle collection during unresolved call", counts, err)
			}
			assertSessionUnchanged(t, s, session)
			active, err := s.GetActiveWalletRequest(ctx)
			if err != nil || active == nil || active.ID != req.ID || active.Phase != app.ExecutionInterruptedCalling {
				t.Fatal("unresolved popup lost ownership", active, err)
			}
			if got, err := s.GetTransactionRequest(ctx, req.ID, later); err != nil || got.FinishedAt == nil || !got.FinishedAt.Equal(at) {
				t.Fatal("maintenance rewrote the interruption time", got, err)
			}
		})
	}
}

func TestTransactionInterruptionAcknowledge(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, session, sent := resultFixture(t, s)
	_, other, _ := executionFixture(t, s, 3)
	at := sent.Add(4 * time.Second)
	ack := func() (app.TransactionRequest, error) {
		return s.acknowledgeTransactionInterruption(ctx, ref.TransactionID, other.BrowserGrantID, fixedDecisionTime(at))
	}
	if _, err := ack(); !errors.Is(err, app.ErrTransactionReviewConflict) {
		t.Fatal("released a live call", err)
	}
	applyReview(t, s, ref.TransactionReviewRef, app.ReviewCommand{Action: app.InterruptReview}, at, nil)
	if _, err := s.ClaimConnectionExecution(ctx, other, at); !errors.Is(err, app.ErrWalletBusy) {
		t.Fatal("another request claimed the unresolved popup", err)
	}
	if _, err := s.acknowledgeTransactionInterruption(ctx, ref.TransactionID, "000000000000000000000000000000ff", fixedDecisionTime(at)); !errors.Is(err, app.ErrInvalidBrowserCredential) {
		t.Fatal("unauthorized acknowledgment", err)
	}
	req, err := ack()
	if err != nil || req.State != app.TransactionUncertain || req.FinishedAt == nil || !req.FinishedAt.Equal(at) {
		t.Fatal("authorized acknowledgment", req, err)
	}
	claimExecution(t, s, other, at)
	if _, err := ack(); err != nil {
		t.Fatal("acknowledgment retry", err)
	}
	if active, err := s.GetActiveWalletRequest(ctx); err != nil || active == nil || active.Kind != app.WalletConnection || active.ID != other.ConnectionID {
		t.Fatal("stale acknowledgment released another request", active, err)
	}
	foreign := ref
	foreign.BrowserGrantID = other.BrowserGrantID
	if _, err := s.recordTransactionHash(ctx, foreign, walletHash, fixedDecisionTime(at)); !errors.Is(err, app.ErrTransactionReviewConflict) {
		t.Fatal("acknowledgment transferred wallet result ownership", err)
	}
	if payload, err := grantAt(s, ref.TransactionReviewRef, app.TransactionSendInput{CallID: ref.CallID}, at); err != nil || payload != nil {
		t.Fatal("acknowledgment enabled resend", payload, err)
	}
	later := at.Add(app.SessionIdleLifetime)
	if got, err := s.AccessSession(ctx, session.ID, later); err != nil || got.ClosedAt == nil {
		t.Fatal("released wait prevented idle collection", got, err)
	}
	if req, err := s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(later)); err != nil || req.TxHash != walletHash {
		t.Fatal("late wallet reply", req, err)
	}
	req, err = s.acknowledgeTransactionInterruption(ctx, ref.TransactionID, other.BrowserGrantID, fixedDecisionTime(later))
	if err != nil || req.State != app.TransactionSent || req.TxHash != walletHash {
		t.Fatal("stale acknowledgment lost hash", req, err)
	}
}

func TestTransactionInterruptionPreparation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, input, _, now := sendFixture(t, s)
	applyReview(t, s, ref, app.ReviewCommand{Action: app.BeginPreparationCall, CallID: input.CallID}, now, nil)
	applyReview(t, s, ref, app.ReviewCommand{Action: app.InterruptReview}, now, nil)
	if _, err := s.acknowledgeTransactionInterruption(ctx, ref.TransactionID, ref.BrowserGrantID, fixedDecisionTime(now)); !errors.Is(err, app.ErrTransactionReviewConflict) {
		t.Fatal("send acknowledgment released a preparation call", err)
	}
	req, err := s.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || req.State != app.TransactionPending || req.FinishedAt != nil {
		t.Fatal("preparation created a send result", req, err)
	}
}

func TestTransactionInterruptionRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, _, now := resultFixture(t, s)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_interruption BEFORE INSERT ON transaction_results
BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MaintainConnectionState(ctx, now, true); err == nil {
		t.Fatal("restart acknowledged a partial interruption")
	}
	review, err := s.GetTransactionReview(ctx, ref.TransactionID)
	if err != nil || review == nil || review.Phase != app.ExecutionCalling {
		t.Fatal("failed interruption changed the execution", review, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_interruption"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MaintainConnectionState(ctx, now, true); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionInterruptionConcurrentHash(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	first, second := openTestStore(t, dir), openTestStore(t, dir)
	ref, _, now := resultFixture(t, first)
	applyReview(t, first, ref.TransactionReviewRef, app.ReviewCommand{Action: app.InterruptReview}, now, nil)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		_, err := second.acknowledgeTransactionInterruption(ctx, ref.TransactionID, ref.BrowserGrantID, fixedDecisionTime(now))
		done <- err
	}()
	close(start)
	_, sendErr := first.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now))
	if ackErr := <-done; sendErr != nil || ackErr != nil {
		t.Fatal("racing wallet reply and acknowledgment", sendErr, ackErr)
	}
	req, err := second.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || req.State != app.TransactionSent || req.TxHash != walletHash {
		t.Fatal("lost the wallet reply", req, err)
	}
	if active, err := first.GetActiveWalletRequest(ctx); err != nil || active != nil {
		t.Fatal("returned call still occupies wallet", active, err)
	}
}
