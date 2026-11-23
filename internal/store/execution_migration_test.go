//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func TestConnectionExecutionMigrationPreservesHistory(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	legacy := openConnectionSchema(t, dir, 3)
	r, ref, now := executionFixture(t, legacy, 0)
	// Seed a v3 completion without invoking the v4 execution coordination path.
	completed := createPendingConnection(t, legacy, 1)
	completion := completionFixture(completed)
	tx, err := legacy.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	completed, err = transitionConnectionInTx(ctx, tx, completed.ID, app.ConnectionSucceeded, completion, completed.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	session, err := legacy.GetSession(ctx, completion.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := legacy.CloseSession(ctx, session.ID, session.LastAccessAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	grant, digest := fixture(0)
	legacy.Close()
	s := openTestStore(t, dir)
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
		t.Fatal("execution migration did not reach the current schema", version, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM connection_executions").Scan(&count); err != nil || count != 0 {
		t.Fatal("migration synthesized an active execution", count, err)
	}
	assertConnectionUnchanged(t, s, r)
	assertConnectionUnchanged(t, s, completed)
	assertSessionUnchanged(t, s, closed)
	got, err := s.FindActiveBrowserGrant(ctx, digest, grant.IssuedAt)
	if err != nil || !reflect.DeepEqual(got, grant) {
		t.Fatal("migration changed browser authorization", got, err)
	}
	if err := migrate(ctx, s.db, migrations[:3]); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatal("v3 migration runner accepted v4", err)
	}
	first := claimExecution(t, s, ref, now)
	s.Close()
	s = openTestStore(t, dir)
	assertExecution(t, s, first)
}

func TestConnectionExecutionMigrationRollback(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	legacy := openConnectionSchema(t, dir, 3)
	r, _, _ := executionFixture(t, legacy, 0)
	steps := append([]string{}, migrations...)
	steps[3] += "INVALID SQL"
	if err := migrate(ctx, legacy.db, steps); err == nil {
		t.Fatal("injected migration failure succeeded")
	}
	var version, count int
	if err := legacy.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatal("failed execution migration advanced schema", version, err)
	}
	if err := legacy.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name LIKE 'connection_executions%'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration left table or index", count, err)
	}
	assertConnectionUnchanged(t, legacy, r)
	legacy.Close()
	s := openTestStore(t, dir)
	assertConnectionUnchanged(t, s, r)
}

func TestConnectionExecutionSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, ref, now := executionFixture(t, s, 0)
	claimExecution(t, s, ref, now)
	for i, phase := range []app.ConnectionExecutionPhase{app.ExecutionReady, app.ExecutionCalling, app.ExecutionInterruptedCalling,
		app.ExecutionInterruptedReady, app.ExecutionFinished} {
		_, next, _ := executionFixture(t, s, i+1)
		err := insertExecutionPhase(s, next, phase, now.UnixMilli(), fmt.Sprintf("%032x", 20000+i))
		occupied := phase == app.ExecutionReady || phase == app.ExecutionCalling || phase == app.ExecutionInterruptedCalling
		if (occupied && err == nil) || (!occupied && err != nil) {
			t.Fatal("partial unique index did not enforce slot occupancy", phase, err)
		}
	}
	if _, err := s.InterruptConnectionExecution(ctx, ref, now); err != nil {
		t.Fatal(err)
	}
	_, candidate, _ := executionFixture(t, s, 10)
	for _, tc := range []struct {
		name  string
		ref   app.ConnectionExecutionRef
		phase app.ConnectionExecutionPhase
		seen  int64
		call  any
	}{
		{"unknown phase", candidate, "unknown", now.UnixMilli(), nil},
		{"missing call", candidate, app.ExecutionCalling, now.UnixMilli(), nil},
		{"interrupted missing call", candidate, app.ExecutionInterruptedCalling, now.UnixMilli(), nil},
		{"negative timestamp", candidate, app.ExecutionReady, -1, nil},
		{"bad call ID", candidate, app.ExecutionReady, now.UnixMilli(), "invalid"},
		{"bad execution ID", app.ConnectionExecutionRef{ConnectionID: candidate.ConnectionID, ID: "bad", BrowserGrantID: candidate.BrowserGrantID}, app.ExecutionReady, now.UnixMilli(), nil},
		{"missing request", app.ConnectionExecutionRef{ConnectionID: fmt.Sprintf("%032x", 999), ID: candidate.ID, BrowserGrantID: candidate.BrowserGrantID}, app.ExecutionReady, now.UnixMilli(), nil},
		{"missing grant", app.ConnectionExecutionRef{ConnectionID: candidate.ConnectionID, ID: candidate.ID, BrowserGrantID: fmt.Sprintf("%032x", 999)}, app.ExecutionReady, now.UnixMilli(), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := insertExecutionPhase(s, tc.ref, tc.phase, tc.seen, tc.call); err == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
	for _, damage := range []string{"DROP TABLE connection_executions", "ALTER TABLE connection_executions RENAME COLUMN call_id TO missing_call"} {
		t.Run(damage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			db := openTestStore(t, dir)
			if _, err := db.db.Exec(damage); err != nil {
				t.Fatal(err)
			}
			db.Close()
			opened, err := Open(ctx, dir)
			if opened != nil {
				opened.Close()
			}
			if err == nil {
				t.Fatal("damaged execution schema accepted")
			}
		})
	}
}

func insertExecutionPhase(s *Store, ref app.ConnectionExecutionRef, phase app.ConnectionExecutionPhase, seen int64, call any) error {
	_, err := s.db.Exec(`INSERT INTO connection_executions
(connection_id, id, browser_grant_id, phase, last_seen_at, call_id) VALUES (?, ?, ?, ?, ?, ?)`, ref.ConnectionID, ref.ID, ref.BrowserGrantID, phase, seen, call)
	return err
}
