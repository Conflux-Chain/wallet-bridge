package update

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

var upgradeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// CreateBackup saves one complete pre-upgrade snapshot after the service stops.
// An existing snapshot is left for recovery to inspect, even on a retry.
func CreateBackup(ctx context.Context, dir, upgradeID string) (err error) {
	if !upgradeIDPattern.MatchString(upgradeID) {
		return errors.New("invalid upgrade ID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := platform.AcquireServiceLock(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()

	return withBackupLock(dir, upgradeID, func(final string) error {
		return stageBackup(ctx, dir, final)
	})
}

func stageBackup(ctx context.Context, dir, final string) (err error) {
	for _, path := range []string{final, final + ".retired"} {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("upgrade backup already exists: %w", os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	// A killed writer can leave only this upgrade's unpublished directory.
	staging := filepath.Join(filepath.Dir(final), ".partial-"+filepath.Base(final))
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(staging); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(staging)) }()

	sums, err := copyBackupData(ctx, dir, staging)
	if err != nil {
		return err
	}
	if err := writeBackupFile(filepath.Join(staging, "SHA256SUMS"), sums); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staging, final)
}

func copyBackupData(ctx context.Context, dir, staging string) (string, error) {
	key, err := credentials.Read(dir)
	if err != nil {
		return "", err
	}
	if err := writeBackupFile(filepath.Join(staging, credentials.FileName), key+"\n"); err != nil {
		return "", err
	}
	saved, err := credentials.Read(staging)
	if err != nil {
		return "", err
	}
	if saved != key {
		return "", errors.New("copied service credential differs from source")
	}
	if err := store.Snapshot(ctx, dir, staging); err != nil {
		return "", fmt.Errorf("snapshot database: %w", err)
	}
	db, err := platform.OpenPrivateFile(filepath.Join(staging, store.DatabaseName))
	if err != nil {
		return "", err
	}
	digest, err := fileDigest(db)
	if err := errors.Join(err, db.Close()); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x  %s\n%s  %s\n", sha256.Sum256([]byte(saved+"\n")), credentials.FileName, digest, store.DatabaseName), nil
}

func writeBackupFile(path, content string) error {
	f, err := platform.CreatePrivateFile(path)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}
