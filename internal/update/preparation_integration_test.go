//go:build integration && (windows || darwin)

package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

// Shared with the external-package test that wires in the actual HTTP client.
func PreparationFixture(t *testing.T) (string, Candidate) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join("..", "..", "dist", name)
	if out, err := exec.CommandContext(t.Context(), old, "install", "--data-dir", dir).CombinedOutput(); err != nil {
		t.Fatalf("install old version: %v %s", err, out)
	}
	newer := filepath.Join(t.TempDir(), name)
	buildTestVersion(t, newer, stagedVersion)
	data, err := os.ReadFile(newer)
	if err != nil {
		t.Fatal(err)
	}
	target, err := stage(t.Context(), stageSource(t, data, new(atomic.Int32)), stagedVersion, dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, target
}

func TestPackagedServiceStartupBackupResume(t *testing.T) {
	ctx := t.Context()
	dir, target := PreparationFixture(t)
	if err := credentials.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	unknown := errors.New("stop response lost")
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		return w.Prepare(ctx, testUpgradeID, target, func(context.Context, WalletCheck) error { return unknown })
	})
	if !errors.Is(err, unknown) {
		t.Fatal(err)
	}
	// Interrupt between publishing the complete backup and updating the phase.
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		if w.current.Phase != phasePreparing {
			t.Fatal("lost stop response advanced the phase")
		}
		if err := w.advance(phaseStopped); err != nil {
			return err
		}
		return CreateBackup(ctx, dir, testUpgradeID)
	})
	if err != nil {
		t.Fatal(err)
	}
	resume := func() error {
		return WithUpgrade(ctx, dir, func(w *Upgrade) error {
			return w.Prepare(ctx, testUpgradeID, target, func(context.Context, WalletCheck) error {
				t.Fatal("repeated the wallet stop after confirmed shutdown")
				return nil
			})
		})
	}
	// The retry must verify the saved snapshot independently of the live data.
	if err := os.Remove(filepath.Join(dir, store.DatabaseName)); err != nil {
		t.Fatal(err)
	}
	if err := resume(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backups", testUpgradeID, "SHA256SUMS"), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := resume(); err == nil {
		t.Fatal("accepted damaged pre-migration backup")
	}
}
