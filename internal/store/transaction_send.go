package store

import (
	"context"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

// GrantTransactionSend returns the frozen payload only to the caller that
// commits the handoff. A replay of that call returns (nil, nil).
func (s *Store) GrantTransactionSend(ctx context.Context, ref app.TransactionReviewRef, input app.TransactionSendInput) (*app.Transaction, error) {
	return s.grantTransactionSend(ctx, ref, input, time.Now)
}

func (s *Store) grantTransactionSend(ctx context.Context, ref app.TransactionReviewRef, input app.TransactionSendInput, clock func() time.Time) (*app.Transaction, error) {
	if !validID(ref.TransactionID) || !validID(ref.ID) || !validID(ref.BrowserGrantID) || !validID(input.CallID) ||
		(input.PreviousCallID != "" && !validID(input.PreviousCallID)) || input.CallID == input.PreviousCallID {
		return nil, app.ErrInvalidTransaction
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Sample the deadline after acquiring the immediate write lock. Another
	// writer may have held it through the last millisecond of approval.
	now := clock()
	if err := checkWalletExecution(ctx, tx, ref.BrowserGrantID, now); err != nil {
		return nil, err
	}
	// Deadline and heartbeat decisions observed in this transaction stay durable
	// even when they disqualify the caller.
	finish := func(payload *app.Transaction, decision error) (*app.Transaction, error) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return payload, decision
	}
	req, err := transactionStatusInTx(ctx, tx, ref.TransactionID, now, false)
	if err != nil {
		return nil, err
	}
	review, err := scanReview(tx.QueryRowContext(ctx, reviewSelect, ref.TransactionID))
	if err != nil {
		return nil, err
	}
	if review == nil || review.TransactionReviewRef != ref {
		return finish(nil, app.ErrTransactionReviewConflict)
	}
	if req.HandedOffAt != nil && review.CallID == input.CallID {
		return finish(nil, nil)
	}
	if req.State != app.TransactionPending || review.Phase != app.ExecutionReady || review.CallID != input.PreviousCallID {
		return finish(nil, app.ErrTransactionReviewConflict)
	}
	if now.Before(req.CreatedAt) {
		return finish(nil, app.ErrInvalidTransaction)
	}
	session, err := scanSession(tx.QueryRowContext(ctx, sessionSelect+" WHERE id = ?", req.SessionID))
	if err != nil {
		return nil, err
	}
	if session.ClosedAt != nil {
		return finish(nil, app.ErrSessionClosed)
	}
	if !input.Binding.MatchesSession(session) || req.Transaction.From != session.Account || req.Transaction.ChainID != session.ChainID {
		return finish(nil, app.ErrInvalidSession)
	}
	_, err = tx.ExecContext(ctx, "UPDATE transaction_requests SET state = 'awaiting_wallet', handed_off_at = ? WHERE id = ?", now.UnixMilli(), req.ID)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE wallet_executions SET phase = 'calling', call_id = ?, last_seen_at = max(last_seen_at, ?)
WHERE transaction_id = ?`, input.CallID, now.UnixMilli(), req.ID)
	if err != nil {
		return nil, err
	}
	return finish(&req.Transaction, nil)
}
