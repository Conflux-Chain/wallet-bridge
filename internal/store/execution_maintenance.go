package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) MaintainConnectionExecutions(ctx context.Context, now time.Time) (int64, error) {
	return s.maintainConnectionExecutions(ctx, now, false)
}

// InterruptAllConnectionExecutions is a service-startup operation. Opening a
// second store connection must not itself interrupt the running service.
func (s *Store) InterruptAllConnectionExecutions(ctx context.Context, now time.Time) (int64, error) {
	return s.maintainConnectionExecutions(ctx, now, true)
}

func (s *Store) maintainConnectionExecutions(ctx context.Context, now time.Time, restart bool) (int64, error) {
	if !validTime(now) {
		return 0, app.ErrInvalidConnectionExecution
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	count, err := maintainExecutions(ctx, tx, now, restart)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func maintainExecutions(ctx context.Context, tx *sql.Tx, now time.Time, restart bool) (int64, error) {
	// A terminal request cannot prove a wallet handoff ended. In particular,
	// interrupted_calling is never released by liveness or deadline maintenance.
	result, err := tx.ExecContext(ctx, `UPDATE wallet_executions SET phase = CASE
WHEN phase = 'calling' THEN 'interrupted_calling'
WHEN EXISTS (SELECT 1 FROM connection_requests r WHERE r.id = connection_id AND (r.state != 'pending' OR r.expires_at <= ?)) THEN 'finished'
ELSE 'interrupted_ready' END
WHERE connection_id IS NOT NULL AND phase IN ('ready', 'calling') AND (
? OR last_seen_at <= ? OR
EXISTS (SELECT 1 FROM connection_requests r WHERE r.id = connection_id AND (r.state != 'pending' OR r.expires_at <= ?)) OR
NOT EXISTS (SELECT 1 FROM browser_grants g WHERE g.id = browser_grant_id AND g.revoked_at IS NULL AND g.issued_at <= ? AND g.expires_at > ?))`,
		now.UnixMilli(), restart, now.UnixMilli()-app.ConnectionExecutionHeartbeatTimeout.Milliseconds(), now.UnixMilli(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE wallet_executions SET phase = 'finished'
WHERE connection_id IS NOT NULL AND phase = 'interrupted_ready' AND EXISTS (
SELECT 1 FROM connection_requests r WHERE r.id = connection_id AND (r.state != 'pending' OR r.expires_at <= ?))`, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	more, err := result.RowsAffected()
	return count + more, err
}

func maintainWalletExecutions(ctx context.Context, tx *sql.Tx, now time.Time, restart bool) (int64, error) {
	connections, err := maintainExecutions(ctx, tx, now, restart)
	if err != nil {
		return 0, err
	}
	// Persist a reviewed transaction's deadline even when a competing connection
	// observes it first. The terminal-state trigger preserves outstanding calls.
	if _, err := tx.ExecContext(ctx, expireDueTransactions+` AND id IN
(SELECT transaction_id FROM wallet_executions WHERE transaction_id IS NOT NULL AND `+executionOccupied+")", now.UnixMilli()); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE wallet_executions SET phase = CASE
WHEN phase = 'calling' THEN 'interrupted_calling' ELSE 'interrupted_ready' END
WHERE transaction_id IS NOT NULL AND phase IN ('ready', 'calling') AND
(? OR last_seen_at <= ? OR NOT EXISTS (SELECT 1 FROM browser_grants g WHERE g.id = browser_grant_id
AND g.revoked_at IS NULL AND g.issued_at <= ? AND g.expires_at > ?))`,
		restart, now.UnixMilli()-app.ConnectionExecutionHeartbeatTimeout.Milliseconds(), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return 0, err
	}
	reviews, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	// Also recover v10 rows already interrupted before outcome tracking.
	if err := recordInterruptedSend(ctx, tx, now); err != nil {
		return 0, err
	}
	return connections + reviews, nil
}
