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

func reviewFixture(t *testing.T, s *Store) (app.TransactionReviewRef, app.ConnectionExecutionRef, app.Session, time.Time) {
	t.Helper()
	_, session := createLiveSession(t, s, 0)
	_, connection, now := executionFixture(t, s, 1)
	input := transactionInput(session)
	input.ID = connection.ConnectionID
	if _, err := s.CreateTransactionRequest(context.Background(), input, now); err != nil {
		t.Fatal(err)
	}
	session.LastAccessAt = now
	return app.TransactionReviewRef{TransactionID: input.ID, ID: strings.Repeat("a", 32), BrowserGrantID: connection.BrowserGrantID}, connection, session, now
}

func applyReview(t *testing.T, s *Store, ref app.TransactionReviewRef, cmd app.ReviewCommand, now time.Time, wantErr error) app.ReviewResult {
	t.Helper()
	result, err := s.applyTransactionReview(context.Background(), ref, cmd, func() time.Time { return now })
	if !errors.Is(err, wantErr) || err != nil && result != (app.ReviewResult{}) {
		t.Fatalf("%s: result %+v, error %v, want %v", cmd.Action, result, err, wantErr)
	}
	return result
}

func TestTransactionReviewOwnership(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, conn, session, now := reviewFixture(t, s)
	claim := app.ReviewCommand{Action: app.ClaimReview}
	claimExecution(t, s, conn, now)
	applyReview(t, s, ref, claim, now, app.ErrWalletBusy)
	if _, err := s.InterruptConnectionExecution(ctx, conn, now); err != nil {
		t.Fatal(err)
	}
	first := applyReview(t, s, ref, claim, now, nil)
	if first.Review.Phase != app.ExecutionReady || first.Invoke || applyReview(t, s, ref, claim, now.Add(time.Second), nil) != first {
		t.Fatal("claim retry changed ownership or liveness")
	}
	other := ref
	other.ID = strings.Repeat("b", 32)
	applyReview(t, s, other, claim, now, app.ErrTransactionReviewConflict)
	begin := app.ReviewCommand{Action: app.BeginPreparationCall, CallID: strings.Repeat("c", 32)}
	if !applyReview(t, s, ref, begin, now, nil).Invoke || applyReview(t, s, ref, begin, now, nil).Invoke {
		t.Fatal("preparation permission must be issued exactly once")
	}
	ret := app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: begin.CallID}
	if got := applyReview(t, s, ref, ret, now, nil); got.Review.Phase != app.ExecutionReady || got.Invoke {
		t.Fatal("return did not preserve the review", got)
	}
	if applyReview(t, s, ref, begin, now, nil).Invoke {
		t.Fatal("delayed preparation replay authorized another call")
	}
	bound := conn
	bound.ID = strings.Repeat("d", 32)
	if _, err := s.RecoverConnectionExecution(ctx, conn.ID, bound, now); !errors.Is(err, app.ErrWalletBusy) {
		t.Fatal("network switch released review ownership", err)
	}
	heartbeat := app.ReviewCommand{Action: app.HeartbeatReview}
	later := now.Add(time.Second)
	if got := applyReview(t, s, ref, heartbeat, later, nil); !got.Review.LastSeenAt.Equal(later) {
		t.Fatal(got)
	}
	applyReview(t, s, ref, heartbeat, later.Add(app.ConnectionExecutionHeartbeatTimeout), app.ErrTransactionReviewConflict)
	recover := app.ReviewCommand{Action: app.RecoverReview, PreviousID: ref.ID}
	recovered := applyReview(t, s, other, recover, later.Add(app.ConnectionExecutionHeartbeatTimeout), nil)
	if recovered.Review.ID != other.ID || recovered.Review.CallID != "" || recovered.Invoke {
		t.Fatal("recovery inherited old preparation", recovered)
	}
	applyReview(t, s, ref, ret, later, app.ErrTransactionReviewConflict)
	assertSessionUnchanged(t, s, session)
}

func TestTransactionReviewOutstandingPopup(t *testing.T) {
	ctx := context.Background()
	for _, end := range []string{"cancel", "close", "expire", "restart"} {
		t.Run(end, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			ref, conn, session, now := reviewFixture(t, s)
			applyReview(t, s, ref, app.ReviewCommand{Action: app.ClaimReview}, now, nil)
			begin := app.ReviewCommand{Action: app.BeginPreparationCall, CallID: strings.Repeat("c", 32)}
			applyReview(t, s, ref, begin, now, nil)
			var err error
			switch end {
			case "cancel":
				_, err = s.CancelTransactionRequest(ctx, ref.TransactionID, now)
			case "close":
				_, err = s.CloseSession(ctx, session.ID, now)
			case "expire":
				now = now.Add(app.TransactionRequestLifetime)
				_, err = s.GetTransactionRequest(ctx, ref.TransactionID, now)
			case "restart":
				s.Close()
				s = openTestStore(t, dir)
				_, err = s.MaintainConnectionState(ctx, now, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			active, err := s.GetActiveWalletRequest(ctx)
			if err != nil || active == nil || active.Kind != app.WalletTransaction || active.Phase != app.ExecutionInterruptedCalling {
				t.Fatal("lost outstanding popup", active, err)
			}
			applyReview(t, s, ref, app.ReviewCommand{Action: app.HeartbeatReview}, now, app.ErrTransactionReviewConflict)
			if end == "restart" {
				next := ref
				next.ID = strings.Repeat("b", 32)
				applyReview(t, s, next, app.ReviewCommand{Action: app.RecoverReview, PreviousID: ref.ID}, now, nil)
				applyReview(t, s, ref, app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: begin.CallID}, now, app.ErrTransactionReviewConflict)
				applyReview(t, s, next, app.ReviewCommand{Action: app.InterruptReview}, now, nil)
			} else {
				got := applyReview(t, s, ref, app.ReviewCommand{Action: app.ReturnPreparationCall, CallID: begin.CallID}, now, nil)
				if got.Review.Phase != app.ExecutionFinished {
					t.Fatal(got)
				}
			}
			if active, err := s.GetActiveWalletRequest(ctx); err != nil || active != nil {
				t.Fatal("completed popup still occupies slot", active, err)
			}
			if end != "expire" {
				claimExecution(t, s, conn, now)
			}
		})
	}
}

func TestTransactionReviewConcurrentClaim(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	first, second := openTestStore(t, dir), openTestStore(t, dir)
	ref, conn, _, now := reviewFixture(t, first)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := first.applyTransactionReview(ctx, ref, app.ReviewCommand{Action: app.ClaimReview}, func() time.Time { return now })
		results <- err
	}()
	go func() {
		<-start
		_, err := second.ClaimConnectionExecution(ctx, conn, now)
		results <- err
	}()
	close(start)
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, app.ErrWalletBusy)) || (b == nil && errors.Is(a, app.ErrWalletBusy))) {
		t.Fatal("expected one owner and one waiter", a, b)
	}
}

func TestTransactionReviewDeadlineUnderWriteLock(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "data")
	first, second := openTestStore(t, dir), openTestStore(t, dir)
	ref, _, _, now := reviewFixture(t, first)
	applyReview(t, first, ref, app.ReviewCommand{Action: app.ClaimReview}, now, nil)
	if _, err := second.db.ExecContext(ctx, "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(app.TransactionRequestLifetime)
	begin := app.ReviewCommand{Action: app.BeginPreparationCall, CallID: strings.Repeat("c", 32)}
	result, err := first.applyTransactionReview(ctx, ref, begin, func() time.Time {
		// If the clock were sampled before the lock, this second writer could begin.
		probe, err := second.db.BeginTx(ctx, nil)
		if err == nil {
			probe.Rollback()
			t.Fatal("approval deadline sampled before acquiring the SQLite write lock")
		}
		return deadline
	})
	if !errors.Is(err, app.ErrTransactionReviewConflict) || result.Invoke {
		t.Fatal("preparation permitted at deadline", result, err)
	}
	req, err := first.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || req.State != app.TransactionExpired || req.FinishedAt == nil || !req.FinishedAt.Equal(deadline) {
		t.Fatal("original expiry was not committed", req, err)
	}
}

func TestTransactionReviewRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, _, session, now := reviewFixture(t, s)
	before := applyReview(t, s, ref, app.ReviewCommand{Action: app.ClaimReview}, now, nil)
	_, err := s.db.Exec(`CREATE TABLE review_guard (id TEXT REFERENCES transaction_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_preparation AFTER UPDATE ON wallet_executions BEGIN INSERT INTO review_guard VALUES ('missing'); END`)
	if err != nil {
		t.Fatal(err)
	}
	begin := app.ReviewCommand{Action: app.BeginPreparationCall, CallID: strings.Repeat("d", 32)}
	if result, err := s.applyTransactionReview(ctx, ref, begin, func() time.Time { return now }); err == nil || result != (app.ReviewResult{}) {
		t.Fatal("failed commit granted permission", result, err)
	}
	got, err := s.GetTransactionReview(ctx, ref.TransactionID)
	if err != nil || got == nil || *got != before.Review {
		t.Fatal("failed preparation changed execution", got, err)
	}
	unknown := ref
	unknown.BrowserGrantID = strings.Repeat("e", 32)
	applyReview(t, s, unknown, begin, now, app.ErrInvalidBrowserCredential)
	if _, err := s.CloseSession(ctx, session.ID, now); err == nil {
		t.Fatal("review trigger failure allowed partial Session closure")
	}
	req, err := s.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || req.State != app.TransactionPending {
		t.Fatal("failed closure canceled the review", req, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_preparation"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseSession(ctx, req.SessionID, now); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTransactionReview(ctx, ref.TransactionID)
	if err != nil || got == nil || got.Phase != app.ExecutionFinished {
		t.Fatal("Session closure did not release a review without a popup", got, err)
	}
}
