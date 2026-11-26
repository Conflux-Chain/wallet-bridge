//go:build windows || darwin

package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestUpgradeCoordination(t *testing.T) {
	const env = "WALLET_BRIDGE_TEST_UPGRADE_OWNER"
	previous := releaseSelection{"dev", strings.Repeat("1", 64)}
	target := releaseSelection{stagedVersion, strings.Repeat("2", 64)}
	if dir := os.Getenv(env); dir != "" {
		t.Fatal(WithUpgrade(t.Context(), dir, func(w *Upgrade) error {
			if err := w.begin(testUpgradeID, previous, target); err != nil {
				return err
			}
			if err := w.advance(phaseStopped); err != nil {
				return err
			}
			os.Exit(23)
			return nil
		}))
	}
	dir := filepath.Join(t.TempDir(), "wallet data")
	if block, err := CheckUpgrade(dir); block != "" || err != nil {
		t.Fatal("fresh install", block, err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("observation created the data directory", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUpgradeCoordination$")
	cmd.Env = append(os.Environ(), env+"="+dir)
	var exit *exec.ExitError
	if out, err := cmd.CombinedOutput(); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("writer did not exit after committing a phase: %s %v", out, err)
	}
	if block, err := CheckUpgrade(dir); block != UpgradeRecoveryRequired || err != nil {
		t.Fatal("lost interrupted upgrade", block, err)
	}
	want := upgradeRecord{testUpgradeID, phaseStopped, previous, target}
	err := WithUpgrade(t.Context(), dir, func(w *Upgrade) error {
		if w.current == nil || *w.current != want {
			t.Fatal("lost saved versions or phase", w.current)
		}
		if block, err := CheckUpgrade(dir); err != nil || block != UpgradeInProgress {
			t.Fatal("live writer", block, err)
		}
		if err := WithUpgrade(t.Context(), dir, func(*Upgrade) error {
			t.Fatal("second writer acquired upgrade lock")
			return nil
		}); !errors.Is(err, UpgradeInProgress) {
			t.Fatal(err)
		}
		if err := w.begin(strings.Repeat("a", 32), previous, target); !errors.Is(err, UpgradeRecoveryRequired) {
			t.Fatal("pending upgrade overwritten", err)
		}
		if err := w.advance(phasePreparing); err == nil {
			t.Fatal("phase moved backwards")
		}
		return w.advance(phaseVerifying)
	})
	if err != nil {
		t.Fatal(err)
	}
	want.Phase = phaseVerifying
	if got, err := readUpgradeRecord(dir); err != nil || got == nil || *got != want {
		t.Fatal("resume changed the saved upgrade", got, err)
	}
	path := filepath.Join(dir, upgradeRecordName)
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckUpgrade(dir); err == nil {
		t.Fatal("corrupt record permitted ordinary startup")
	}
	if err := WithUpgrade(t.Context(), dir, func(*Upgrade) error {
		t.Fatal("corrupt record accepted as resumable")
		return nil
	}); err == nil {
		t.Fatal("missing record error")
	}
}

func TestUpgradeCoordinationConcurrentObservers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	err := WithUpgrade(t.Context(), dir, func(*Upgrade) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reader, err := platform.ObserveServiceLock(filepath.Join(dir, upgradeLockDir))
	if err != nil || reader == nil {
		t.Fatal("missing observer", err)
	}
	defer reader.Close()
	if block, err := CheckUpgrade(dir); block != "" || err != nil {
		t.Fatal("readers blocked each other", block, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err = WithUpgrade(ctx, dir, func(*Upgrade) error {
		t.Fatal("writer entered before shared reader left")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("reader reported as an active upgrade", err)
	}
}
