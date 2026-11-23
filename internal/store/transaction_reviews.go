package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const reviewSelect = `SELECT transaction_id, id, browser_grant_id, phase, last_seen_at, coalesce(call_id, '')
FROM wallet_executions WHERE transaction_id = ?`

func (s *Store) GetTransactionReview(ctx context.Context, id string) (*app.TransactionReview, error) {
	return scanReview(s.db.QueryRowContext(ctx, reviewSelect, id))
}

func scanReview(row *sql.Row) (*app.TransactionReview, error) {
	var review app.TransactionReview
	var seen int64
	err := row.Scan(&review.TransactionID, &review.ID, &review.BrowserGrantID, &review.Phase, &seen, &review.CallID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	review.LastSeenAt = time.UnixMilli(seen).UTC()
	return &review, nil
}

// ApplyTransactionReview coordinates preparation only. Invoke is acknowledged
// after commit and is never granted on replay or for a transaction send.
func (s *Store) ApplyTransactionReview(ctx context.Context, ref app.TransactionReviewRef, cmd app.ReviewCommand, now time.Time) (app.ReviewResult, error) {
	if !validID(ref.TransactionID) || !validID(ref.ID) || !validID(ref.BrowserGrantID) || !validTime(now) || !validReviewCommand(ref, cmd) {
		return app.ReviewResult{}, app.ErrInvalidTransaction
	}
	tx, err := s.beginWalletExecution(ctx, ref.TransactionID, ref.ID, ref.BrowserGrantID, now)
	if err != nil {
		return app.ReviewResult{}, err
	}
	defer tx.Rollback()
	req, err := transactionStatusInTx(ctx, tx, ref.TransactionID, now, false)
	if err != nil {
		return app.ReviewResult{}, err
	}
	current, err := scanReview(tx.QueryRowContext(ctx, reviewSelect, ref.TransactionID))
	if err != nil {
		return app.ReviewResult{}, err
	}
	var before app.TransactionReview
	if current != nil {
		before = *current
	}
	result, decision := decideReview(before, ref, cmd, req, now.UTC().Truncate(time.Millisecond))
	if decision == nil && result.Review != before {
		decision = saveReview(ctx, tx, before.ID, result.Review)
		if decision != nil && !errors.Is(decision, app.ErrWalletBusy) {
			return app.ReviewResult{}, decision
		}
	}
	if err := tx.Commit(); err != nil {
		return app.ReviewResult{}, err
	}
	if decision != nil {
		return app.ReviewResult{}, decision
	}
	return result, nil
}

func saveReview(ctx context.Context, tx *sql.Tx, previousID string, next app.TransactionReview) error {
	if next.ID != previousID {
		var busy bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM wallet_executions WHERE "+executionOccupied+" AND transaction_id IS NOT ?)", next.TransactionID).Scan(&busy)
		if err != nil {
			return err
		}
		if busy {
			return app.ErrWalletBusy
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO wallet_executions
(transaction_id, id, browser_grant_id, phase, last_seen_at, call_id) VALUES (?, ?, ?, ?, ?, nullif(?, ''))
ON CONFLICT (transaction_id) DO UPDATE SET id=excluded.id, browser_grant_id=excluded.browser_grant_id,
phase=excluded.phase, last_seen_at=excluded.last_seen_at, call_id=excluded.call_id`,
		next.TransactionID, next.ID, next.BrowserGrantID, next.Phase, next.LastSeenAt.UnixMilli(), next.CallID)
	return executionWriteError(err, app.ErrTransactionReviewConflict)
}

func validReviewCommand(ref app.TransactionReviewRef, cmd app.ReviewCommand) bool {
	switch cmd.Action {
	case app.RecoverReview:
		return validID(cmd.PreviousID) && cmd.PreviousID != ref.ID && cmd.CallID == ""
	case app.BeginPreparationCall:
		return validID(cmd.CallID) && (cmd.PreviousID == "" || validID(cmd.PreviousID)) && cmd.CallID != cmd.PreviousID
	case app.ReturnPreparationCall:
		return validID(cmd.CallID) && cmd.PreviousID == ""
	case app.ClaimReview, app.HeartbeatReview, app.InterruptReview:
		return cmd.PreviousID == "" && cmd.CallID == ""
	default:
		return false
	}
}

func decideReview(current app.TransactionReview, ref app.TransactionReviewRef, cmd app.ReviewCommand, req app.TransactionRequest, now time.Time) (app.ReviewResult, error) {
	if now.Before(req.CreatedAt) {
		return app.ReviewResult{}, app.ErrInvalidTransaction
	}
	pending := req.State == app.TransactionPending
	owner := current.TransactionReviewRef == ref
	live := current.Phase == app.ExecutionReady || current.Phase == app.ExecutionCalling
	create := func() app.ReviewResult {
		seen := now
		if current.LastSeenAt.After(seen) {
			seen = current.LastSeenAt
		}
		return app.ReviewResult{Review: app.TransactionReview{TransactionReviewRef: ref, Phase: app.ExecutionReady, LastSeenAt: seen}}
	}
	switch cmd.Action {
	case app.ClaimReview:
		if current.ID == "" && pending {
			return create(), nil
		}
		if !owner || !pending || !live {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
	case app.RecoverReview:
		if owner {
			break // Recovery response was lost. Return the durable phase.
		}
		if current.ID != cmd.PreviousID {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		if !pending {
			current.Phase = app.ExecutionFinished
			break
		}
		if current.Phase != app.ExecutionInterruptedReady && current.Phase != app.ExecutionInterruptedCalling {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		return create(), nil
	default:
		if !owner {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		return advanceOwnedReview(current, cmd, pending, now)
	}
	return app.ReviewResult{Review: current}, nil
}

func advanceOwnedReview(review app.TransactionReview, cmd app.ReviewCommand, pending bool, now time.Time) (app.ReviewResult, error) {
	switch cmd.Action {
	case app.HeartbeatReview:
		if !pending || (review.Phase != app.ExecutionReady && review.Phase != app.ExecutionCalling) {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		if now.After(review.LastSeenAt) {
			review.LastSeenAt = now
		}
	case app.InterruptReview:
		switch review.Phase {
		case app.ExecutionReady:
			review.Phase = app.ExecutionInterruptedReady
		case app.ExecutionCalling:
			review.Phase = app.ExecutionInterruptedCalling
		}
	case app.BeginPreparationCall:
		if review.CallID == cmd.CallID {
			break
		}
		if !pending || review.Phase != app.ExecutionReady || review.CallID != cmd.PreviousID {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		review.CallID, review.Phase = cmd.CallID, app.ExecutionCalling
		return app.ReviewResult{Review: review, Invoke: true}, nil
	case app.ReturnPreparationCall:
		if review.CallID != cmd.CallID {
			return app.ReviewResult{}, app.ErrTransactionReviewConflict
		}
		switch review.Phase {
		case app.ExecutionCalling:
			review.Phase = app.ExecutionReady
		case app.ExecutionInterruptedCalling:
			review.Phase = app.ExecutionInterruptedReady
			if !pending {
				review.Phase = app.ExecutionFinished
			}
		}
	}
	return app.ReviewResult{Review: review}, nil
}
