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
chain_id, value, data, gas, gas_price, max_fee_per_gas, max_priority_fee_per_gas, created_at, expires_at
FROM transaction_requests`

func (s *Store) GetTransactionRequest(ctx context.Context, id string) (app.TransactionRequest, error) {
	return scanTransactionRequest(s.db.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", id))
}

func scanTransactionRequest(row *sql.Row) (app.TransactionRequest, error) {
	var req app.TransactionRequest
	payload := &req.Transaction
	var created, expires int64
	err := row.Scan(&req.ID, &req.SessionID, &req.ClientName, &req.Intent, &payload.From, &payload.To, &payload.ChainID,
		&payload.Value, &payload.Data, &payload.Gas, &payload.GasPrice, &payload.MaxFeePerGas, &payload.MaxPriorityFeePerGas,
		&created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return app.TransactionRequest{}, app.ErrTransactionRequestNotFound
	}
	if err != nil {
		return app.TransactionRequest{}, err
	}
	req.CreatedAt, req.ExpiresAt = time.UnixMilli(created).UTC(), time.UnixMilli(expires).UTC()
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

	original, err := scanTransactionRequest(tx.QueryRowContext(ctx, transactionSelect+" WHERE id = ?", input.ID))
	if err == nil {
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
		Transaction: payload, CreatedAt: created, ExpiresAt: created.Add(app.TransactionRequestLifetime),
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
