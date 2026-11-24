package store

import (
	"context"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) RecordTransactionHash(ctx context.Context, ref app.TransactionSendRef, hash string) (app.TransactionRequest, error) {
	return s.recordTransactionHash(ctx, ref, hash, time.Now)
}

func (s *Store) recordTransactionHash(ctx context.Context, ref app.TransactionSendRef, hash string, clock func() time.Time) (app.TransactionRequest, error) {
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") || strings.Trim(hash[2:], "0123456789abcdefABCDEF") != "" {
		return app.TransactionRequest{}, app.ErrInvalidTransaction
	}
	return s.recordWalletResult(ctx, ref, app.TransactionSent, strings.ToLower(hash), clock)
}

// A provider failure without an EIP-1193 code can be reported with code zero.
func (s *Store) RecordTransactionError(ctx context.Context, ref app.TransactionSendRef, code int) (app.TransactionRequest, error) {
	return s.recordTransactionError(ctx, ref, code, time.Now)
}

func (s *Store) recordTransactionError(ctx context.Context, ref app.TransactionSendRef, code int, clock func() time.Time) (app.TransactionRequest, error) {
	state := app.TransactionUncertain
	if code == 4001 {
		state = app.TransactionRejected
	}
	return s.recordWalletResult(ctx, ref, state, "", clock)
}

func (s *Store) recordWalletResult(ctx context.Context, ref app.TransactionSendRef, state app.TransactionState, hash string, clock func() time.Time) (app.TransactionRequest, error) {
	if !validID(ref.TransactionID) || !validID(ref.ID) || !validID(ref.BrowserGrantID) || !validID(ref.CallID) {
		return app.TransactionRequest{}, app.ErrInvalidTransaction
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	defer tx.Rollback()
	now := clock()
	if err := checkWalletExecution(ctx, tx, ref.BrowserGrantID, now); err != nil {
		return app.TransactionRequest{}, err
	}
	req, err := scanTransactionRequest(tx.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", ref.TransactionID))
	if err != nil {
		return app.TransactionRequest{}, err
	}
	// The current subject was authenticated above. A replacement grant may
	// report only a known call after the original grant ended (D-050).
	var matchingCall bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (
  SELECT 1 FROM wallet_executions w
  JOIN browser_grants original ON original.id = w.browser_grant_id
  JOIN browser_grants caller ON caller.id = ?
  WHERE w.transaction_id = ? AND w.id = ? AND w.call_id = ?
    AND (caller.id = original.id OR
         caller.issued_at >= min(original.expires_at, coalesce(original.revoked_at, original.expires_at))))`,
		ref.BrowserGrantID, ref.TransactionID, ref.ID, ref.CallID).Scan(&matchingCall)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if req.HandedOffAt == nil || !matchingCall {
		return app.TransactionRequest{}, app.ErrTransactionReviewConflict
	}
	switch {
	case req.State == app.TransactionAwaitingWallet || (req.State == app.TransactionUncertain && state != app.TransactionUncertain):
		reported := max(now.UnixMilli(), req.HandedOffAt.UnixMilli())
		if req.FinishedAt != nil {
			reported = max(reported, req.FinishedAt.UnixMilli())
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO transaction_results (transaction_id, outcome, tx_hash, reported_at) VALUES (?, ?, ?, ?)
ON CONFLICT (transaction_id) DO UPDATE SET outcome=excluded.outcome, tx_hash=excluded.tx_hash, reported_at=excluded.reported_at`,
			req.ID, state, hash, reported)
		if err != nil {
			return app.TransactionRequest{}, err
		}
		at := time.UnixMilli(reported).UTC()
		req.State, req.TxHash, req.FinishedAt = state, hash, &at
	case state == app.TransactionUncertain || (req.State == state && req.TxHash == hash):
		// A repeated or delayed inconclusive report returns the best known result.
	default:
		return req, app.ErrTransactionResultConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE wallet_executions SET phase = 'finished'
WHERE transaction_id = ? AND phase IN ('calling', 'interrupted_calling')`, req.ID)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.TransactionRequest{}, err
	}
	return req, nil
}
