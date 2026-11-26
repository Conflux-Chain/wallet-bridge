// Package store implements the application's persistent storage interfaces.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	_ "modernc.org/sqlite"
)

const DatabaseName = "bridge.db"

var (
	ErrSchemaTooNew       = errors.New("database schema is newer than this program supports")
	ErrInvalidDatabase    = errors.New("database application or schema is not recognized")
	ErrUnsupportedJournal = errors.New("unexpected WAL or SHM file; automatic conversion is disabled")
)

type Store struct{ db *sql.DB }

var _ app.BrowserGrantStore = (*Store)(nil)

// Open initializes the store before the service reports readiness. The caller
// owns the service lock and closes this store before releasing that lock.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, DatabaseName)
	if err := rejectWALFiles(path); err != nil {
		return nil, err
	}
	for _, name := range []string{path, path + "-journal"} {
		if err := protectFile(name); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", dataSource(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	success := false
	defer func() {
		if !success {
			db.Close()
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return nil, err
	}
	if mode != "persist" {
		return nil, errors.New("persistent rollback journal is unavailable")
	}
	// Hot-journal recovery can delete the journal before connection PRAGMAs
	// select PERSIST. Restore protection before any application transaction.
	if err := protectFile(path + "-journal"); err != nil {
		return nil, err
	}
	if err := migrate(ctx, db, migrations); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	for _, query := range []string{
		"SELECT id, token_hash, issued_at, expires_at, revoked_at FROM browser_grants LIMIT 0",
		connectionSelect + " LIMIT 0",
		sessionSelect + " LIMIT 0",
		executionSelect + " WHERE transaction_id IS NULL LIMIT 0",
		transactionSelect + " LIMIT 0",
	} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("validate database schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	// SQLite must retain the protected journal across initialization and commits.
	f, err := platform.OpenPrivateFile(path + "-journal")
	if err != nil {
		return nil, fmt.Errorf("validate database journal: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	success = true
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func protectFile(path string) error {
	f, err := platform.CreatePrivateFile(path)
	if errors.Is(err, os.ErrExist) {
		f, err = platform.OpenPrivateFile(path)
	}
	if err != nil {
		return fmt.Errorf("protect database file: %w", err)
	}
	return f.Close()
}

func rejectWALFiles(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return ErrUnsupportedJournal
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func databaseURI(path string, query url.Values) string {
	name := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" {
		name = "/" + name
	}
	u := url.URL{Scheme: "file", Path: name, RawQuery: query.Encode()}
	return u.String()
}

func dataSource(path string) string {
	query := url.Values{"mode": {"rw"}, "_txlock": {"immediate"}}
	for _, pragma := range []string{"busy_timeout(1000)", "journal_mode(PERSIST)", "synchronous(FULL)", "foreign_keys(ON)", "temp_store(MEMORY)"} {
		query.Add("_pragma", pragma)
	}
	return databaseURI(path, query)
}
