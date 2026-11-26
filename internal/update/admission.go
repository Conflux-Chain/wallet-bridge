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
	if w.current != nil && w.current.Phase == phaseAdmitted {
		return w.recoverAdmittedSelection()
	}
	if err := w.VerifyCandidate(ctx, probe); err != nil {
		return err
	}
	if err := publishSelection(w.dir, w.current.Target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return withBackupLock(w.dir, w.current.ID, func(_ string) error {
		return w.advance(phaseAdmitted)
	})
}

func checkAdmittedSelection(dir string, target releaseSelection) error {
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
	if err := checkAdmittedSelection(w.dir, w.current.Target); err == nil {
		_, err = w.current.Target.candidate(w.dir)
		return err
	}
	return publishSelection(w.dir, w.current.Target)
}

// CandidateAdmitted observes the writer's decision under a shared lock. It
// cannot expose the atomic rename while the writer is still syncing it.
func CandidateAdmitted(dir, id string) (bool, error) {
	lock, err := platform.ObserveServiceLock(filepath.Join(dir, upgradeLockDir))
	if errors.Is(err, platform.ErrLocked) {
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
	if r == nil || r.ID != id || r.Phase != phaseAdmitted {
		return false, errors.New("upgrade owner exited before committing admission")
	}
	return true, checkAdmittedSelection(dir, r.Target)
}
