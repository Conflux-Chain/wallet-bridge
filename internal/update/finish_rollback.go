package update

import (
	"context"
	"errors"
	"fmt"
)

// RolledBackError records an unsuccessful upgrade after old-service verification.
type RolledBackError struct {
	Target, Restored string
	Cause            error
}

func (e *RolledBackError) Error() string {
	return fmt.Sprintf("upgrade to %s failed: %v; restored %s", e.Target, e.Cause, e.Restored)
}

func (e *RolledBackError) Unwrap() error { return e.Cause }

func (w *Upgrade) finishRollback(ctx context.Context, cause error, probe func(context.Context, string, string) (bool, error)) error {
	if w.current.Phase == phaseRolledBack {
		// New requests may exist. Only repair the selection and finish cleanup.
		if err := w.recoverAdmittedSelection(); err != nil {
			return errors.Join(cause, err)
		}
	} else {
		if err := w.RestorePreviousData(ctx, cause); err != nil {
			return errors.Join(cause, err)
		}
		if err := w.VerifyCandidate(ctx, probe); err != nil {
			return errors.Join(cause, fmt.Errorf("verify restored service: %w", err))
		}
		if err := w.commitAdmission(ctx, phaseRolledBack); err != nil {
			return errors.Join(cause, err)
		}
	}
	result := &RolledBackError{Target: w.current.Target.Version, Restored: w.current.Previous.Version, Cause: cause}
	return errors.Join(result, CleanupCompletedBackup(w.dir, w.current.ID))
}
