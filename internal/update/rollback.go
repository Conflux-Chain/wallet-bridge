package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

var ErrRollbackAfterAdmission = errors.New("the upgrade already committed admission, restoring the snapshot would discard new data")

// RollbackFromBackup supplies a verified, disposable copy to the version
// switcher. The switcher must allow rollback only before admission and return
// success only after the old service is ready and that outcome is durable.
func RollbackFromBackup(ctx context.Context, dir, id string, restore func(string) error) error {
	return withBackupLock(dir, id, func(backup string) error {
		record, err := readUpgradeRecord(dir)
		if err != nil {
			return err
		}
		if record != nil && record.Phase == phaseAdmitted {
			return ErrRollbackAfterAdmission
		}
		if record != nil && record.ID != id {
			return errors.New("this backup belongs to another upgrade")
		}
		if record != nil && record.Failure != "" {
			return UpgradeRecoveryRequired
		}
		if err := platform.CheckPrivateDir(backup); err != nil {
			return err
		}
		if err := withVerifiedCopy(ctx, backup, restore); err != nil {
			return err
		}
		return retireBackup(backup)
	})
}

func withVerifiedCopy(ctx context.Context, backup string, use func(string) error) (err error) {
	work := filepath.Join(backup, ".restore")
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(work); err != nil {
		return err
	}
	if err = prepareRollback(ctx, backup, work); err == nil {
		err = use(work)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(work))
	}
	return nil
}

func prepareRollback(ctx context.Context, backup, work string) error {
	sums, err := copyBackupData(ctx, backup, work)
	if err != nil {
		return err
	}
	f, err := platform.OpenPrivateFile(filepath.Join(backup, "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer f.Close()
	recorded, err := io.ReadAll(io.LimitReader(f, int64(len(sums)+1)))
	if err != nil {
		return err
	}
	if string(recorded) != sums {
		return errors.New("upgrade backup checksum mismatch")
	}
	return ctx.Err()
}

// CleanupCompletedBackup is also safe to retry while the enabled service is
// running. Call after a successful upgrade or a durably completed rollback.
func CleanupCompletedBackup(dir, id string) error {
	return withBackupLock(dir, id, func(backup string) error {
		r, err := readUpgradeRecord(dir)
		if err != nil {
			return err
		}
		if r != nil && r.ID == id && r.Phase != phaseAdmitted {
			return UpgradeRecoveryRequired
		}
		return retireBackup(backup)
	})
}

func retireBackup(backup string) error {
	retired := backup + ".retired"
	if err := os.Rename(backup, retired); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("retire upgrade backup: %w", err)
	}
	partial := filepath.Join(filepath.Dir(backup), ".partial-"+filepath.Base(backup))
	return errors.Join(os.RemoveAll(retired), os.RemoveAll(partial))
}

// Cleanup can run after service readiness, so it shares an artifact lock with
// creation and rollback independently of the live service's data lock.
func withBackupLock(dir, id string, run func(string) error) (err error) {
	if !upgradeIDPattern.MatchString(id) {
		return errors.New("invalid upgrade ID")
	}
	if err := platform.CheckPrivateDir(dir); err != nil {
		return err
	}
	root := filepath.Join(dir, "backups")
	lock, err := platform.AcquireServiceLock(root)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return run(filepath.Join(root, id))
}
