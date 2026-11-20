package store

import (
	"context"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (s *Store) BeginConnectionCall(ctx context.Context, ref app.ConnectionCallRef, previousCallID string, now time.Time) (app.ConnectionCallPermit, error) {
	if !validID(ref.CallID) || (previousCallID != "" && !validID(previousCallID)) || ref.CallID == previousCallID {
		return app.ConnectionCallPermit{}, app.ErrInvalidConnectionExecution
	}
	tx, err := s.beginExecution(ctx, ref.ConnectionExecutionRef, now)
	if err != nil {
		return app.ConnectionCallPermit{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionCallPermit{}, err
	}
	if current.ConnectionExecutionRef != ref.ConnectionExecutionRef {
		result, err := commitExecution(tx, current, app.ErrConnectionExecutionConflict)
		return app.ConnectionCallPermit{Execution: result}, err
	}
	if current.CallID == ref.CallID {
		result, err := commitExecution(tx, current, nil)
		return app.ConnectionCallPermit{Execution: result}, err
	}
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionCallPermit{}, err
	}
	if current.Phase != app.ExecutionReady || current.CallID != previousCallID || !pendingExecutionRequest(request, now) {
		result, err := commitExecution(tx, current, app.ErrConnectionExecutionConflict)
		return app.ConnectionCallPermit{Execution: result}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE connection_executions SET phase = 'calling', call_id = ? WHERE connection_id = ?", ref.CallID, ref.ConnectionID); err != nil {
		return app.ConnectionCallPermit{}, err
	}
	current.Phase, current.CallID = app.ExecutionCalling, ref.CallID
	result, err := commitExecution(tx, current, nil)
	return app.ConnectionCallPermit{Execution: result, Invoke: err == nil}, err
}

func (s *Store) ReturnConnectionCall(ctx context.Context, ref app.ConnectionCallRef, now time.Time) (app.ConnectionExecution, error) {
	if !validID(ref.CallID) {
		return app.ConnectionExecution{}, app.ErrInvalidConnectionExecution
	}
	tx, err := s.beginExecution(ctx, ref.ConnectionExecutionRef, now)
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	defer tx.Rollback()
	current, err := scanExecution(tx.QueryRowContext(ctx, executionSelect+" WHERE connection_id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if current.ConnectionExecutionRef != ref.ConnectionExecutionRef || current.CallID != ref.CallID {
		return commitExecution(tx, current, app.ErrConnectionExecutionConflict)
	}
	if current.Phase != app.ExecutionCalling && current.Phase != app.ExecutionInterruptedCalling {
		return commitExecution(tx, current, nil)
	}
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", ref.ConnectionID))
	if err != nil {
		return app.ConnectionExecution{}, err
	}
	if now.Before(request.CreatedAt) {
		return app.ConnectionExecution{}, app.ErrInvalidConnectionExecution
	}
	phase := app.ExecutionReady
	if !pendingExecutionRequest(request, now) {
		phase = app.ExecutionFinished
	} else if current.Phase == app.ExecutionInterruptedCalling {
		phase = app.ExecutionInterruptedReady
	}
	if _, err := tx.ExecContext(ctx, "UPDATE connection_executions SET phase = ? WHERE connection_id = ?", phase, ref.ConnectionID); err != nil {
		return app.ConnectionExecution{}, err
	}
	current.Phase = phase
	return commitExecution(tx, current, nil)
}
