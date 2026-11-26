package update

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// Admit publishes the selected executable before committing the decision that
// makes the old snapshot ineligible for rollback.
func (w *Upgrade) Admit(ctx context.Context, probe func(context.Context, string, string) (bool, error)) error {
	if w.current != nil && w.current.Failure != "" {
		return w.finishRollback(ctx, errors.New(w.current.Failure), probe)
	}
	if w.current != nil && w.current.Phase == phaseAdmitted {
		return w.recoverAdmittedSelection()
	}
	if err := w.VerifyCandidate(ctx, probe); err != nil {
		if ctx.Err() != nil || w.current == nil || w.current.Phase != phaseVerifying {
			return err
		}
		return w.finishRollback(ctx, err, probe)
	}
	return w.commitAdmission(ctx, phaseAdmitted)
}

func (w *Upgrade) commitAdmission(ctx context.Context, phase string) error {
	if err := w.current.selection().publish(w.dir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return withBackupLock(w.dir, w.current.ID, func(_ string) error {
		return w.advance(phase)
	})
}

func checkAdmittedSelection(dir string, target activeSelection) error {
	active := filepath.Join(dir, "active")
	if err := platform.CheckPrivateDir(active); err != nil {
		return err
	}
	selected, err := readSelectionRecord(active)
	if err != nil {
		return err
	}
	if selected != target {
		return errors.New("the enabled version differs from the committed upgrade")
	}
	return nil
}

func (w *Upgrade) recoverAdmittedSelection() error {
	selection := w.current.selection()
	if err := checkAdmittedSelection(w.dir, selection); err == nil {
		_, err = selection.program(w.dir)
		return err
	}
	return selection.publish(w.dir)
}

// CandidateAdmitted observes the writer's decision under a shared lock. It
// cannot expose the atomic rename while the writer is still syncing it.
func CandidateAdmitted(dir, id string) (bool, error) {
	lock, err := platform.ObserveServiceLock(filepath.Join(dir, upgradeLockDir))
	if errors.Is(err, platform.ErrLocked) {
		// During rollback the owner must keep its lock while waiting for the
		// candidate to release the database. Never admit from this read.
		r, err := readUpgradeRecord(dir)
		if err != nil {
			return false, err
		}
		if r == nil || r.ID != id || r.Phase == phaseRestoring {
			return false, errors.New("the candidate is no longer being verified")
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if lock != nil {
		defer lock.Close()
	}
	r, err := readUpgradeRecord(dir)
	if err != nil {
		return false, err
	}
	if r == nil || r.ID != id || !r.committed() {
		return false, errors.New("upgrade owner exited before committing admission")
	}
	return true, checkAdmittedSelection(dir, r.selection())
}
