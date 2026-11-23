//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func transactionInput(session app.Session) app.TransactionRequestInput {
	return app.TransactionRequestInput{
		ID: strings.Repeat("a", 32), SessionID: session.ID, ClientName: "Test CLI", Intent: "核对并发送",
		Fields: map[string]string{"from": session.Account, "to": "0x" + strings.Repeat("aB", 20),
			"chainId": session.ChainID, "value": "9007199254740993", "data": "0x00AB",
			"gas": "21000", "maxFeePerGas": "15", "maxPriorityFeePerGas": "0"},
	}
}

func TestTransactionRequestMigrationAndReplay(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	old := openConnectionSchema(t, dir, 4)
	connection, session := createLiveSession(t, old, 0)
	broken := append([]string{}, migrations[:5]...)
	broken[4] += "INVALID SQL"
	if err := migrate(ctx, old.db, broken); err == nil {
		t.Fatal("broken migration committed")
	}
	var count, version int
	if err := old.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatal("failed migration advanced schema", version, err)
	}
	if err := old.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name LIKE 'transaction_requests%'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration left objects", count, err)
	}
	old.Close()
	s := openTestStore(t, dir)
	assertConnectionUnchanged(t, s, connection)
	assertSessionUnchanged(t, s, session)
	if err := migrate(ctx, s.db, migrations[:4]); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatal("older program accepted upgraded database", err)
	}
	input := transactionInput(session)
	now := session.LastAccessAt.Add(time.Minute)
	created, err := s.CreateTransactionRequest(ctx, input, now)
	if err != nil || !created.CreatedAt.Equal(now) || !created.ExpiresAt.Equal(now.Add(5*time.Minute)) ||
		created.Transaction.Value != "0x20000000000001" || created.Transaction.MaxPriorityFeePerGas != "0x0" {
		t.Fatal("first creation", created, err)
	}
	session.LastAccessAt = now
	assertSessionUnchanged(t, s, session)
	s.Close() // Lost creation response. Retry on the reopened database.
	s = openTestStore(t, dir)
	input.Fields["to"] = strings.ToLower(input.Fields["to"])
	input.Fields["data"], input.Fields["value"] = "0x00ab", "0x20000000000001"
	retry, err := s.CreateTransactionRequest(ctx, input, now.Add(time.Minute))
	if err != nil || retry != created {
		t.Fatal("replay changed original", retry, err)
	}
	assertSessionUnchanged(t, s, session)
	for _, change := range []func(*app.TransactionRequestInput){
		func(i *app.TransactionRequestInput) { i.SessionID = strings.Repeat("b", 32) },
		func(i *app.TransactionRequestInput) { i.ClientName += "2" },
		func(i *app.TransactionRequestInput) { i.Intent += "2" },
		func(i *app.TransactionRequestInput) { i.Fields["value"] = "1" },
		func(i *app.TransactionRequestInput) { i.Fields["nonce"] = "0" },
	} {
		other := input
		other.Fields = maps.Clone(input.Fields)
		change(&other)
		got, err := s.CreateTransactionRequest(ctx, other, now.Add(time.Minute))
		if !errors.Is(err, app.ErrTransactionRequestConflict) || got != created {
			t.Fatal("expected conflict with original record", got, err)
		}
	}
	assertSessionUnchanged(t, s, session)
	newInput := input
	newInput.ID = strings.Repeat("c", 32)
	if second, err := s.CreateTransactionRequest(ctx, newInput, now.Add(time.Minute)); err != nil || second.ID == created.ID {
		t.Fatal("same payload with new ID", second, err)
	}
	closed, err := s.CloseSession(ctx, session.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.CreateTransactionRequest(ctx, input, created.ExpiresAt.Add(time.Hour)); err != nil || got != created {
		t.Fatal("historical replay", got, err)
	}
	assertSessionUnchanged(t, s, closed)
	newInput.ID = strings.Repeat("d", 32)
	if _, err := s.CreateTransactionRequest(ctx, newInput, now); !errors.Is(err, app.ErrSessionClosed) {
		t.Fatal("new request for closed Session", err)
	}
}

func TestTransactionRequestRollbackAndExpiry(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, session := createLiveSession(t, s, 0)
	input := transactionInput(session)
	now := session.LastAccessAt.Add(time.Minute)
	input.Fields["chainId"] = "1"
	if _, err := s.CreateTransactionRequest(ctx, input, now); !errors.Is(err, app.ErrInvalidTransaction) {
		t.Fatal("accepted wrong network", err)
	}
	assertSessionUnchanged(t, s, session)
	input.Fields["chainId"] = session.ChainID
	_, err := s.db.Exec(`CREATE TABLE commit_guard (id TEXT REFERENCES sessions(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_create AFTER INSERT ON transaction_requests BEGIN INSERT INTO commit_guard VALUES ('missing'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.CreateTransactionRequest(ctx, input, now); err == nil || got.ID != "" {
		t.Fatal("failed commit returned success", got, err)
	}
	assertSessionUnchanged(t, s, session)
	if _, err := s.GetTransactionRequest(ctx, input.ID); !errors.Is(err, app.ErrTransactionRequestNotFound) {
		t.Fatal("failed commit left a record", err)
	}
	if _, err := s.db.Exec("DROP TRIGGER fail_create"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.CreateTransactionRequest(ctx, input, now); err != nil || got.ID != input.ID {
		t.Fatal("retry after failed commit", got, err)
	}
	session.LastAccessAt = now
	assertSessionUnchanged(t, s, session)
	input.ID = strings.Repeat("b", 32)
	expiry := session.LastAccessAt.Add(app.SessionIdleLifetime)
	if _, err := s.CreateTransactionRequest(ctx, input, expiry); !errors.Is(err, app.ErrSessionClosed) {
		t.Fatal("accepted Session at idle deadline", err)
	}
	session.ClosedAt = &expiry
	assertSessionUnchanged(t, s, session)
}

func TestTransactionRequestConcurrentCreation(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	first, second := openTestStore(t, dir), openTestStore(t, dir)
	_, session := createLiveSession(t, first, 0)
	input := transactionInput(session)
	type result struct {
		req app.TransactionRequest
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for index, handle := range []*Store{first, second} {
		go func() {
			<-start
			req, err := handle.CreateTransactionRequest(ctx, input, session.LastAccessAt.Add(time.Duration(index+1)*time.Minute))
			results <- result{req, err}
		}()
	}
	close(start)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.req != b.req {
		t.Fatal("concurrent creation returned different results", a, b)
	}
	session.LastAccessAt = a.req.CreatedAt
	assertSessionUnchanged(t, first, session)
	if got, err := second.GetTransactionRequest(ctx, input.ID); err != nil || got != a.req {
		t.Fatal("stored result differs from acknowledged creation", got, err)
	}
}
