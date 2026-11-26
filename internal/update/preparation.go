package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

var ErrStopRefused = errors.New("the service declined the upgrade stop")

const checkEnv = "WALLET_BRIDGE_UPGRADE_WALLET_CHECK"

// WalletCheck identifies the old service allowed to start for an occupancy check.
type WalletCheck struct {
	id, dir  string
	previous Candidate
}

func (c *WalletCheck) Version() string {
	if c == nil {
		return ""
	}
	return c.previous.Version
}

func (c *WalletCheck) CheckAccess(dir string) (UpgradeBlock, error) {
	if c == nil {
		return CheckUpgrade(dir)
	}
	record, err := ownedRecord(dir, c.id, phasePreparing)
	if err != nil {
		return "", err
	}
	if c.dir != dir || record.Previous.SHA256 != c.previous.SHA256 {
		return "", errors.New("wallet check does not match the saved program")
	}
	return "", nil
}

func (c *WalletCheck) Start(dir string) (*platform.BackgroundProcess, error) {
	return platform.StartBackground(c.previous.Path, dir, checkEnv+"="+c.id)
}

func ownedRecord(dir, id, phase string) (*upgradeRecord, error) {
	busy, err := platform.ServiceLocked(filepath.Join(dir, upgradeLockDir))
	if err != nil {
		return nil, err
	}
	if !busy {
		return nil, errors.New("the upgrade owner is no longer running")
	}
	r, err := readUpgradeRecord(dir)
	if err != nil {
		return nil, err
	}
	if r == nil || r.ID != id || r.Phase != phase {
		return nil, errors.New("the upgrade ID or phase has changed")
	}
	return r, nil
}

// Called under the service lock. Only the retained program can use this startup path.
func IsWalletCheckService(dir string) (bool, error) {
	id := os.Getenv(checkEnv)
	if id == "" {
		return false, nil
	}
	record, err := ownedRecord(dir, id, phasePreparing)
	if err != nil {
		return false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	return true, verifyExecutable(exe, record.Previous.SHA256)
}

// Prepare checks and stops the old service, then publishes the pre-migration backup.
// The caller holds the upgrade lock through candidate verification and admission.
func (w *Upgrade) Prepare(ctx context.Context, id string, target Candidate, stop func(context.Context, WalletCheck) error) error {
	if w.current != nil && w.current.Failure != "" {
		return UpgradeRecoveryRequired
	}
	if w.current != nil && w.current.Phase == phaseAdmitted {
		if w.current.ID != id || w.current.Target != (releaseSelection{target.Version, target.SHA256}) {
			return UpgradeRecoveryRequired
		}
		return w.recoverAdmittedSelection()
	}
	old, err := w.preparePrograms(ctx, id, releaseSelection{target.Version, target.SHA256})
	if err != nil {
		return err
	}
	if w.current.Phase == phasePreparing {
		if err := stop(ctx, WalletCheck{id: id, dir: w.dir, previous: old}); err != nil {
			if errors.Is(err, ErrStopRefused) {
				if removeErr := os.Remove(filepath.Join(w.dir, upgradeRecordName)); removeErr != nil {
					return errors.Join(err, removeErr)
				}
				w.current = nil
			}
			return err
		}
		if err := w.advance(phaseStopped); err != nil {
			return err
		}
	}
	if w.current.Phase == phaseVerifying {
		return withBackupLock(w.dir, id, func(backup string) error {
			if err := platform.CheckPrivateDir(backup); err != nil {
				return err
			}
			return withVerifiedCopy(ctx, backup, os.RemoveAll)
		})
	}
	if err := backupStoppedService(ctx, w.dir, id, true); err != nil {
		return err
	}
	return w.advance(phaseVerifying)
}
