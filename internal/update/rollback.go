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

// RollbackFromBackup supplies a verified, disposable copy to the version
// switcher. The switcher must allow rollback only before admission and return
// success only after the old service is ready and that outcome is durable.
func RollbackFromBackup(ctx context.Context, dir, id string, restore func(string) error) error {
	return withBackupLock(dir, id, func(backup string) error {
		if err := platform.CheckPrivateDir(backup); err != nil {
			return err
		}
		work := filepath.Join(backup, ".restore")
		if err := os.RemoveAll(work); err != nil {
			return err
		}
		if err := platform.EnsurePrivateDir(work); err != nil {
			return err
		}
		if err := prepareRollback(ctx, backup, work); err != nil {
			return errors.Join(err, os.RemoveAll(work))
		}
		if err := restore(work); err != nil {
			return errors.Join(err, os.RemoveAll(work))
		}
		return retireBackup(backup)
	})
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
	return withBackupLock(dir, id, retireBackup)
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
