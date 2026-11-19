package store

import (
	"context"
	"database/sql"
	"fmt"
)

const applicationID = 0x57425247 // WBRG

var migrations = []string{`
CREATE TABLE browser_grants (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    issued_at INTEGER NOT NULL CHECK (issued_at >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at - issued_at = 2592000000),
    revoked_at INTEGER CHECK (revoked_at >= issued_at)
);
CREATE INDEX browser_grants_by_issued_at ON browser_grants (issued_at DESC, id);
`}

func migrate(ctx context.Context, db *sql.DB, steps []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version, identity int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&identity); err != nil {
		return err
	}
	if version > len(steps) {
		return ErrSchemaTooNew
	}
	if version < 0 || (version != 0 && identity != applicationID) || (identity != 0 && identity != applicationID) {
		return ErrInvalidDatabase
	}
	if version == 0 {
		var objects int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return ErrInvalidDatabase
		}
	}
	for next := version; next < len(steps); next++ {
		if _, err := tx.ExecContext(ctx, steps[next]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", next+1)); err != nil {
			return err
		}
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
