package store

import (
	"context"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) ListPendingWalletRequests(ctx context.Context, now time.Time) ([]app.PendingWalletRequest, error) {
	// Filter by the deadline even when periodic maintenance has not run yet.
	// A transaction leaves pending before the wallet receives the send call.
	rows, err := s.db.QueryContext(ctx, `SELECT 'connection' AS kind, id, client_name, chain_id, created_at, expires_at
FROM connection_requests WHERE state = 'pending' AND expires_at > ?1
UNION ALL
SELECT 'transaction', id, client_name, chain_id, created_at, expires_at
FROM transaction_requests WHERE state = 'pending' AND expires_at > ?1
ORDER BY created_at, kind, id`, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []app.PendingWalletRequest
	for rows.Next() {
		var request app.PendingWalletRequest
		var created, expires int64
		if err := rows.Scan(&request.Kind, &request.ID, &request.ClientName, &request.ChainID, &created, &expires); err != nil {
			return nil, err
		}
		request.CreatedAt = time.UnixMilli(created).UTC()
		request.ExpiresAt = time.UnixMilli(expires).UTC()
		requests = append(requests, request)
	}
	return requests, rows.Err()
}
