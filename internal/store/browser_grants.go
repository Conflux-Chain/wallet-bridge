package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func (s *Store) CreateBrowserGrant(ctx context.Context, grant app.BrowserGrant, digest [32]byte) error {
	if !validID(grant.ID) || !validTime(grant.IssuedAt) || !validTime(grant.ExpiresAt) || grant.RevokedAt != nil ||
		grant.ExpiresAt.UnixMilli()-grant.IssuedAt.UnixMilli() != app.BrowserGrantLifetime.Milliseconds() {
		return app.ErrInvalidBrowserGrant
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO browser_grants (id, token_hash, issued_at, expires_at) VALUES (?, ?, ?, ?)", grant.ID, digest[:], grant.IssuedAt.UnixMilli(), grant.ExpiresAt.UnixMilli())
	var failure *sqlite.Error
	if errors.As(err, &failure) && (failure.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || failure.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
		return app.ErrBrowserGrantExists
	}
	return err
}

func (s *Store) FindActiveBrowserGrant(ctx context.Context, digest [32]byte, now time.Time) (app.BrowserGrant, error) {
	if !validTime(now) {
		return app.BrowserGrant{}, app.ErrInvalidBrowserGrant
	}
	return scanGrant(s.db.QueryRowContext(ctx, `SELECT id, issued_at, expires_at, revoked_at FROM browser_grants
WHERE token_hash = ? AND revoked_at IS NULL AND issued_at <= ? AND expires_at > ?`, digest[:], now.UnixMilli(), now.UnixMilli()))
}

func (s *Store) ListBrowserGrants(ctx context.Context) ([]app.BrowserGrant, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, issued_at, expires_at, revoked_at FROM browser_grants ORDER BY issued_at DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []app.BrowserGrant{}
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (s *Store) RevokeBrowserGrant(ctx context.Context, id string, now time.Time) error {
	if !validID(id) || !validTime(now) {
		return app.ErrInvalidBrowserGrant
	}
	result, err := s.db.ExecContext(ctx, "UPDATE browser_grants SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ? AND issued_at <= ?", now.UnixMilli(), id, now.UnixMilli())
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return app.ErrBrowserGrantNotFound
	}
	return nil
}

func (s *Store) RevokeAllBrowserGrants(ctx context.Context, now time.Time) (int64, error) {
	if !validTime(now) {
		return 0, app.ErrInvalidBrowserGrant
	}
	// Revoke every outstanding grant, including expired records. If the system
	// clock moved back, keep the persisted revocation at or after issuance.
	result, err := s.db.ExecContext(ctx, "UPDATE browser_grants SET revoked_at = MAX(issued_at, ?) WHERE revoked_at IS NULL", now.UnixMilli())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func scanGrant(row interface{ Scan(...any) error }) (app.BrowserGrant, error) {
	var grant app.BrowserGrant
	var issued, expires int64
	var revoked sql.NullInt64
	err := row.Scan(&grant.ID, &issued, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return grant, app.ErrBrowserGrantNotFound
	}
	if err != nil {
		return grant, err
	}
	grant.IssuedAt, grant.ExpiresAt = time.UnixMilli(issued).UTC(), time.UnixMilli(expires).UTC()
	if revoked.Valid {
		value := time.UnixMilli(revoked.Int64).UTC()
		grant.RevokedAt = &value
	}
	return grant, nil
}

func validID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id
}

func validTime(value time.Time) bool { return value.Year() >= 1970 && value.Year() <= 9999 }
