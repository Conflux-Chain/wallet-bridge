//go:build windows || darwin

package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func rollbackFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := credentials.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CreateBackup(t.Context(), dir, testUpgradeID); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(dir, "backups", testUpgradeID)
}

func TestUpgradeRollbackInterruption(t *testing.T) {
	const env = "WALLET_BRIDGE_TEST_ROLLBACK_DIR"
	if dir := os.Getenv(env); dir != "" {
		t.Fatal(RollbackFromBackup(t.Context(), dir, testUpgradeID, func(string) error {
			os.Exit(23)
			return nil
		}))
	}
	dir, backup := rollbackFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUpgradeRollbackInterruption$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	var exit *exec.ExitError
	if out, err := cmd.CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child did not interrupt verified rollback: %v %s", err, out)
	}
	work := filepath.Join(backup, ".restore")
	if err := writeBackupFile(filepath.Join(work, "stale"), "partial state"); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("old service failed its readiness check")
	err := RollbackFromBackup(ctx, dir, testUpgradeID, func(restored string) error {
		if _, err := os.Stat(filepath.Join(restored, "stale")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old working copy survived retry", err)
		}
		if err := CleanupCompletedBackup(dir, testUpgradeID); !errors.Is(err, platform.ErrLocked) {
			t.Fatal("cleanup raced an active rollback", err)
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatal("lost rollback failure", err)
	}
	if err := RollbackFromBackup(ctx, dir, testUpgradeID, func(string) error { return nil }); err != nil {
		t.Fatal("retry could not complete rollback", err)
	}
}

func TestUpgradeRollbackRejectsTamperedBackup(t *testing.T) {
	for _, name := range []string{credentials.FileName, store.DatabaseName, "SHA256SUMS"} {
		t.Run(name, func(t *testing.T) {
			dir, backup := rollbackFixture(t)
			path := filepath.Join(backup, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case credentials.FileName:
				data = append(bytes.Repeat([]byte("a"), 64), '\n')
			case store.DatabaseName:
				data[31] ^= 1 // change the header's page count
			default:
				data = append(data, '!')
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := RollbackFromBackup(t.Context(), dir, testUpgradeID, func(string) error {
				t.Fatal("tampered backup was handed to the switcher")
				return nil
			}); err == nil {
				t.Fatal("missing recovery error")
			}
			if err := platform.CheckPrivateDir(backup); err != nil {
				t.Fatal("failed verification removed original backup", err)
			}
		})
	}
}

func TestUpgradeRollbackCleanupAfterAdmission(t *testing.T) {
	dir, backup := rollbackFixture(t)
	second := "ad841234000000000000000000000002"
	if err := CreateBackup(t.Context(), dir, second); err != nil {
		t.Fatal(err)
	}
	live, err := platform.AcquireServiceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	// The switcher committed success and renamed the snapshot before exiting.
	if err := os.Rename(backup, backup+".retired"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(backup+".retired", credentials.FileName)); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(dir, "backups", ".partial-"+testUpgradeID)
	if err := platform.EnsurePrivateDir(partial); err != nil {
		t.Fatal(err)
	}
	if err := RollbackFromBackup(t.Context(), dir, testUpgradeID, func(string) error {
		t.Fatal("restored a retired backup")
		return nil
	}); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	for range 2 {
		if err := CleanupCompletedBackup(dir, testUpgradeID); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{backup + ".retired", partial} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("incomplete cleanup", err)
		}
	}
	if _, err := credentials.Read(filepath.Join(dir, "backups", second)); err != nil {
		t.Fatal("cleanup affected another upgrade", err)
	}
}
