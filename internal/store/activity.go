package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) ListActivity(ctx context.Context, now time.Time, after *app.ActivityKey, limit int) ([]app.Activity, error) {
	createdBefore := int64(math.MaxInt64)
	var id string
	var kind app.WalletRequestKind
	if after != nil {
		createdBefore, id, kind = after.CreatedAt.UnixMilli(), after.ID, after.Kind
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT c.created_at, c.id, 'connection' AS kind, c.client_name, c.chain_id,
       CASE WHEN c.state = 'pending' AND c.expires_at <= ?1 THEN 'expired' ELSE c.state END,
       coalesce(s.id, ''), ''
FROM connection_requests c LEFT JOIN sessions s ON s.connection_id = c.id
WHERE (c.created_at, c.id, 'connection') < (?2, ?3, ?4)
  AND NOT EXISTS (SELECT 1 FROM removed_activities WHERE kind = 'connection' AND request_id = c.id)
UNION ALL
SELECT t.created_at, t.id, 'transaction' AS kind, t.client_name, t.chain_id,
       CASE WHEN t.state = 'pending' AND t.expires_at <= ?1 THEN 'expired' ELSE t.state END,
       t.session_id, t.tx_hash
FROM transaction_status t
WHERE (t.created_at, t.id, 'transaction') < (?2, ?3, ?4)
  AND NOT EXISTS (SELECT 1 FROM removed_activities WHERE kind = 'transaction' AND request_id = t.id)
ORDER BY created_at DESC, id DESC, kind DESC LIMIT ?5`, now.UnixMilli(), createdBefore, id, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []app.Activity
	for rows.Next() {
		var record app.Activity
		var created int64
		if err := rows.Scan(&created, &record.ID, &record.Kind, &record.ClientName, &record.ChainID, &record.State, &record.SessionID, &record.TxHash); err != nil {
			return nil, err
		}
		record.CreatedAt = time.UnixMilli(created).UTC()
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) RemoveActivity(ctx context.Context, grantID string, kind app.WalletRequestKind, id string) error {
	var expire, readState string
	switch kind {
	case app.WalletConnection:
		expire = "UPDATE connection_requests SET state = 'expired', finished_at = expires_at WHERE state = 'pending' AND expires_at <= ? AND id = ?"
		readState = "SELECT state FROM connection_requests WHERE id = ?"
	case app.WalletTransaction:
		expire = expireDueTransactions + " AND id = ?"
		readState = "SELECT state FROM transaction_status WHERE id = ?"
	default:
		return app.ErrActivityNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now()
	if err := requireActiveGrant(ctx, tx, grantID, now); err != nil {
		return err
	}
	// Persist effective expiry before hiding a row already shown as expired.
	if _, err := tx.ExecContext(ctx, expire, now.UnixMilli(), id); err != nil {
		return err
	}
	var state string
	err = tx.QueryRowContext(ctx, readState, id).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return app.ErrActivityNotFound
	}
	if err != nil {
		return err
	}
	if !app.ActivityRemovable(state) {
		return app.ErrActivityUnfinished
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO removed_activities(kind, request_id) VALUES (?, ?) ON CONFLICT DO NOTHING", kind, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
