//go:build windows || darwin

package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAdmissionRecovery(t *testing.T) {
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "data")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := stage(ctx, stageSource(t, data, new(atomic.Int32)), stagedVersion, dir)
	if err != nil {
		t.Fatal(err)
	}
	target := releaseSelection{candidate.Version, candidate.SHA256}
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		r := upgradeRecord{testUpgradeID, phaseVerifying, releaseSelection{"dev", strings.Repeat("1", 64)}, target}
		if err := w.save(r); err != nil {
			return err
		}
		return publishSelection(dir, target) // Writer exits before committing admission.
	})
	if err != nil {
		t.Fatal(err)
	}
	if b, err := CheckUpgrade(dir); err != nil || b != UpgradeRecoveryRequired {
		t.Fatal("selection alone allowed normal startup", b, err)
	}
	if open, err := CandidateAdmitted(dir, testUpgradeID); open || err == nil {
		t.Fatal("selection alone opened the candidate", open, err)
	}
	if err := WithUpgrade(ctx, dir, func(w *Upgrade) error {
		if err := w.Admit(ctx, func(_ context.Context, id, version string) (bool, error) {
			return id == testUpgradeID && version == candidate.Version, nil
		}); err != nil {
			return err
		}
		if open, err := CandidateAdmitted(dir, testUpgradeID); err != nil || open {
			t.Fatal("candidate opened before writer released the lock", open, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if open, err := CandidateAdmitted(dir, testUpgradeID); err != nil || !open {
		t.Fatal("committed candidate stayed closed", open, err)
	}
	if err := RollbackFromBackup(ctx, dir, testUpgradeID, func(string) error {
		t.Fatal("attempted to overwrite committed service data")
		return nil
	}); !errors.Is(err, ErrRollbackAfterAdmission) {
		t.Fatal("committed admission allowed rollback", err)
	}
	if err := os.Remove(filepath.Join(dir, "active", selectionFile)); err != nil {
		t.Fatal(err)
	}
	if b, err := CheckUpgrade(dir); b != UpgradeRecoveryRequired || err != nil {
		t.Fatal("missing selection allowed fallback", b, err)
	}
	if err := WithUpgrade(ctx, dir, func(w *Upgrade) error {
		return w.Admit(ctx, nil) // Restore the recorded selection without repeating verification.
	}); err != nil {
		t.Fatal(err)
	}
	if b, err := CheckUpgrade(dir); b != "" || err != nil {
		t.Fatal("repaired selection blocked startup", b, err)
	}
}
