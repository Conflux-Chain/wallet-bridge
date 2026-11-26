package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

// RestorePreviousData keeps ordinary startup blocked until the old program
// has been verified against the restored database. A retry uses the same snapshot.
func (w *Upgrade) RestorePreviousData(ctx context.Context, cause error) (err error) {
	// A record rename may have succeeded even if the subsequent directory
	// sync failed. Read the published decision before choosing rollback.
	record, err := readUpgradeRecord(w.dir)
	if err != nil {
		return err
	}
	if record == nil || w.current == nil || record.ID != w.current.ID {
		return errors.New("no matching upgrade to restore")
	}
	w.current = record
	switch record.Phase {
	case phaseAdmitted:
		return ErrRollbackAfterAdmission
	case phaseDataRestored:
		return nil
	case phaseVerifying:
		if cause == nil {
			return errors.New("missing upgrade failure")
		}
		next := *record
		next.Phase, next.Failure = phaseRestoring, failureMessage(cause)
		if err := w.save(next); err != nil {
			return err
		}
	case phaseRestoring:
		// Keep the original failure when resuming after an interruption.
	default:
		return errors.New("rollback requires a pre-admission upgrade backup")
	}

	lock, err := waitForStoppedStore(ctx, w.dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return withBackupLock(w.dir, record.ID, func(backup string) error {
		if err := platform.CheckPrivateDir(backup); err != nil {
			return err
		}
		return withVerifiedCopy(ctx, backup, func(work string) error {
			if err := publishRestoredData(ctx, work, w.dir); err != nil {
				return err
			}
			if err := os.RemoveAll(work); err != nil {
				return err
			}
			return w.advance(phaseDataRestored)
		})
	})
}

func waitForStoppedStore(ctx context.Context, dir string) (*platform.ServiceLock, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := platform.AcquireServiceLock(dir)
		if !errors.Is(err, platform.ErrLocked) {
			return lock, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func publishRestoredData(ctx context.Context, work, dir string) error {
	// A journal left by a failed migration belongs to the candidate database.
	// Drop it only after the complete replacement snapshot has passed validation.
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(dir, store.DatabaseName+suffix)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for _, name := range []string{credentials.FileName, store.DatabaseName} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := platform.CommitRecord(filepath.Join(work, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

func failureMessage(cause error) string {
	msg := strings.ToValidUTF8(strings.TrimSpace(cause.Error()), "�")
	if msg == "" {
		return "candidate verification failed"
	}
	const limit = 1024
	if len(msg) <= limit {
		return msg
	}
	end := limit
	for !utf8.RuneStart(msg[end]) {
		end--
	}
	return msg[:end] + "…"
}
