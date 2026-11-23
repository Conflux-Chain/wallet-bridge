package store

import (
	"context"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

// MaintainConnectionState commits deadlines and execution interruption together.
// restart is used only by explicit service startup, never by Store.Open.
func (s *Store) MaintainConnectionState(ctx context.Context, now time.Time, restart bool) (app.ConnectionMaintenanceCounts, error) {
	if !validTime(now) {
		return app.ConnectionMaintenanceCounts{}, app.ErrInvalidConnectionExecution
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.ConnectionMaintenanceCounts{}, err
	}
	defer tx.Rollback()
	counts, err := expireConnectionsInTx(ctx, tx, now)
	if err != nil {
		return app.ConnectionMaintenanceCounts{}, err
	}
	executions, err := maintainWalletExecutions(ctx, tx, now, restart)
	if err != nil {
		return app.ConnectionMaintenanceCounts{}, err
	}
	if err := tx.Commit(); err != nil {
		return app.ConnectionMaintenanceCounts{}, err
	}
	return app.ConnectionMaintenanceCounts{ConnectionExpiryCounts: counts, Executions: executions}, nil
}
