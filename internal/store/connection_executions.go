package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"modernc.org/sqlite"

	sqlite3 "modernc.org/sqlite/lib"
)

var _ app.ConnectionExecutionStore = (*Store)(nil)

const executionSelect = `SELECT connection_id, id, browser_grant_id, phase, last_seen_at, call_id FROM connection_executions`
const executionOccupied = `phase IN ('ready', 'calling', 'interrupted_calling')`

func (s *Store) GetConnectionExecution(ctx context.Context, id string) (app.ConnectionExecution, error) {
	return scanExecution(s.db.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", id))
}

func (s *Store) GetOccupiedExecution(ctx context.Context) (app.ConnectionExecution, error) {
	return scanExecution(s.db.QueryRowContext(ctx, executionSelect+" WHERE "+executionOccupied))
}

func (s *Store) ClaimConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, now time.Time) (app.ConnectionExecution, error) {
	tx, err := s.beginExecution(ctx, ref, now)
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	defer tx.Rollback()
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err == nil {
		if current.ConnectionExecutionRef == ref && (current.Phase == app.ExecutionReady || current.Phase == app.ExecutionCalling) {
			return commitExecution(tx, current, nil)
		}
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	if !errors.Is(err, app.ErrConnectionExecutionNotFound) {
		return app.ConnectionExecution{}, err
	}
	if !pendingExecutionRequest(request, now) {
		return commitExecution(tx, app.ConnectionExecution{}, app.ErrConnectionConflict)
	}
	if busy, err := occupiedExecution(ctx, tx, ref.ConnectionID); err == nil {
		return commitExecution(tx, busy, app.ErrConnectionExecutionBusy)
	} else if !errors.Is(err, app.ErrConnectionExecutionNotFound) {
		return app.ConnectionExecution{}, err
	}
	current = app.ConnectionExecution{ConnectionExecutionRef: ref, Phase: app.ExecutionReady, LastSeenAt: time.UnixMilli(now.UnixMilli()).UTC()}
	_, err = tx.ExecContext(ctx, `INSERT INTO connection_executions
(connection_id, id, browser_grant_id, phase, last_seen_at) VALUES (?, ?, ?, 'ready', ?)`, ref.ConnectionID, ref.ID, ref.BrowserGrantID, now.UnixMilli())
	if err != nil {
		return app.ConnectionExecution{}, executionWriteError(err)
	}
	return commitExecution(tx, current, nil)
}

func (s *Store) TouchConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, now time.Time) (app.ConnectionExecution, error) {
	tx, err := s.beginExecution(ctx, ref, now)
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if current.ConnectionExecutionRef != ref || (current.Phase != app.ExecutionReady && current.Phase != app.ExecutionCalling) {
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	if now.UnixMilli() > current.LastSeenAt.UnixMilli() {
		if _, err := tx.ExecContext(ctx, "UPDATE connection_executions SET last_seen_at = ? WHERE connection_id = ?", now.UnixMilli(), ref.ConnectionID); err != nil {
			return app.ConnectionExecution{}, err
		}
		current.LastSeenAt = time.UnixMilli(now.UnixMilli()).UTC()
	}
	return commitExecution(tx, current, nil)
}

func (s *Store) InterruptConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, now time.Time) (app.ConnectionExecution, error) {
	tx, err := s.beginExecution(ctx, ref, now)
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if current.ConnectionExecutionRef != ref {
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	phase := current.Phase
	if phase == app.ExecutionReady {
		phase = app.ExecutionInterruptedReady
	} else if phase == app.ExecutionCalling {
		phase = app.ExecutionInterruptedCalling
	}
	if phase != current.Phase {
		if _, err := tx.ExecContext(ctx, "UPDATE connection_executions SET phase = ? WHERE connection_id = ?", phase, ref.ConnectionID); err != nil {
			return app.ConnectionExecution{}, err
		}
		current.Phase = phase
	}
	return commitExecution(tx, current, nil)
}

func (s *Store) RecoverConnectionExecution(ctx context.Context, previousID string, ref app.ConnectionExecutionRef, now time.Time) (app.ConnectionExecution, error) {
	if !validID(previousID) || previousID == ref.ID {
		return app.ConnectionExecution{}, app.ErrInvalidConnectionExecution
	}
	tx, err := s.beginExecution(ctx, ref, now)
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if current.ConnectionExecutionRef == ref {
		// A lost recovery response is read back without renewing liveness.
		return commitExecution(tx, current, nil)
	}
	if current.ID != previousID {
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if now.Before(request.CreatedAt) {
		return app.ConnectionExecution{}, app.ErrInvalidConnectionExecution
	}
	if !pendingExecutionRequest(request, now) {
		// Explicit user recovery can clear an outstanding wallet wait, but must
		// never turn a terminal connection into a new execution or Session.
		if _, err := tx.ExecContext(ctx, "UPDATE connection_executions SET phase = 'finished' WHERE connection_id = ?", ref.ConnectionID); err != nil {
			return app.ConnectionExecution{}, err
		}
		current.Phase = app.ExecutionFinished
		return commitExecution(tx, current, nil)
	}
	if current.Phase != app.ExecutionInterruptedReady && current.Phase != app.ExecutionInterruptedCalling {
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	if busy, err := occupiedExecution(ctx, tx, ref.ConnectionID); err == nil {
		return commitExecution(tx, busy, app.ErrConnectionExecutionBusy)
	} else if !errors.Is(err, app.ErrConnectionExecutionNotFound) {
		return app.ConnectionExecution{}, err
	}
	seen := now.UnixMilli()
	if seen < current.LastSeenAt.UnixMilli() {
		seen = current.LastSeenAt.UnixMilli()
	}
	_, err = tx.ExecContext(ctx, `UPDATE connection_executions
SET id = ?, browser_grant_id = ?, phase = 'ready', last_seen_at = ?, call_id = NULL WHERE connection_id = ?`, ref.ID, ref.BrowserGrantID, seen, ref.ConnectionID)
	if err != nil {
		return app.ConnectionExecution{}, executionWriteError(err)
	}
	current = app.ConnectionExecution{ConnectionExecutionRef: ref, Phase: app.ExecutionReady, LastSeenAt: time.UnixMilli(seen).UTC()}
	return commitExecution(tx, current, nil)
}

func (s *Store) beginExecution(ctx context.Context, ref app.ConnectionExecutionRef, now time.Time) (*sql.Tx, error) {
	if !validID(ref.ConnectionID) || !validID(ref.ID) || !validID(ref.BrowserGrantID) || !validTime(now) {
		return nil, app.ErrInvalidConnectionExecution
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM browser_grants
WHERE id = ? AND revoked_at IS NULL AND issued_at <= ? AND expires_at > ?)`, ref.BrowserGrantID, now.UnixMilli(), now.UnixMilli()).Scan(&active)
	if err == nil && !active {
		err = app.ErrInvalidBrowserCredential
	}
	if err == nil {
		_, err = maintainExecutions(ctx, tx, now, false)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func occupiedExecution(ctx context.Context, tx *sql.Tx, except string) (app.ConnectionExecution, error) {
	return scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE "+executionOccupied+" AND connection_id != ?", except))
}

func pendingExecutionRequest(r app.ConnectionRequest, now time.Time) bool {
	return r.State == app.ConnectionPending && !now.Before(r.CreatedAt) && now.Before(r.ExpiresAt)
}

func commitExecution(tx *sql.Tx, result app.ConnectionExecution, decision error) (app.ConnectionExecution, error) {
	if err := tx.Commit(); err != nil {
		return app.ConnectionExecution{}, err
	}
	return result, decision
}

func executionWriteError(err error) error {
	var failure *sqlite.Error
	if errors.As(err, &failure) && (failure.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || failure.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return app.ErrConnectionExecutionConflict
	}
	return err
}

func scanExecution(row interface{ Scan(...any) error }) (app.ConnectionExecution, error) {
	var result app.ConnectionExecution
	var seen int64
	var call sql.NullString
	err := row.Scan(&result.ConnectionID, &result.ID, &result.BrowserGrantID, &result.Phase, &seen, &call)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ConnectionExecution{}, app.ErrConnectionExecutionNotFound
	}
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	result.LastSeenAt, result.CallID = time.UnixMilli(seen).UTC(), call.String
	return result, nil
}
