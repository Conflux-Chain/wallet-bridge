package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// Snapshot copies a stopped store into a new private file. The caller holds
// the service lock and owns the staging directory until verification succeeds.
func Snapshot(ctx context.Context, dir, staging string) error {
	path := filepath.Join(dir, DatabaseName)
	if err := rejectWALFiles(path); err != nil {
		return err
	}
	if err := checkColdJournal(path + "-journal"); err != nil {
		return err
	}
	source, err := platform.OpenPrivateFile(path)
	if err != nil {
		return err
	}
	defer source.Close()
	dest := filepath.Join(staging, DatabaseName)
	f, err := platform.CreatePrivateFile(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, source)
	if err == nil {
		err = f.Sync()
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return err
	}
	return checkSnapshot(ctx, dest)
}

// PERSIST commits invalidate the rollback journal by zeroing its header.
// Let the enabled service recover a hot journal before another upgrade attempt.
func checkColdJournal(path string) error {
	f, err := platform.OpenPrivateFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	var header [28]byte
	_, err = io.ReadFull(f, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	if header != [28]byte{} {
		return errors.New("database rollback journal needs recovery by the enabled service")
	}
	return nil
}

func checkSnapshot(ctx context.Context, path string) error {
	// On a read-only database SQLite can skip CHECK constraints during
	// integrity_check. Check the private copy with writes disabled at SQL level.
	db, err := sql.Open("sqlite", databaseURI(path, url.Values{"mode": {"rw"}, "_pragma": {"query_only(ON)"}}))
	if err != nil {
		return err
	}
	defer db.Close()
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if mode == "wal" {
		return ErrUnsupportedJournal
	}
	var id, version int
	if err := db.QueryRowContext(ctx, "SELECT application_id, user_version FROM pragma_application_id, pragma_user_version").Scan(&id, &version); err != nil {
		return err
	}
	if id != applicationID || version < 1 {
		return ErrInvalidDatabase
	}
	if version > len(migrations) {
		return ErrSchemaTooNew
	}
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("database integrity check: %s", result)
	}
	var broken bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pragma_foreign_key_check)").Scan(&broken); err != nil {
		return err
	}
	if broken {
		return errors.New("database contains a broken foreign key")
	}
	return nil
}
