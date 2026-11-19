//go:build windows || darwin

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func openTestStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func fixture(index int) (app.BrowserGrant, [32]byte) {
	issued := time.Date(2026, 11, 19, 10, 0, index, 123000000, time.UTC)
	return app.BrowserGrant{ID: fmt.Sprintf("%032x", index+1), IssuedAt: issued, ExpiresAt: issued.Add(app.BrowserGrantLifetime)}, sha256.Sum256([]byte(fmt.Sprintf("fixture-cookie-must-not-be-stored-%d", index)))
}

func TestStoreConfigurationAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data space & # 钱包")
	s := openTestStore(t, dir)
	for pragma, want := range map[string]int{"user_version": 1, "application_id": applicationID, "synchronous": 2, "foreign_keys": 1, "busy_timeout": 1000, "temp_store": 2} {
		var got int
		if err := s.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s=%d, want %d: %v", pragma, got, want, err)
		}
	}
	var engine string
	if err := s.db.QueryRow("SELECT sqlite_version()").Scan(&engine); err != nil {
		t.Fatal(err)
	}
	t.Log("SQLite", engine)
	grant, digest := fixture(0)
	if err := s.CreateBrowserGrant(context.Background(), grant, digest); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTestStore(t, dir)
	got, err := s.FindActiveBrowserGrant(context.Background(), digest, grant.IssuedAt)
	if err != nil || got.ID != grant.ID || !got.ExpiresAt.Equal(grant.ExpiresAt) {
		t.Fatalf("reopen lost grant: %v", err)
	}
	var stored []byte
	if err := s.db.QueryRow("SELECT token_hash FROM browser_grants WHERE id=?", grant.ID).Scan(&stored); err != nil || !bytes.Equal(stored, digest[:]) {
		t.Fatal("token digest was not persisted correctly")
	}
	for _, name := range []string{DatabaseName, DatabaseName + "-journal"} {
		path := filepath.Join(dir, name)
		file, err := platform.OpenPrivateFile(path)
		if err != nil {
			t.Fatal("database file lost private protection:", err)
		}
		file.Close()
		data, err := os.ReadFile(path)
		if err != nil || bytes.Contains(data, []byte("fixture-cookie-must-not-be-stored-0")) {
			t.Fatal("raw cookie persisted")
		}
	}
}

func TestGrantExpiryAndRevocationPersist(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	grant, digest := fixture(0)
	other, otherDigest := fixture(1)
	for _, item := range []struct {
		grant  app.BrowserGrant
		digest [32]byte
	}{{grant, digest}, {other, otherDigest}} {
		if err := s.CreateBrowserGrant(ctx, item.grant, item.digest); err != nil {
			t.Fatal(err)
		}
	}
	for _, now := range []time.Time{grant.IssuedAt.Add(-time.Millisecond), grant.ExpiresAt, grant.ExpiresAt.Add(time.Millisecond)} {
		if _, err := s.FindActiveBrowserGrant(ctx, digest, now); !errors.Is(err, app.ErrBrowserGrantNotFound) {
			t.Fatalf("inactive grant accepted: %v", err)
		}
	}
	for _, now := range []time.Time{grant.IssuedAt, grant.ExpiresAt.Add(-time.Millisecond)} {
		got, err := s.FindActiveBrowserGrant(ctx, digest, now)
		if err != nil || !got.ExpiresAt.Equal(grant.ExpiresAt) {
			t.Fatal("lookup extended or lost expiration", err)
		}
	}
	revoked := grant.IssuedAt.Add(time.Hour)
	if err := s.RevokeBrowserGrant(ctx, grant.ID, revoked); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeBrowserGrant(ctx, grant.ID, revoked.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTestStore(t, dir)
	if _, err := s.FindActiveBrowserGrant(ctx, digest, revoked); !errors.Is(err, app.ErrBrowserGrantNotFound) {
		t.Fatal("revoked grant recovered as active", err)
	}
	if _, err := s.FindActiveBrowserGrant(ctx, otherDigest, revoked); err != nil {
		t.Fatal("revocation affected another grant", err)
	}
	if _, err := s.FindActiveBrowserGrant(ctx, sha256.Sum256([]byte("unknown")), revoked); !errors.Is(err, app.ErrBrowserGrantNotFound) {
		t.Fatal("unknown digest accepted", err)
	}
	grants, err := s.ListBrowserGrants(ctx)
	if err != nil || len(grants) != 2 || grants[0].ID != other.ID || grants[1].RevokedAt == nil || !grants[1].RevokedAt.Equal(revoked) {
		t.Fatal("revocation metadata lost or overwritten", err)
	}
}

func TestDuplicateGrantCannotReplaceExistingRecord(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	grant, digest := fixture(0)
	if err := s.CreateBrowserGrant(ctx, grant, digest); err != nil {
		t.Fatal(err)
	}
	other, otherDigest := fixture(1)
	for _, item := range []struct {
		grant  app.BrowserGrant
		digest [32]byte
	}{{grant, otherDigest}, {other, digest}} {
		if err := s.CreateBrowserGrant(ctx, item.grant, item.digest); !errors.Is(err, app.ErrBrowserGrantExists) {
			t.Fatalf("duplicate accepted: %v", err)
		}
	}
	got, err := s.FindActiveBrowserGrant(ctx, digest, grant.IssuedAt)
	if err != nil || got.ID != grant.ID || !got.ExpiresAt.Equal(grant.ExpiresAt) {
		t.Fatal("conflict replaced original record", err)
	}
}

func TestConcurrentGrantCreation(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	grant, digest := fixture(0)
	results := make(chan error, 12)
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() { defer group.Done(); results <- s.CreateBrowserGrant(context.Background(), grant, digest) }()
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, app.ErrBrowserGrantExists) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent writes committed %d records", successes)
	}
}

func TestMigrationRollbackIsAtomic(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	ctx := context.Background()
	// Exercise two pending migrations: the failed second step must also undo
	// the first step's data, table and version update.
	steps := append(append([]string{}, migrations...), "CREATE TABLE staged (value TEXT); INSERT INTO staged VALUES ('pending');", "INVALID SQL")
	if err := migrate(ctx, s.db, steps); err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var version, count int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("failed migration advanced schema", err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='staged'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration left a partial table", err)
	}
	grant, digest := fixture(0)
	if err := s.CreateBrowserGrant(ctx, grant, digest); err != nil {
		t.Fatal("failed migration left database unusable", err)
	}
}

func TestRejectUnrecognizedOrFutureSchemaWithoutChangingData(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		expected  error
	}{
		{"future", "PRAGMA user_version=2", ErrSchemaTooNew},
		{"other application", "PRAGMA application_id=123", ErrInvalidDatabase},
		{"unversioned nonempty", "PRAGMA application_id=0; PRAGMA user_version=0", ErrInvalidDatabase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			grant, digest := fixture(0)
			if err := s.CreateBrowserGrant(context.Background(), grant, digest); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			s.Close()
			path := filepath.Join(dir, DatabaseName)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := Open(context.Background(), dir)
			if opened != nil {
				opened.Close()
			}
			if !errors.Is(err, tc.expected) {
				t.Fatalf("unexpected schema result: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected database was changed", err)
			}
		})
	}
}

func TestCorruptDatabaseIsRetained(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := platform.EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, DatabaseName)
	f, err := platform.CreatePrivateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("damaged database - retain this evidence"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s, err := Open(context.Background(), dir)
	if s != nil {
		s.Close()
	}
	if err == nil {
		t.Fatal("corrupt database opened")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "damaged database - retain this evidence" {
		t.Fatal("corrupt database overwritten", err)
	}
}

func TestUnexpectedJournalFilesAreRetained(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			if err := platform.EnsurePrivateDir(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, DatabaseName+suffix)
			if err := os.WriteFile(path, []byte("existing state"), 0600); err != nil {
				t.Fatal(err)
			}
			if s, err := Open(context.Background(), dir); !errors.Is(err, ErrUnsupportedJournal) {
				if s != nil {
					s.Close()
				}
				t.Fatalf("unexpected journal accepted: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "existing state" {
				t.Fatal("unexpected journal changed", err)
			}
		})
	}
}

func TestInvalidGrantOrCanceledWriteDoesNotPersist(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	grant, digest := fixture(0)
	bad := grant
	bad.ExpiresAt = bad.ExpiresAt.Add(time.Hour)
	if err := s.CreateBrowserGrant(context.Background(), bad, digest); !errors.Is(err, app.ErrInvalidBrowserGrant) {
		t.Fatal("variable expiration accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.CreateBrowserGrant(ctx, grant, digest); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled write committed", err)
	}
	if _, err := s.FindActiveBrowserGrant(context.Background(), digest, grant.IssuedAt); !errors.Is(err, app.ErrBrowserGrantNotFound) {
		t.Fatal("invalid or canceled grant persisted", err)
	}
	if err := s.RevokeBrowserGrant(context.Background(), grant.ID, grant.IssuedAt); !errors.Is(err, app.ErrBrowserGrantNotFound) {
		t.Fatal("missing revocation accepted", err)
	}
}
