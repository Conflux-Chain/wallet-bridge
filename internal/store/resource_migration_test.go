//go:build windows || darwin

package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func openConnectionSchema(t *testing.T, dir string, version int) *Store {
	t.Helper()
	if err := platform.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		if err := protectFile(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", dataSource(filepath.Join(dir, DatabaseName)))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := migrate(context.Background(), db, migrations[:version]); err != nil {
		t.Fatal(err)
	}
	return &Store{db: db}
}

// Prepare v2 rows independently of the current credential-free write methods.
func insertLegacyConnection(t *testing.T, s *Store, request app.ConnectionRequest) {
	t.Helper()
	digest := sha256.Sum256([]byte(request.ID))
	var finished any
	if request.FinishedAt != nil {
		finished = request.FinishedAt.UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO connection_requests
(id, token_hash, client_name, intent, chain_id, chain_name, state, created_at, expires_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, request.ID, digest[:], request.ClientName, request.Intent,
		request.ChainID, request.ChainName, request.State, request.CreatedAt.UnixMilli(), request.ExpiresAt.UnixMilli(), finished)
	if err != nil {
		t.Fatal(err)
	}
}

func insertLegacySession(t *testing.T, s *Store, session app.Session) {
	t.Helper()
	digest := sha256.Sum256([]byte(session.ID))
	var closed any
	if session.ClosedAt != nil {
		closed = session.ClosedAt.UnixMilli()
	}
	_, err := s.db.Exec(`INSERT INTO sessions
(id, connection_id, token_hash, wallet_kind, wallet_name, wallet_rdns, account, chain_id, created_at, last_access_at, closed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, session.ID, session.ConnectionID, digest[:], session.Wallet.Kind,
		session.Wallet.Name, session.Wallet.RDNS, session.Account, session.ChainID,
		session.CreatedAt.UnixMilli(), session.LastAccessAt.UnixMilli(), closed)
	if err != nil {
		t.Fatal(err)
	}
}

func assertResourceSchema(t *testing.T, s *Store) {
	t.Helper()
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
		t.Fatal("resource schema was not upgraded", version, err)
	}
	for _, table := range []string{"connection_requests", "sessions"} {
		if err := s.db.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name='token_hash'", table).Scan(&count); err != nil || count != 0 {
			t.Fatal("resource token column remains", table, count, err)
		}
	}
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='index' AND name IN ('connection_requests_by_expiry', 'sessions_by_idle')").Scan(&count); err != nil || count != 2 {
		t.Fatal("resource maintenance indexes were lost", count, err)
	}
	var parent string
	if err := s.db.QueryRow(`SELECT "table" FROM pragma_foreign_key_list('sessions')`).Scan(&parent); err != nil || parent != "connection_requests" {
		t.Fatal("Session foreign key refers to the wrong table", parent, err)
	}
	if err := s.db.QueryRow("PRAGMA foreign_key_check").Scan(new(string), new(int), new(string), new(int)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("migration left a foreign key violation", err)
	}
}

func TestConnectionResourceSchemaHasNoTokens(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	assertResourceSchema(t, s)
}

func TestConnectionResourceMigrationPreservesV2History(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	legacy := openConnectionSchema(t, dir, 2)
	grant, digest := fixture(0)
	if err := legacy.CreateBrowserGrant(ctx, grant, digest); err != nil {
		t.Fatal(err)
	}
	var requests []app.ConnectionRequest
	var sessions []app.Session
	for i, state := range []app.ConnectionState{app.ConnectionPending, app.ConnectionSucceeded, app.ConnectionSucceeded,
		app.ConnectionSucceeded, app.ConnectionRejected, app.ConnectionCanceled, app.ConnectionExpired} {
		request := connectionFixture(i)
		request.State = state
		finished := request.CreatedAt.Add(time.Second)
		if state == app.ConnectionExpired {
			finished = request.ExpiresAt
		}
		if state != app.ConnectionPending {
			request.FinishedAt = &finished
		}
		insertLegacyConnection(t, legacy, request)
		if state == app.ConnectionSucceeded {
			session := sessionFixture(request)
			session.ID = fmt.Sprintf("%032x", 1000+i)
			if i == 2 {
				session.Wallet = app.WalletDescriptor{Kind: app.WalletLegacy, Name: "Legacy Wallet"}
				closed := session.LastAccessAt.Add(time.Minute)
				session.ClosedAt = &closed
			}
			insertLegacySession(t, legacy, session)
			request.SessionID = session.ID
			sessions = append(sessions, session)
		}
		requests = append(requests, request)
	}
	legacy.Close()
	s := openTestStore(t, dir)
	assertResourceSchema(t, s)
	// Older programs must refuse the new schema instead of attempting a downgrade.
	if err := migrate(ctx, s.db, migrations[:2]); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatal("v2 program accepted or downgraded the new schema", err)
	}
	for range 2 {
		for _, request := range requests {
			assertConnectionUnchanged(t, s, request)
		}
		for _, session := range sessions {
			assertSessionUnchanged(t, s, session)
		}
		got, err := s.FindActiveBrowserGrant(ctx, digest, grant.IssuedAt)
		if err != nil || !reflect.DeepEqual(got, grant) {
			t.Fatal("resource migration changed browser authorization", got, err)
		}
		s.Close()
		s = openTestStore(t, dir)
		assertResourceSchema(t, s)
	}
	completed := requests[1]
	completion := app.ConnectionCompletion{SessionID: sessions[0].ID, Wallet: sessions[0].Wallet,
		Account: sessions[0].Account, ChainID: sessions[0].ChainID}
	got, err := s.CompleteConnectionRequest(ctx, completed.ID, completion, completed.ExpiresAt.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, completed) {
		t.Fatal("migrated success could not be replayed without a token", got, err)
	}
	closed, err := s.AccessSession(ctx, sessions[1].ID, sessions[1].CreatedAt)
	if err != nil || !reflect.DeepEqual(closed, sessions[1]) {
		t.Fatal("migration or access revived closed history", closed, err)
	}
	deadline := sessions[2].LastAccessAt.Add(app.SessionIdleLifetime)
	idle, err := s.AccessSession(ctx, sessions[2].ID, deadline.Add(time.Hour))
	if err != nil || idle.ClosedAt == nil || !idle.ClosedAt.Equal(deadline) {
		t.Fatal("migration reset the idle deadline", idle, err)
	}
}

func TestConnectionResourceMigrationRollback(t *testing.T) {
	ctx := context.Background()
	for _, stage := range []string{"copy", "after rebuild"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openConnectionSchema(t, dir, 2)
			request := connectionFixture(0)
			session := sessionFixture(request)
			request.State, request.FinishedAt, request.SessionID = app.ConnectionSucceeded, &session.CreatedAt, session.ID
			insertLegacyConnection(t, s, request)
			insertLegacySession(t, s, session)
			steps := append([]string{}, migrations...)
			if stage == "copy" {
				if _, err := s.db.Exec("PRAGMA ignore_check_constraints=ON; UPDATE sessions SET account='invalid'; PRAGMA ignore_check_constraints=OFF"); err != nil {
					t.Fatal(err)
				}
				session.Account = "invalid"
			} else {
				steps[2] += "INVALID SQL"
			}
			if err := migrate(ctx, s.db, steps); err == nil {
				t.Fatal("injected migration failure succeeded")
			}
			var version, tables, hashes int
			if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
				t.Fatal("failed migration advanced schema", version, err)
			}
			if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('connection_requests_v3', 'sessions_v3')").Scan(&tables); err != nil || tables != 0 {
				t.Fatal("failed migration left rebuilt tables", tables, err)
			}
			for _, table := range []string{"connection_requests", "sessions"} {
				if err := s.db.QueryRow("SELECT count(*) FROM " + table + " WHERE token_hash IS NOT NULL").Scan(&hashes); err != nil || hashes != 1 {
					t.Fatal("failed migration lost original token columns or records", table, hashes, err)
				}
			}
			assertConnectionUnchanged(t, s, request)
			assertSessionUnchanged(t, s, session)
			// Repair only the deliberately invalid fixture, then retry the real step.
			if stage == "copy" {
				valid := sessionFixture(request)
				if _, err := s.db.Exec("UPDATE sessions SET account=?", valid.Account); err != nil {
					t.Fatal(err)
				}
				session.Account = valid.Account
			}
			s.Close()
			s = openTestStore(t, dir)
			assertResourceSchema(t, s)
			assertConnectionUnchanged(t, s, request)
			assertSessionUnchanged(t, s, session)
		})
	}
}
