//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func connectionFixture(index int) app.ConnectionRequest {
	created := time.Date(2026, 11, 20, 8, 0, index, 123000000, time.UTC)
	return app.ConnectionRequest{
		ID: fmt.Sprintf("%032x", index+1), ClientName: "Example CLI", Intent: "连接用途",
		ChainID: "0x20000000000000000000000001", ChainName: "Example Network", State: app.ConnectionPending,
		CreatedAt: created, ExpiresAt: created.Add(app.ConnectionRequestLifetime),
	}
}

func sessionFixture(request app.ConnectionRequest) app.Session {
	created := request.CreatedAt.Add(time.Second)
	return app.Session{
		ID: "10000000000000000000000000000001", ConnectionID: request.ID,
		Wallet:  app.WalletDescriptor{Kind: app.WalletEIP6963, Name: "Example Wallet", RDNS: "org.example.wallet"},
		Account: "0x0000000000000000000000000000000000001234", ChainID: request.ChainID,
		CreatedAt: created, LastAccessAt: created.Add(time.Second),
	}
}

const sessionInsert = `INSERT INTO sessions
(id, connection_id, wallet_kind, wallet_name, wallet_rdns, account, chain_id, created_at, last_access_at, closed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func sessionArgs(session app.Session) []any {
	var closed any
	if session.ClosedAt != nil {
		closed = session.ClosedAt.UnixMilli()
	}
	return []any{session.ID, session.ConnectionID, session.Wallet.Kind, session.Wallet.Name,
		session.Wallet.RDNS, session.Account, session.ChainID, session.CreatedAt.UnixMilli(), session.LastAccessAt.UnixMilli(), closed}
}

// Fixture SQL keeps read/schema tests independent of the transition methods.
func insertSessionFixture(t *testing.T, s *Store, session app.Session) {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(sessionInsert, sessionArgs(session)...); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("UPDATE connection_requests SET state='succeeded', finished_at=? WHERE id=?", session.CreatedAt.UnixMilli(), session.ConnectionID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Historical schemas use their own fixture writes, independent of current execution maintenance.
func seedMigrationSession(t *testing.T, s *Store, index int) (app.ConnectionRequest, app.Session) {
	t.Helper()
	r := createPendingConnection(t, s, index)
	session := sessionFixture(r)
	session.ID = fmt.Sprintf("%032x", index+1000)
	insertSessionFixture(t, s, session)
	r.State, r.FinishedAt, r.SessionID = app.ConnectionSucceeded, &session.CreatedAt, session.ID
	return r, session
}

func TestConnectionMigrationPreservesV1Authorization(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	legacy := openConnectionSchema(t, dir, 1)
	db := legacy.db
	grant, digest := fixture(0)
	if err := legacy.CreateBrowserGrant(ctx, grant, digest); err != nil {
		t.Fatal(err)
	}
	// A failed v2 step must also roll back tables created earlier in that step.
	if err := migrate(ctx, db, []string{migrations[0], migrations[1] + "INVALID SQL"}); err == nil {
		t.Fatal("invalid v2 migration succeeded")
	}
	var version, tables int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("failed upgrade advanced v1 schema", version, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('connection_requests', 'sessions')").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("failed upgrade retained partial connection tables", tables, err)
	}
	legacy.Close()
	s := openTestStore(t, dir)
	got, err := s.FindActiveBrowserGrant(ctx, digest, grant.IssuedAt)
	if err != nil || !reflect.DeepEqual(got, grant) {
		t.Fatal("v1 migration lost or changed authorization", got, err)
	}
	request := connectionFixture(0)
	if err := s.CreateConnectionRequest(ctx, request); err != nil {
		t.Fatal("migrated database cannot create connection", err)
	}
}

func TestConnectionRequestPersistenceAndIDLookup(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	request, other := connectionFixture(0), connectionFixture(1)
	for _, item := range []app.ConnectionRequest{request, other} {
		if err := s.CreateConnectionRequest(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s = openTestStore(t, dir)
	for _, want := range []app.ConnectionRequest{request, other} {
		got, err := s.GetConnectionRequest(ctx, want.ID)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("read or restart changed request, precision or deadline", got, err)
		}
	}
	for _, id := range []string{"unknown", ""} {
		got, err := s.GetConnectionRequest(ctx, id)
		if !errors.Is(err, app.ErrConnectionRequestNotFound) || got.ID != "" {
			t.Fatal("missing ID returned a request", got, err)
		}
	}
	if _, err := s.GetConnectionRequest(ctx, "missing"); !errors.Is(err, app.ErrConnectionRequestNotFound) {
		t.Fatal("missing connection did not return not-found", err)
	}
}

func TestSessionReadsPreserveBindingAndCompletedConnection(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	request := connectionFixture(0)
	if err := s.CreateConnectionRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	session := sessionFixture(request)
	insertSessionFixture(t, s, session)
	closed := session.LastAccessAt.Add(time.Minute)
	if _, err := s.db.Exec("UPDATE sessions SET closed_at=? WHERE id=?", closed.UnixMilli(), session.ID); err != nil {
		t.Fatal(err)
	}
	session.ClosedAt = &closed
	s.Close()
	s = openTestStore(t, dir)
	for range 2 {
		got, err := s.GetSession(ctx, session.ID)
		if err != nil || !reflect.DeepEqual(got, session) {
			t.Fatal("session read lost binding, historical result or access time", got, err)
		}
	}
	if _, err := s.GetSession(ctx, "missing"); !errors.Is(err, app.ErrSessionNotFound) {
		t.Fatal("missing session did not return not-found", err)
	}
	got, err := s.GetConnectionRequest(ctx, request.ID)
	if err != nil || got.State != app.ConnectionSucceeded || got.SessionID != session.ID ||
		got.FinishedAt == nil || !got.FinishedAt.Equal(session.CreatedAt) || !got.ExpiresAt.Equal(request.ExpiresAt) {
		t.Fatal("closed session lost or changed original connection result", got, err)
	}
}

func TestConnectionCreateRejectsInvalidRecords(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	for name, mutate := range map[string]func(*app.ConnectionRequest){
		"ID":        func(r *app.ConnectionRequest) { r.ID = "invalid" },
		"client":    func(r *app.ConnectionRequest) { r.ClientName = " \t" },
		"decimal":   func(r *app.ConnectionRequest) { r.ChainID = "1030" },
		"leading 0": func(r *app.ConnectionRequest) { r.ChainID = "0x047" },
		"upper hex": func(r *app.ConnectionRequest) { r.ChainID = "0xAB" },
		"lifetime":  func(r *app.ConnectionRequest) { r.ExpiresAt = r.ExpiresAt.Add(time.Second) },
		"precision": func(r *app.ConnectionRequest) {
			r.CreatedAt = r.CreatedAt.Add(time.Nanosecond)
			r.ExpiresAt = r.CreatedAt.Add(app.ConnectionRequestLifetime)
		},
		"state":      func(r *app.ConnectionRequest) { r.State = app.ConnectionSucceeded },
		"completion": func(r *app.ConnectionRequest) { r.FinishedAt = &r.CreatedAt },
		"session":    func(r *app.ConnectionRequest) { r.SessionID = "unexpected" },
	} {
		t.Run(name, func(t *testing.T) {
			request := connectionFixture(0)
			mutate(&request)
			if err := s.CreateConnectionRequest(context.Background(), request); !errors.Is(err, app.ErrInvalidConnectionRequest) {
				t.Fatal("invalid request accepted", err)
			}
		})
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM connection_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid creation persisted data", count, err)
	}
}

func TestConnectionCreateConflictsDoNotReplaceRecords(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request := connectionFixture(0)
	results := make(chan error, 6)
	var group sync.WaitGroup
	for range 6 {
		group.Go(func() { results <- s.CreateConnectionRequest(ctx, request) })
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, app.ErrConnectionRequestExists) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent creation did not commit exactly one record", successes)
	}
	other := connectionFixture(1)
	if err := s.CreateConnectionRequest(ctx, other); err != nil {
		t.Fatal("different ID could not create a request", err)
	}
	request.ClientName = "attempted replacement"
	if err := s.CreateConnectionRequest(ctx, request); !errors.Is(err, app.ErrConnectionRequestExists) {
		t.Fatal("ID collision accepted", err)
	}
	got, err := s.GetConnectionRequest(ctx, request.ID)
	if err != nil || got.ClientName != "Example CLI" {
		t.Fatal("conflict replaced original request", got, err)
	}
}

func TestConnectionSessionSchemaConstraints(t *testing.T) {
	for name, mutate := range map[string]func(*app.Session){
		"orphan":      func(s *app.Session) { s.ConnectionID = "000000000000000000000000000000ff" },
		"wallet kind": func(s *app.Session) { s.Wallet.Kind = "unknown" },
		"rdns":        func(s *app.Session) { s.Wallet.RDNS = "" },
		"account":     func(s *app.Session) { s.Account = "0xABC0000000000000000000000000000000000000" },
		"chain":       func(s *app.Session) { s.ChainID = "0x01" },
		"access time": func(s *app.Session) { s.LastAccessAt = s.CreatedAt.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			request := connectionFixture(0)
			if err := s.CreateConnectionRequest(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			session := sessionFixture(request)
			mutate(&session)
			if _, err := s.db.Exec(sessionInsert, sessionArgs(session)...); err == nil {
				t.Fatal("invalid session bypassed schema constraints")
			}
		})
	}
	t.Run("one session per connection", func(t *testing.T) {
		s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
		request := connectionFixture(0)
		if err := s.CreateConnectionRequest(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		session := sessionFixture(request)
		insertSessionFixture(t, s, session)
		session.ID = "10000000000000000000000000000002"
		if _, err := s.db.Exec(sessionInsert, sessionArgs(session)...); err == nil {
			t.Fatal("connection acquired two sessions")
		}
	})
}

func TestConnectionSchemaValidationRejectsDamagedTables(t *testing.T) {
	for _, statement := range []string{"DROP TABLE sessions", "ALTER TABLE connection_requests RENAME COLUMN chain_id TO damaged_chain"} {
		t.Run(statement, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			if _, err := s.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			s.Close()
			opened, err := Open(context.Background(), dir)
			if opened != nil {
				opened.Close()
			}
			if err == nil {
				t.Fatal("damaged connection schema reported ready")
			}
		})
	}
}

func TestConnectionCanceledContextDoesNotCreateRecord(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := connectionFixture(0)
	if err := s.CreateConnectionRequest(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled context was ignored", err)
	}
	if _, err := s.GetConnectionRequest(context.Background(), request.ID); !errors.Is(err, app.ErrConnectionRequestNotFound) {
		t.Fatal("canceled creation persisted a request", err)
	}
}
