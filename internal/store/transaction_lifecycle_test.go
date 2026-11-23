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

func assertTransactionState(t *testing.T, req app.TransactionRequest, state app.TransactionState, finished time.Time) {
	t.Helper()
	if req.State != state || (req.FinishedAt == nil) != finished.IsZero() ||
		req.FinishedAt != nil && !req.FinishedAt.Equal(finished) {
		t.Fatalf("request %s: got %s at %v, want %s at %v", req.ID, req.State, req.FinishedAt, state, finished)
	}
}

func TestTransactionLifecycleDeadline(t *testing.T) {
	ctx := context.Background()
	for _, first := range []string{"cancel before", "cancel at", "poll at", "replay at", "restart"} {
		t.Run(first, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			_, session := createLiveSession(t, s, 0)
			input := transactionInput(session)
			req, err := s.CreateTransactionRequest(ctx, input, session.LastAccessAt)
			if err != nil {
				t.Fatal(err)
			}
			before := req.ExpiresAt.Add(-time.Millisecond)
			got, err := s.GetTransactionRequest(ctx, req.ID, before)
			if err != nil || got != req {
				t.Fatal("poll near deadline", got, err)
			}
			assertSessionUnchanged(t, s, session)
			state, finished := app.TransactionExpired, req.ExpiresAt
			switch first {
			case "cancel before":
				state, finished = app.TransactionCanceled, before
				got, err = s.CancelTransactionRequest(ctx, req.ID, before)
			case "cancel at":
				got, err = s.CancelTransactionRequest(ctx, req.ID, finished)
			case "poll at":
				got, err = s.GetTransactionRequest(ctx, req.ID, finished)
			case "replay at":
				got, err = s.CreateTransactionRequest(ctx, input, finished)
			case "restart":
				s.Close()
				s = openTestStore(t, dir)
				counts, restoreErr := s.MaintainConnectionState(ctx, finished.Add(time.Hour), true)
				if restoreErr != nil || counts.TransactionRequests != 1 || counts.Sessions != 1 {
					t.Fatal("startup recovery", counts, restoreErr)
				}
				got, err = s.GetTransactionRequest(ctx, req.ID, before)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertTransactionState(t, got, state, finished)
			s.Close()
			s = openTestStore(t, dir)
			for _, at := range []time.Time{before, req.ExpiresAt.Add(time.Hour)} {
				got, err = s.CancelTransactionRequest(ctx, req.ID, at)
				if err != nil {
					t.Fatal(err)
				}
				assertTransactionState(t, got, state, finished)
			}
		})
	}
}

func TestTransactionLifecycleCloseRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, session := createLiveSession(t, s, 0)
	input := transactionInput(session)
	first, err := s.CreateTransactionRequest(ctx, input, session.LastAccessAt)
	if err != nil {
		t.Fatal(err)
	}
	input.ID = strings.Repeat("b", 32)
	second, err := s.CreateTransactionRequest(ctx, input, first.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	session.LastAccessAt = second.CreatedAt
	_, err = s.db.Exec(`CREATE TRIGGER fail_decision AFTER UPDATE OF state ON transaction_requests
BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseSession(ctx, session.ID, first.ExpiresAt); err == nil {
		t.Fatal("close partially committed")
	}
	assertSessionUnchanged(t, s, session)
	for _, req := range []app.TransactionRequest{first, second} {
		got, err := s.GetTransactionRequest(ctx, req.ID, second.CreatedAt)
		if err != nil || got != req {
			t.Fatal("rollback lost pending request", got, err)
		}
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_decision"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseSession(ctx, session.ID, first.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		req   app.TransactionRequest
		state app.TransactionState
	}{
		{first, app.TransactionExpired}, {second, app.TransactionCanceled},
	} {
		got, err := s.GetTransactionRequest(ctx, tc.req.ID, second.ExpiresAt)
		if err != nil {
			t.Fatal(err)
		}
		assertTransactionState(t, got, tc.state, first.ExpiresAt)
	}
}

func TestTransactionLifecycleConcurrentCloseAndCreate(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	writer, closer := openTestStore(t, dir), openTestStore(t, dir)
	_, session := createLiveSession(t, writer, 0)
	input := transactionInput(session)
	at := session.LastAccessAt.Add(time.Second)
	start := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		<-start
		_, err := closer.CloseSession(ctx, session.ID, at)
		done <- err
	}()
	close(start)
	_, createErr := writer.CreateTransactionRequest(ctx, input, at)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := writer.GetTransactionRequest(ctx, input.ID, at)
	if errors.Is(createErr, app.ErrSessionClosed) {
		if !errors.Is(err, app.ErrTransactionRequestNotFound) {
			t.Fatal("closed Session acquired a request", got, err)
		}
		return
	}
	if createErr != nil || err != nil {
		t.Fatal(createErr, err)
	}
	assertTransactionState(t, got, app.TransactionCanceled, at)
}

func TestTransactionLifecyclePendingProtectsSession(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, session := createLiveSession(t, s, 0)
	input := transactionInput(session)
	req, err := s.CreateTransactionRequest(ctx, input, session.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// Isolate pending-request protection from the renewal performed by create.
	if _, err := s.db.Exec("UPDATE sessions SET last_access_at = created_at WHERE id = ?", session.ID); err != nil {
		t.Fatal(err)
	}
	at := req.ExpiresAt.Add(-time.Millisecond)
	counts, err := s.MaintainConnectionState(ctx, at, false)
	if err != nil || counts.Sessions != 0 || counts.TransactionRequests != 0 {
		t.Fatal("maintenance closed a protected Session", counts, err)
	}
	got, err := s.AccessSession(ctx, session.ID, at)
	if err != nil || got.ClosedAt != nil || !got.LastAccessAt.Equal(at) {
		t.Fatal("active access lost the pending request", got, err)
	}
}

func TestTransactionLifecycleV5Recovery(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	old := openConnectionSchema(t, dir, 5)
	type prior struct {
		session  app.Session
		state    app.TransactionState
		finished time.Time
	}
	var records []prior
	for index, state := range []app.TransactionState{app.TransactionCanceled, app.TransactionExpired, app.TransactionPending} {
		_, session := seedMigrationSession(t, old, index)
		at := session.LastAccessAt
		_, err := old.db.Exec(`INSERT INTO transaction_requests
(id,session_id,client_name,intent,from_address,to_address,chain_id,value,data,gas,gas_price,max_fee_per_gas,max_priority_fee_per_gas,created_at,expires_at)
VALUES (?,?, 'CLI','',?,?,?,'0x0','0x','','','','',?,?)`,
			session.ID, session.ID, session.Account, session.Account, session.ChainID, at.UnixMilli(), at.Add(5*time.Minute).UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		var finished time.Time
		if state != app.TransactionPending {
			finished = at.Add(time.Minute)
			if state == app.TransactionExpired {
				finished = at.Add(5 * time.Minute)
			}
			if _, err := old.db.Exec("UPDATE sessions SET closed_at=? WHERE id=?", finished.UnixMilli(), session.ID); err != nil {
				t.Fatal(err)
			}
		}
		records = append(records, prior{session, state, finished})
	}
	old.Close()
	s := openTestStore(t, dir)
	for _, r := range records {
		got, err := s.GetTransactionRequest(ctx, r.session.ID, r.session.LastAccessAt)
		if err != nil {
			t.Fatal(err)
		}
		assertTransactionState(t, got, r.state, r.finished)
	}
}
