//go:build windows || darwin

package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func rollbackRecord(t *testing.T, dir string) {
	t.Helper()
	err := WithUpgrade(t.Context(), dir, func(w *Upgrade) error {
		return w.save(upgradeRecord{
			ID: testUpgradeID, Phase: phaseVerifying,
			Previous: releaseSelection{"dev", strings.Repeat("1", 64)},
			Target:   releaseSelection{stagedVersion, strings.Repeat("2", 64)},
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestorePreviousData(t *testing.T) {
	ctx := t.Context()
	dir, backup := rollbackFixture(t)
	rollbackRecord(t, dir)
	db := filepath.Join(dir, store.DatabaseName)
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	key, err := credentials.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, credentials.FileName), []byte(strings.Repeat("a", 64)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Fail the second publication after the credential was already replaced.
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(db, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db+"-journal", []byte("candidate journal"), 0600); err != nil {
		t.Fatal(err)
	}
	original := errors.New("candidate failed its readiness check")
	restore := func(cause error) error {
		return WithUpgrade(ctx, dir, func(w *Upgrade) error { return w.RestorePreviousData(ctx, cause) })
	}
	if err := restore(original); err == nil {
		t.Fatal("database replacement unexpectedly succeeded")
	}
	if saved, err := credentials.Read(dir); err != nil || saved != key {
		t.Fatal("first publication did not restore the original credential", err)
	}
	if err := CleanupCompletedBackup(dir, testUpgradeID); !errors.Is(err, UpgradeRecoveryRequired) {
		t.Fatal("incomplete rollback allowed cleanup", err)
	}
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	if err := restore(errors.New("a retry diagnostic")); err != nil {
		t.Fatal(err)
	}
	r, err := readUpgradeRecord(dir)
	if err != nil || r == nil || r.Phase != phaseDataRestored || r.Failure != original.Error() {
		t.Fatal("retry lost rollback decision", r, err)
	}
	if got, err := os.ReadFile(db); err != nil || !bytes.Equal(got, before) {
		t.Fatal("retry restored different database bytes", err)
	}
	if _, err := os.Stat(db + "-journal"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("candidate journal survived recovery", err)
	}
	if _, err := os.Stat(filepath.Join(backup, "SHA256SUMS")); err != nil {
		t.Fatal("data recovery retired the backup before old service readiness", err)
	}
	if block, err := CheckUpgrade(dir); err != nil || block != UpgradeRecoveryRequired {
		t.Fatal("ordinary access opened before old service readiness", block, err)
	}
	if err := restore(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRestorePreviousDataRequiresExclusiveStoreAndVerifiedBackup(t *testing.T) {
	dir, backup := rollbackFixture(t)
	rollbackRecord(t, dir)
	lock, err := platform.AcquireServiceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, store.DatabaseName)
	changed := []byte("candidate data")
	if err := os.WriteFile(db, changed, 0600); err != nil {
		t.Fatal(err)
	}
	err = WithUpgrade(t.Context(), dir, func(w *Upgrade) error {
		wait, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		return w.RestorePreviousData(wait, errors.New("verification failed"))
	})
	if closeErr := lock.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("failed to wait for live service", err)
	}
	if err := os.WriteFile(filepath.Join(backup, "SHA256SUMS"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WithUpgrade(t.Context(), dir, func(w *Upgrade) error { return w.RestorePreviousData(t.Context(), nil) }); err == nil {
		t.Fatal("accepted corrupted backup")
	}
	if got, err := os.ReadFile(db); err != nil || !bytes.Equal(got, changed) {
		t.Fatal("live store changed before validating the snapshot", err)
	}
}
