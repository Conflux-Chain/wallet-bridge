package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) AccessSession(ctx context.Context, id string, now time.Time) (app.Session, error) {
	return s.updateSessionLifecycle(ctx, id, now, false)
}

func (s *Store) CloseSession(ctx context.Context, id string, now time.Time) (app.Session, error) {
	return s.updateSessionLifecycle(ctx, id, now, true)
}

func (s *Store) updateSessionLifecycle(ctx context.Context, id string, now time.Time, closeSession bool) (app.Session, error) {
	if !validTime(now) {
		return app.Session{}, app.ErrInvalidSession
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.Session{}, err
	}
	defer tx.Rollback()
	session, err := updateSessionInTx(ctx, tx, id, now, closeSession)
	if err != nil {
		return app.Session{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.Session{}, err
	}
	return session, nil
}

func updateSessionInTx(ctx context.Context, tx *sql.Tx, id string, now time.Time, closeSession bool) (app.Session, error) {
	now = time.UnixMilli(now.UnixMilli()).UTC()
	session, err := scanSession(tx.QueryRowContext(ctx, sessionSelect+" WHERE id = ?", id))
	if err != nil {
		return app.Session{}, err
	}
	if session.ClosedAt != nil {
		return session, nil
	}
	expires := session.LastAccessAt.Add(app.SessionIdleLifetime)
	if !now.Before(expires) {
		session.ClosedAt = &expires
	} else if closeSession {
		// A clock adjustment must not put closure before the last client access.
		closed := now
		if closed.Before(session.LastAccessAt) {
			closed = session.LastAccessAt
		}
		session.ClosedAt = &closed
	} else if now.After(session.LastAccessAt) {
		session.LastAccessAt = now
	} else {
		return session, nil
	}
	var closed any
	if session.ClosedAt != nil {
		closed = session.ClosedAt.UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET last_access_at = ?, closed_at = ? WHERE id = ?", session.LastAccessAt.UnixMilli(), closed, id); err != nil {
		return app.Session{}, err
	}
	return session, nil
}

func (s *Store) ExpireConnectionsAndSessions(ctx context.Context, now time.Time) (app.ConnectionExpiryCounts, error) {
	if !validTime(now) {
		return app.ConnectionExpiryCounts{}, app.ErrInvalidConnectionRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	defer tx.Rollback()
	counts, err := expireConnectionsInTx(ctx, tx, now)
	if err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	return counts, nil
}

func expireConnectionsInTx(ctx context.Context, tx *sql.Tx, now time.Time) (app.ConnectionExpiryCounts, error) {
	connections, err := tx.ExecContext(ctx, `UPDATE connection_requests SET state = 'expired', finished_at = expires_at
WHERE state = 'pending' AND expires_at <= ?`, now.UnixMilli())
	if err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	// Creating a transaction renews its Session through the request deadline.
	// Wallet handoff must also protect calls that outlive that deadline.
	sessions, err := tx.ExecContext(ctx, `UPDATE sessions SET closed_at = last_access_at + ?
WHERE closed_at IS NULL AND last_access_at <= ?`, app.SessionIdleLifetime.Milliseconds(), now.UnixMilli()-app.SessionIdleLifetime.Milliseconds())
	if err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	var counts app.ConnectionExpiryCounts
	if counts.ConnectionRequests, err = connections.RowsAffected(); err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	if counts.Sessions, err = sessions.RowsAffected(); err != nil {
		return app.ConnectionExpiryCounts{}, err
	}
	return counts, nil
}
