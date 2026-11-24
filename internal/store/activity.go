package store

import (
	"context"
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
UNION ALL
SELECT t.created_at, t.id, 'transaction' AS kind, t.client_name, t.chain_id,
       CASE WHEN t.state = 'pending' AND t.expires_at <= ?1 THEN 'expired' ELSE t.state END,
       t.session_id, t.tx_hash
FROM transaction_status t
WHERE (t.created_at, t.id, 'transaction') < (?2, ?3, ?4)
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
