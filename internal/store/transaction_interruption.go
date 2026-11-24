package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

// Only one execution can occupy the wallet. The handoff timestamp distinguishes
// a send from an interrupted preparation call on the same transaction.
func recordInterruptedSend(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO transaction_results (transaction_id, outcome, tx_hash, reported_at)
SELECT r.id, 'uncertain', '', max(r.handed_off_at, ?)
FROM wallet_executions w JOIN transaction_status r ON r.id = w.transaction_id
WHERE `+executionOccupied+` AND w.phase = 'interrupted_calling' AND r.state = 'awaiting_wallet'`, now.UnixMilli())
	return err
}

// AcknowledgeTransactionInterruption releases the original popup wait. Any
// authorized management page can acknowledge it without knowing the call ID.
func (s *Store) AcknowledgeTransactionInterruption(ctx context.Context, id, browserGrantID string) (app.TransactionRequest, error) {
	return s.acknowledgeTransactionInterruption(ctx, id, browserGrantID, time.Now)
}

func (s *Store) acknowledgeTransactionInterruption(ctx context.Context, id, browserGrantID string, clock func() time.Time) (app.TransactionRequest, error) {
	if !validID(id) || !validID(browserGrantID) {
		return app.TransactionRequest{}, app.ErrInvalidTransaction
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	defer tx.Rollback()
	now := clock()
	if err := checkWalletExecution(ctx, tx, browserGrantID, now); err != nil {
		return app.TransactionRequest{}, err
	}
	req, err := scanTransactionRequest(tx.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", id))
	if err != nil {
		return app.TransactionRequest{}, err
	}
	review, err := scanReview(tx.QueryRowContext(ctx, reviewSelect, id))
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if req.HandedOffAt == nil || review == nil {
		return app.TransactionRequest{}, app.ErrTransactionReviewConflict
	}
	switch review.Phase {
	case app.ExecutionInterruptedCalling:
		_, err = tx.ExecContext(ctx, "UPDATE wallet_executions SET phase = 'finished' WHERE transaction_id = ?", id)
		if err != nil {
			return app.TransactionRequest{}, err
		}
	case app.ExecutionFinished:
		// The user or the wallet already released this request.
	default:
		return app.TransactionRequest{}, app.ErrTransactionReviewConflict
	}
	if err := tx.Commit(); err != nil {
		return app.TransactionRequest{}, err
	}
	return req, nil
}
