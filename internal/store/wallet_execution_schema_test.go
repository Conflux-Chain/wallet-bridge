//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestWalletExecutionMigration(t *testing.T) {
	ctx := context.Background()
	for _, phase := range []app.ConnectionExecutionPhase{
		app.ExecutionReady, app.ExecutionCalling, app.ExecutionInterruptedReady, app.ExecutionInterruptedCalling, app.ExecutionFinished,
	} {
		t.Run(string(phase), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			old := openConnectionSchema(t, dir, 6)
			_, ref, now := executionFixture(t, old, 1)
			want := app.ConnectionExecution{ConnectionExecutionRef: ref, Phase: phase, LastSeenAt: now}
			var call any
			if phase == app.ExecutionCalling || phase == app.ExecutionInterruptedCalling {
				want.CallID = fmt.Sprintf("%032x", 9400)
				call = want.CallID
			}
			if _, err := old.db.Exec(`INSERT INTO connection_executions
(connection_id, id, browser_grant_id, phase, last_seen_at, call_id) VALUES (?, ?, ?, ?, ?, ?)`,
				ref.ConnectionID, ref.ID, ref.BrowserGrantID, phase, now.UnixMilli(), call); err != nil {
				t.Fatal(err)
			}
			broken := append([]string{}, migrations[:7]...)
			broken[6] += "INVALID SQL"
			if err := migrate(ctx, old.db, broken); err == nil {
				t.Fatal("broken migration committed")
			}
			var version int
			if err := old.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
				t.Fatal("failed migration advanced schema", version, err)
			}
			old.Close()
			s := openTestStore(t, dir)
			assertExecution(t, s, want)
			got, err := s.GetActiveWalletRequest(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch phase {
			case app.ExecutionInterruptedReady, app.ExecutionFinished:
				if got != nil {
					t.Fatal("inactive record occupies the wallet", got, err)
				}
			default:
				if got == nil || *got != (app.ActiveWalletRequest{Kind: app.WalletConnection, ID: ref.ConnectionID, Phase: phase}) {
					t.Fatal("migration lost the wallet owner", got, err)
				}
			}
		})
	}
}

func TestWalletExecutionSharedSlot(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s, other := openTestStore(t, dir), openTestStore(t, dir)
	_, session := createLiveSession(t, s, 0)
	_, ref, now := executionFixture(t, s, 1)
	input := transactionInput(session)
	input.ID = ref.ConnectionID
	if _, err := s.CreateTransactionRequest(ctx, input, now); err != nil {
		t.Fatal(err)
	}
	claimExecution(t, s, ref, now)
	const insert = `INSERT INTO wallet_executions
(connection_id, transaction_id, id, browser_grant_id, phase, last_seen_at) VALUES (?, ?, ?, ?, 'ready', ?)`
	txExecutionID := fmt.Sprintf("%032x", 9100)
	insertTransaction := func() error {
		_, err := other.db.Exec(insert, nil, input.ID, txExecutionID, ref.BrowserGrantID, now.UnixMilli())
		return err
	}
	assertWalletConstraint(t, insertTransaction(), sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	if _, err := s.InterruptConnectionExecution(ctx, ref, now); err != nil {
		t.Fatal(err)
	}
	if err := insertTransaction(); err != nil {
		t.Fatal("released slot did not admit transaction", err)
	}
	if _, err := s.InterruptAllConnectionExecutions(ctx, now); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.Exec("UPDATE wallet_executions SET phase='ready' WHERE connection_id=?", ref.ConnectionID)
	assertWalletConstraint(t, err, sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	if _, err := other.db.Exec("UPDATE wallet_executions SET phase='finished' WHERE transaction_id=?", input.ID); err != nil {
		t.Fatal(err)
	}
	next := ref
	next.ID = fmt.Sprintf("%032x", 9200)
	if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, now); err != nil {
		t.Fatal("connection did not recover after release", err)
	}
	if _, err := s.InterruptConnectionExecution(ctx, next, now); err != nil {
		t.Fatal(err)
	}

	// Both handles now have an inactive record with the same request UUID.
	start := make(chan struct{})
	attempt := make(chan error, 2)
	for db, column := range map[*Store]string{s: "connection_id", other: "transaction_id"} {
		go func() {
			<-start
			_, err := db.db.Exec("UPDATE wallet_executions SET phase='ready' WHERE "+column+"=?", input.ID)
			attempt <- err
		}()
	}
	close(start)
	first, second := <-attempt, <-attempt
	if first != nil && second != nil {
		t.Fatalf("neither request acquired the slot: %v, %v", first, second)
	}
	if first != nil {
		assertWalletConstraint(t, first, sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	} else {
		assertWalletConstraint(t, second, sqlite3.SQLITE_CONSTRAINT_UNIQUE)
	}
}

func TestWalletExecutionRequestReferences(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, session := createLiveSession(t, s, 0)
	r, ref, now := executionFixture(t, s, 1)
	input := transactionInput(session)
	if _, err := s.CreateTransactionRequest(context.Background(), input, now); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		connection  any
		transaction any
		code        int
	}{
		{"missing owner", nil, nil, sqlite3.SQLITE_CONSTRAINT_CHECK},
		{"two owners", r.ID, input.ID, sqlite3.SQLITE_CONSTRAINT_CHECK},
		{"wrong transaction", nil, r.ID, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY},
		{"wrong connection", input.ID, nil, sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.db.Exec(`INSERT INTO wallet_executions
(connection_id, transaction_id, id, browser_grant_id, phase, last_seen_at) VALUES (?, ?, ?, ?, 'finished', ?)`,
				tc.connection, tc.transaction, ref.ID, ref.BrowserGrantID, now.UnixMilli())
			assertWalletConstraint(t, err, tc.code)
		})
	}
}

func assertWalletConstraint(t *testing.T, err error, code int) {
	t.Helper()
	var failure *sqlite.Error
	if !errors.As(err, &failure) || failure.Code() != code {
		t.Fatalf("want SQLite constraint %d, got %v", code, err)
	}
}
