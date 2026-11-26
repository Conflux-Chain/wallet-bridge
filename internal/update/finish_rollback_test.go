//go:build windows || darwin

package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestCommittedRollbackRecovery(t *testing.T) {
	ctx := t.Context()
	dir, backup := rollbackFixture(t)
	rollbackRecord(t, dir)
	installed, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	slot := filepath.Join(dir, "rollback-program")
	if err := platform.EnsurePrivateDir(slot); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(slot, name)
	if err := os.Link(installed, path); err != nil {
		t.Fatal(err)
	}
	digest, err := executableDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("candidate failed")
	// Interrupt cleanup after the outcome has been committed.
	retired := backup + ".retired"
	if err := os.Mkdir(retired, 0700); err != nil {
		t.Fatal(err)
	}
	obstruction := filepath.Join(retired, "occupied")
	if err := os.WriteFile(obstruction, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		r := *w.current
		r.Previous = releaseSelection{"dev", digest}
		if err := w.save(r); err != nil {
			return err
		}
		return w.finishRollback(ctx, cause, func(_ context.Context, id, version string) (bool, error) {
			if id != r.ID || version != "dev" {
				t.Fatal("probed wrong program", id, version)
			}
			return true, nil
		})
	})
	var result *RolledBackError
	var cleanup *os.LinkError
	if !errors.As(err, &result) || !errors.Is(err, cause) || !errors.As(err, &cleanup) {
		t.Fatal("expected rollback report and failed cleanup", err)
	}
	if block, err := CheckUpgrade(dir); err != nil || block != "" {
		t.Fatal("committed rollback blocked ordinary access", block, err)
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(retired); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "active", selectionFile)); err != nil {
		t.Fatal(err)
	}
	if block, err := CheckUpgrade(dir); err != nil || block != UpgradeRecoveryRequired {
		t.Fatal("missing selection allowed ordinary startup", block, err)
	}
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		if err := w.RestorePreviousData(ctx, nil); !errors.Is(err, ErrRollbackAfterAdmission) {
			t.Fatal("rollback allowed restoring stale data", err)
		}
		return w.Admit(ctx, nil)
	})
	if !errors.As(err, &result) || result.Cause.Error() != cause.Error() || errors.As(err, &cleanup) {
		t.Fatal("cleanup retry lost the original result", err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("backup still exists", err)
	}
	if selected, err := ServiceProgram(dir); err != nil || selected.Path != path || selected.SHA256 != digest {
		t.Fatal("failed to repair retained selection", selected, err)
	}
}
