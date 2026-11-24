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

var walletHash = "0x" + strings.Repeat("a1", 32)

func fixedDecisionTime(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func resultFixture(t *testing.T, s *Store) (app.TransactionSendRef, app.Session, time.Time) {
	t.Helper()
	ref, input, session, now := sendFixture(t, s)
	if payload, err := grantAt(s, ref, input, now); err != nil || payload == nil {
		t.Fatal("send handoff", payload, err)
	}
	return app.TransactionSendRef{TransactionReviewRef: ref, CallID: input.CallID}, session, now
}

func TestTransactionResultLateHash(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	ref, session, now := resultFixture(t, s)
	unknown, err := s.recordTransactionError(ctx, ref, 4900, fixedDecisionTime(now))
	if err != nil || unknown.State != app.TransactionUncertain || unknown.TxHash != "" || unknown.FinishedAt == nil {
		t.Fatal("disconnect", unknown, err)
	}
	if active, err := s.GetActiveWalletRequest(ctx); err != nil || active != nil {
		t.Fatal("returned call still occupies wallet", active, err)
	}
	// A different request now owns the wallet. Reporting the late hash must
	// neither release that request nor depend on the old Session being active.
	_, other, _ := executionFixture(t, s, 3)
	later := now.Add(4 * time.Second)
	claimExecution(t, s, other, later)
	if _, err := s.CloseSession(ctx, session.ID, later); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTestStore(t, dir)
	got, err := s.recordTransactionHash(ctx, ref, "0x"+strings.ToUpper(walletHash[2:]), fixedDecisionTime(later))
	if err != nil || got.State != app.TransactionSent || got.TxHash != walletHash || got.FinishedAt == nil || !got.FinishedAt.Equal(later) {
		t.Fatal("late hash", got, err)
	}
	if active, err := s.GetActiveWalletRequest(ctx); err != nil || active == nil || active.Kind != app.WalletConnection || active.ID != other.ConnectionID {
		t.Fatal("late hash released a different call", active, err)
	}
	if got, err = s.recordTransactionError(ctx, ref, 4900, fixedDecisionTime(later)); err != nil || got.TxHash != walletHash {
		t.Fatal("old error overwrote known hash", got, err)
	}
	got, err = s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(later.Add(time.Second)))
	if err != nil || got.FinishedAt == nil || !got.FinishedAt.Equal(later) {
		t.Fatal("retry changed result time", got, err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM transaction_results").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate activity", count, err)
	}
}

func TestTransactionResultRejected(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, session, now := resultFixture(t, s)
	got, err := s.recordTransactionError(ctx, ref, 4001, fixedDecisionTime(now))
	if err != nil || got.State != app.TransactionRejected || got.TxHash != "" {
		t.Fatal("provider rejection", got, err)
	}
	got, err = s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now))
	if !errors.Is(err, app.ErrTransactionResultConflict) || got.State != app.TransactionRejected {
		t.Fatal("rejection overwritten", got, err)
	}
	session, err = s.AccessSession(ctx, session.ID, now.Add(app.SessionIdleLifetime))
	if err != nil || session.ClosedAt == nil {
		t.Fatal("returned call prevented idle collection", session, err)
	}
	input := transactionInput(session)
	input.ID = ref.TransactionID
	got, err = s.CreateTransactionRequest(ctx, input, now)
	if err != nil || got.State != app.TransactionRejected {
		t.Fatal("creation retry lost the result", got, err)
	}
}

func TestTransactionResultReauthorization(t *testing.T) {
	ctx := context.Background()
	for _, end := range []string{"revoke", "expiry"} {
		t.Run(end, func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			ref, _, now := resultFixture(t, s)
			issue := func(index int, at time.Time) string {
				g, digest := fixture(index)
				g.IssuedAt, g.ExpiresAt = at, at.Add(app.BrowserGrantLifetime)
				if err := s.CreateBrowserGrant(ctx, g, digest); err != nil {
					t.Fatal(err)
				}
				return g.ID
			}
			other := ref
			other.BrowserGrantID = issue(10, now)
			if _, err := s.recordTransactionHash(ctx, other, walletHash, fixedDecisionTime(now)); !errors.Is(err, app.ErrTransactionReviewConflict) {
				t.Fatal("unrelated grant admitted", err)
			}
			at := now.Add(time.Hour)
			if end == "expiry" {
				old, _ := fixture(1)
				at = old.ExpiresAt
			} else if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, at); err != nil {
				t.Fatal(err)
			}
			if _, err := s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(at)); !errors.Is(err, app.ErrInvalidBrowserCredential) {
				t.Fatal("invalid grant admitted", err)
			}
			ref.BrowserGrantID = issue(11, at)
			wrong := ref
			wrong.CallID = strings.Repeat("f", 32)
			if _, err := s.recordTransactionHash(ctx, wrong, walletHash, fixedDecisionTime(at)); !errors.Is(err, app.ErrTransactionReviewConflict) {
				t.Fatal("wrong call admitted", err)
			}
			got, err := s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(at))
			if err != nil || got.TxHash != walletHash {
				t.Fatal("reauthorized report", got, err)
			}
			if payload, err := grantAt(s, ref.TransactionReviewRef, app.TransactionSendInput{CallID: ref.CallID}, at); payload != nil || !errors.Is(err, app.ErrTransactionReviewConflict) {
				t.Fatal("reauthorization acquired send ownership", payload, err)
			}
		})
	}
}

func TestTransactionResultRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ref, _, now := resultFixture(t, s)
	if _, err := s.recordTransactionHash(ctx, ref, "0xbeef", fixedDecisionTime(now)); !errors.Is(err, app.ErrInvalidTransaction) {
		t.Fatal("malformed hash accepted", err)
	}
	_, err := s.db.Exec(`CREATE TABLE result_guard (id TEXT REFERENCES sessions(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_result AFTER INSERT ON transaction_results BEGIN INSERT INTO result_guard VALUES ('missing'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now)); err == nil || got.ID != "" {
		t.Fatal("acknowledged failed commit", got, err)
	}
	got, err := s.GetTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || got.State != app.TransactionAwaitingWallet || got.TxHash != "" {
		t.Fatal("failed result escaped rollback", got, err)
	}
	if active, err := s.GetActiveWalletRequest(ctx); err != nil || active == nil || active.Phase != app.ExecutionCalling {
		t.Fatal("rollback released wallet", active, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_result"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now)); err != nil || got.TxHash != walletHash {
		t.Fatal("retry failed", got, err)
	}
}

func TestTransactionResultConcurrentHashes(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	first, second := openTestStore(t, dir), openTestStore(t, dir)
	ref, _, now := resultFixture(t, first)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		_, err := first.recordTransactionHash(ctx, ref, walletHash, fixedDecisionTime(now))
		done <- err
	}()
	otherHash := "0x" + strings.Repeat("bc", 32)
	close(start)
	_, secondErr := second.recordTransactionHash(ctx, ref, otherHash, fixedDecisionTime(now))
	firstErr := <-done
	want := walletHash
	switch {
	case firstErr == nil && errors.Is(secondErr, app.ErrTransactionResultConflict):
	case secondErr == nil && errors.Is(firstErr, app.ErrTransactionResultConflict):
		want = otherHash
	default:
		t.Fatal("expected one accepted hash", firstErr, secondErr)
	}
	got, err := first.CancelTransactionRequest(ctx, ref.TransactionID, now)
	if err != nil || got.State != app.TransactionSent || got.TxHash != want {
		t.Fatal("durable winner", got, err)
	}
}
