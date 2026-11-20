package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ app.ConnectionStore = (*Store)(nil)

const connectionSelect = `SELECT r.id, r.client_name, r.intent, r.chain_id, r.chain_name,
r.state, r.created_at, r.expires_at, r.finished_at, s.id
FROM connection_requests r LEFT JOIN sessions s ON s.connection_id = r.id`

const sessionSelect = `SELECT id, connection_id, wallet_kind, wallet_name, wallet_rdns,
account, chain_id, created_at, last_access_at, closed_at FROM sessions`

func (s *Store) CreateConnectionRequest(ctx context.Context, request app.ConnectionRequest, digest [32]byte) error {
	if !validID(request.ID) || strings.TrimSpace(request.ClientName) == "" || !validChainID(request.ChainID) ||
		!validTime(request.CreatedAt) || !validTime(request.ExpiresAt) ||
		request.CreatedAt.Nanosecond()%int(time.Millisecond) != 0 ||
		!request.ExpiresAt.Equal(request.CreatedAt.Add(app.ConnectionRequestLifetime)) ||
		request.State != app.ConnectionPending || request.FinishedAt != nil || request.SessionID != "" {
		return app.ErrInvalidConnectionRequest
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO connection_requests
(id, token_hash, client_name, intent, chain_id, chain_name, state, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, request.ID, digest[:], request.ClientName, request.Intent,
		request.ChainID, request.ChainName, request.State, request.CreatedAt.UnixMilli(), request.ExpiresAt.UnixMilli())
	var failure *sqlite.Error
	if errors.As(err, &failure) && (failure.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || failure.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
		return app.ErrConnectionRequestExists
	}
	return err
}

func (s *Store) GetConnectionRequest(ctx context.Context, id string) (app.ConnectionRequest, error) {
	return scanConnection(s.db.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", id))
}

func (s *Store) FindConnectionRequest(ctx context.Context, id string, digest [32]byte) (app.ConnectionRequest, error) {
	return scanConnection(s.db.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ? AND r.token_hash = ?", id, digest[:]))
}

func (s *Store) GetSession(ctx context.Context, id string) (app.Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, sessionSelect+" WHERE id = ?", id))
}

func (s *Store) FindSession(ctx context.Context, id string, digest [32]byte) (app.Session, error) {
	return scanSession(s.db.QueryRowContext(ctx, sessionSelect+" WHERE id = ? AND token_hash = ?", id, digest[:]))
}

func scanConnection(row interface{ Scan(...any) error }) (app.ConnectionRequest, error) {
	var request app.ConnectionRequest
	var created, expires int64
	var finished sql.NullInt64
	var sessionID sql.NullString
	err := row.Scan(&request.ID, &request.ClientName, &request.Intent, &request.ChainID, &request.ChainName,
		&request.State, &created, &expires, &finished, &sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ConnectionRequest{}, app.ErrConnectionRequestNotFound
	}
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	request.CreatedAt, request.ExpiresAt = time.UnixMilli(created).UTC(), time.UnixMilli(expires).UTC()
	if finished.Valid {
		value := time.UnixMilli(finished.Int64).UTC()
		request.FinishedAt = &value
	}
	request.SessionID = sessionID.String
	return request, nil
}

func scanSession(row interface{ Scan(...any) error }) (app.Session, error) {
	var session app.Session
	var created, accessed int64
	var closed sql.NullInt64
	err := row.Scan(&session.ID, &session.ConnectionID, &session.Wallet.Kind, &session.Wallet.Name,
		&session.Wallet.RDNS, &session.Account, &session.ChainID, &created, &accessed, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return app.Session{}, app.ErrSessionNotFound
	}
	if err != nil {
		return app.Session{}, err
	}
	session.CreatedAt, session.LastAccessAt = time.UnixMilli(created).UTC(), time.UnixMilli(accessed).UTC()
	if closed.Valid {
		value := time.UnixMilli(closed.Int64).UTC()
		session.ClosedAt = &value
	}
	return session, nil
}

func validChainID(value string) bool {
	if len(value) < 3 || !strings.HasPrefix(value, "0x") || (len(value) > 3 && value[2] == '0') {
		return false
	}
	for _, c := range value[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
