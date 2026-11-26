//go:build integration && (windows || darwin)

package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestPackagedServiceStartupRollbackProgram(t *testing.T) {
	ctx := t.Context()
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := filepath.Abs(filepath.Join("..", "..", "dist", name))
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), name)
	buildTestVersion(t, second, stagedVersion)
	nextProgram, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("direct download can be removed", func(t *testing.T) {
		download := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(download, program, 0700); err != nil {
			t.Fatal(err)
		}
		slot := filepath.Join(t.TempDir(), "rollback-program", name)
		saved, err := retainProgram(ctx, Candidate{Path: download}, slot)
		if err != nil {
			t.Fatal(err)
		}
		first, err := os.Stat(slot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := retainProgram(ctx, Candidate{Path: download}, slot); err != nil {
			t.Fatal(err)
		}
		reused, err := os.Stat(slot)
		if err != nil || !os.SameFile(first, reused) {
			t.Fatal("retry replaced the existing executable", err)
		}
		if err := os.Remove(download); err != nil {
			t.Fatal(err)
		}
		if err := verifyExecutable(saved.Path, saved.SHA256); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(ctx, saved.Path, "version", "--json").CombinedOutput(); err != nil {
			t.Fatalf("saved program failed: %s %v", out, err)
		}
	})
	t.Run("installed baseline and interrupted upgrade", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "data")
		raw, err := exec.CommandContext(ctx, bundle, "install", "--data-dir", dir).Output()
		if err != nil {
			t.Fatal(err)
		}
		installed := strings.TrimSpace(string(raw))
		originalDigest, err := executableDigest(installed)
		if err != nil {
			t.Fatal(err)
		}
		staged, err := stage(ctx, stageSource(t, nextProgram, new(atomic.Int32)), stagedVersion, dir)
		if err != nil {
			t.Fatal(err)
		}
		target := releaseSelection{staged.Version, staged.SHA256}
		var saved Candidate
		err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
			bad := target
			bad.SHA256 = strings.Repeat("0", 64)
			if _, err := w.preparePrograms(ctx, testUpgradeID, bad); err == nil || w.current != nil {
				t.Fatal("invalid target published upgrade intent")
			}
			var err error
			saved, err = w.preparePrograms(ctx, testUpgradeID, target)
			if err == nil && (w.current.Previous.Version != saved.Version || w.current.Previous.SHA256 != originalDigest || saved.SHA256 == target.SHA256) {
				t.Fatal("incorrect previous program", w.current.Previous, saved)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(installed); err != nil {
			t.Fatal(err)
		}
		err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
			if _, err := w.preparePrograms(ctx, strings.Repeat("b", 32), target); !errors.Is(err, UpgradeRecoveryRequired) {
				t.Fatal("another upgrade replaced pending recovery", err)
			}
			got, err := w.preparePrograms(ctx, testUpgradeID, target)
			if err != nil || got != saved {
				t.Fatal("could not recover exact saved program", got, err)
			}
			if err := os.WriteFile(saved.Path, []byte("damaged"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := w.preparePrograms(ctx, testUpgradeID, target); err == nil {
				t.Fatal("resumed with damaged rollback program")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("enabled release label must match executable", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "data")
		staged, err := stage(ctx, stageSource(t, nextProgram, new(atomic.Int32)), stagedVersion, dir)
		if err != nil {
			t.Fatal(err)
		}
		// Point a different release label at the bundle's otherwise valid bytes.
		const label = "v0.0.0-label-mismatch"
		wrong := releaseSelection{label, staged.SHA256}
		targetDir := filepath.Join(dir, "versions", label)
		if err := platform.EnsurePrivateDir(targetDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(staged.Path, filepath.Join(targetDir, name)); err != nil {
			t.Fatal(err)
		}
		if err := publishSelection(dir, wrong); err != nil {
			t.Fatal(err)
		}
		err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
			_, err := w.preparePrograms(ctx, testUpgradeID, releaseSelection{staged.Version, staged.SHA256})
			if err == nil || w.current != nil {
				t.Fatal("accepted mismatched version", err)
			}
			state := releaseSelection{staged.Version, staged.SHA256}
			if err := publishSelection(dir, state); err != nil {
				return err
			}
			saved, err := w.preparePrograms(ctx, testUpgradeID, state)
			if err == nil && (saved.Version != stagedVersion || saved.SHA256 != staged.SHA256) {
				t.Fatal("saved program differs from enabled version", saved)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
