package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const transactionSelect = `SELECT id, session_id, client_name, intent, from_address, to_address,
chain_id, value, data, gas, gas_price, max_fee_per_gas, max_priority_fee_per_gas, created_at, expires_at, state, finished_at, handed_off_at, tx_hash
FROM transaction_status`

const expireDueTransactions = `UPDATE transaction_requests SET state = 'expired', finished_at = expires_at
WHERE state = 'pending' AND expires_at <= ?`

func (s *Store) GetTransactionRequest(ctx context.Context, id string, now time.Time) (app.TransactionRequest, error) {
	return s.transactionStatus(ctx, id, now, false)
}

func (s *Store) CancelTransactionRequest(ctx context.Context, id string, now time.Time) (app.TransactionRequest, error) {
	return s.transactionStatus(ctx, id, now, true)
}

func (s *Store) RejectTransactionRequest(ctx context.Context, id, grantID string) (app.TransactionRequest, error) {
	if !validID(id) || !validID(grantID) {
		return app.TransactionRequest{}, app.ErrInvalidTransaction
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	defer tx.Rollback()
	now := time.Now()
	if err := requireActiveGrant(ctx, tx, grantID, now); err != nil {
		return app.TransactionRequest{}, err
	}
	req, err := transactionStatusInTx(ctx, tx, id, now, false)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if req.State == app.TransactionPending {
		// Stop pending work with the same trigger used by client cancellation.
		// The result row records the user's decision for history and all readers.
		req, err = cancelPendingTransaction(ctx, tx, req, now)
		if err != nil {
			return app.TransactionRequest{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO transaction_results (transaction_id, outcome, tx_hash, reported_at)
VALUES (?, 'rejected', '', ?)`, id, req.FinishedAt.UnixMilli())
		if err != nil {
			return app.TransactionRequest{}, err
		}
		req.State = app.TransactionRejected
	}
	if err := tx.Commit(); err != nil {
		return app.TransactionRequest{}, err
	}
	return req, nil
}

func (s *Store) transactionStatus(ctx context.Context, id string, now time.Time, cancel bool) (app.TransactionRequest, error) {
	if !validID(id) || !validTime(now) {
		return app.TransactionRequest{}, app.ErrInvalidTransaction
	}
	if !cancel {
		// Acquire the write lock only when a poll observes a deadline.
		req, err := scanTransactionRequest(s.db.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", id))
		if err != nil || req.State != app.TransactionPending || now.Before(req.ExpiresAt) {
			return req, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	defer tx.Rollback()
	req, err := transactionStatusInTx(ctx, tx, id, now, cancel)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.TransactionRequest{}, err
	}
	return req, nil
}

func transactionStatusInTx(ctx context.Context, tx *sql.Tx, id string, now time.Time, cancel bool) (app.TransactionRequest, error) {
	req, err := scanTransactionRequest(tx.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", id))
	if err != nil || req.State != app.TransactionPending {
		return req, err
	}
	if !now.Before(req.ExpiresAt) {
		req.State, req.FinishedAt = app.TransactionExpired, &req.ExpiresAt
		_, err = tx.ExecContext(ctx, "UPDATE transaction_requests SET state = 'expired', finished_at = expires_at WHERE id = ?", id)
		return req, err
	}
	if cancel {
		return cancelPendingTransaction(ctx, tx, req, now)
	}
	return req, nil
}

func cancelPendingTransaction(ctx context.Context, tx *sql.Tx, req app.TransactionRequest, now time.Time) (app.TransactionRequest, error) {
	finished := time.UnixMilli(max(now.UnixMilli(), req.CreatedAt.UnixMilli())).UTC()
	req.State, req.FinishedAt = app.TransactionCanceled, &finished
	_, err := tx.ExecContext(ctx, "UPDATE transaction_requests SET state = 'canceled', finished_at = ? WHERE id = ?", finished.UnixMilli(), req.ID)
	return req, err
}

func scanTransactionRequest(row *sql.Row) (app.TransactionRequest, error) {
	var req app.TransactionRequest
	payload := &req.Transaction
	var created, expires int64
	var finished, handedOff sql.NullInt64
	err := row.Scan(&req.ID, &req.SessionID, &req.ClientName, &req.Intent, &payload.From, &payload.To, &payload.ChainID,
		&payload.Value, &payload.Data, &payload.Gas, &payload.GasPrice, &payload.MaxFeePerGas, &payload.MaxPriorityFeePerGas,
		&created, &expires, &req.State, &finished, &handedOff, &req.TxHash)
	if errors.Is(err, sql.ErrNoRows) {
		return app.TransactionRequest{}, app.ErrTransactionRequestNotFound
	}
	if err != nil {
		return app.TransactionRequest{}, err
	}
	req.CreatedAt, req.ExpiresAt = time.UnixMilli(created).UTC(), time.UnixMilli(expires).UTC()
	if handedOff.Valid {
		at := time.UnixMilli(handedOff.Int64).UTC()
		req.HandedOffAt = &at
	}
	if finished.Valid {
		at := time.UnixMilli(finished.Int64).UTC()
		req.FinishedAt = &at
	}
	return req, nil
}

func (s *Store) CreateTransactionRequest(ctx context.Context, input app.TransactionRequestInput, now time.Time) (app.TransactionRequest, error) {
	if !validID(input.ID) || !validTime(now) || !validTime(now.Add(app.TransactionRequestLifetime)) {
		return app.TransactionRequest{}, fmt.Errorf("%w: invalid request ID or time", app.ErrInvalidTransaction)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	defer tx.Rollback()

	original, err := transactionStatusInTx(ctx, tx, input.ID, now, false)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return app.TransactionRequest{}, err
		}
		if !sameTransactionRequest(original, input) {
			return original, app.ErrTransactionRequestConflict
		}
		return original, nil
	}
	if !errors.Is(err, app.ErrTransactionRequestNotFound) {
		return app.TransactionRequest{}, err
	}
	if !validID(input.SessionID) || strings.TrimSpace(input.ClientName) == "" {
		return app.TransactionRequest{}, fmt.Errorf("%w: invalid Session ID or client name", app.ErrInvalidTransaction)
	}
	session, err := updateSessionInTx(ctx, tx, input.SessionID, now, false)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if session.ClosedAt != nil {
		// An observed idle expiry remains durable even if the clock later moves back.
		if err := tx.Commit(); err != nil {
			return app.TransactionRequest{}, err
		}
		return app.TransactionRequest{}, app.ErrSessionClosed
	}
	payload, err := app.NormalizeTransaction(input.Fields, session)
	if err != nil {
		return app.TransactionRequest{}, err
	}
	created := time.UnixMilli(now.UnixMilli()).UTC()
	req := app.TransactionRequest{
		ID: input.ID, SessionID: session.ID, ClientName: input.ClientName, Intent: input.Intent,
		Transaction: payload, CreatedAt: created, ExpiresAt: created.Add(app.TransactionRequestLifetime), State: app.TransactionPending,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO transaction_requests
(id, session_id, client_name, intent, from_address, to_address, chain_id, value, data, gas, gas_price,
max_fee_per_gas, max_priority_fee_per_gas, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.SessionID, req.ClientName, req.Intent, payload.From, payload.To, payload.ChainID,
		payload.Value, payload.Data, payload.Gas, payload.GasPrice, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas,
		req.CreatedAt.UnixMilli(), req.ExpiresAt.UnixMilli())
	if err != nil {
		return app.TransactionRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.TransactionRequest{}, err
	}
	return req, nil
}

func sameTransactionRequest(original app.TransactionRequest, input app.TransactionRequestInput) bool {
	if original.SessionID != input.SessionID || original.ClientName != input.ClientName || original.Intent != input.Intent {
		return false
	}
	stored := original.Transaction
	binding := app.Session{Account: stored.From, ChainID: stored.ChainID}
	payload, err := app.NormalizeTransaction(input.Fields, binding)
	if err != nil || !strings.EqualFold(payload.To, stored.To) {
		return false
	}
	payload.To = stored.To
	return payload == stored
}
