package store

import (
	"context"
	"errors"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) CompleteConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, completion app.ConnectionCompletion, now time.Time) (app.ConnectionRequest, error) {
	if !validCompletion(completion) {
		return app.ConnectionRequest{}, app.ErrInvalidSession
	}
	return s.decideConnectionExecution(ctx, ref, app.ConnectionSucceeded, completion, now)
}

func (s *Store) RejectConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, now time.Time) (app.ConnectionRequest, error) {
	return s.decideConnectionExecution(ctx, ref, app.ConnectionRejected, app.ConnectionCompletion{}, now)
}

func (s *Store) decideConnectionExecution(ctx context.Context, ref app.ConnectionExecutionRef, desired app.ConnectionState, completion app.ConnectionCompletion, now time.Time) (app.ConnectionRequest, error) {
	tx, err := s.beginExecution(ctx, ref, now)
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	commit := func(result app.ConnectionRequest, decision error) (app.ConnectionRequest, error) {
		if err := tx.Commit(); err != nil {
			return app.ConnectionRequest{}, err
		}
		return result, decision
	}
	if current.ConnectionExecutionRef != ref {
		return commit(request, app.ErrConnectionExecutionConflict)
	}
	if request.State == app.ConnectionPending && !now.Before(request.ExpiresAt) {
		// Deadline decisions remain durable even when a wallet call still waits.
		result, decision := transitionConnectionInTx(ctx, tx, ref.ConnectionID, app.ConnectionExpired, app.ConnectionCompletion{}, now.UTC().Truncate(time.Millisecond))
		if decision != nil {
			return app.ConnectionRequest{}, decision
		}
		return commit(result, app.ErrConnectionConflict)
	}
	if (request.State == app.ConnectionPending && current.Phase != app.ExecutionReady) ||
		(request.State != app.ConnectionPending && current.Phase != app.ExecutionFinished) {
		return commit(request, app.ErrConnectionExecutionConflict)
	}
	result, decision := transitionConnectionInTx(ctx, tx, ref.ConnectionID, desired, completion, now.UTC().Truncate(time.Millisecond))
	if decision != nil && !errors.Is(decision, app.ErrConnectionConflict) {
		return app.ConnectionRequest{}, decision
	}
	if decision == nil && current.Phase != app.ExecutionFinished {
		if _, err := tx.ExecContext(ctx, "UPDATE wallet_executions SET phase = 'finished' WHERE connection_id = ?", ref.ConnectionID); err != nil {
			return app.ConnectionRequest{}, err
		}
	}
	return commit(result, decision)
}
