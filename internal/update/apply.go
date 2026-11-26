package update

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type UpgradeService interface {
	StopPrevious(context.Context, WalletCheck) error
	ProbeCandidate(context.Context, string, string) (bool, error)
	ReadyVersion(context.Context) (string, error)
}

// Apply resumes a recorded switch before consulting the release source. On a
// new switch choose compares the enabled program and stages the selected release.
func Apply(ctx context.Context, dir string, service UpgradeService, choose func(string) (Result, Candidate, error)) (Result, error) {
	var result Result
	var decision upgradeRecord
	var rolledBack *RolledBackError
	err := WithUpgrade(ctx, dir, func(w *Upgrade) error {
		var target Candidate
		var id string
		if w.current == nil {
			current, err := ServiceProgram(dir)
			if err != nil {
				return err
			}
			version := current.Version
			if version == "" {
				version, err = programVersion(ctx, current.Path)
				if err != nil {
					return err
				}
			}
			result, target, err = choose(version)
			if err != nil || target.Version == "" {
				return err
			}
			var nonce [16]byte
			rand.Read(nonce[:])
			id = fmt.Sprintf("%x", nonce)
		} else {
			id = w.current.ID
			target = Candidate{Version: w.current.Target.Version, SHA256: w.current.Target.SHA256}
		}
		if w.current == nil || (!w.current.committed() && w.current.Failure == "") {
			if err := w.Prepare(ctx, id, target, service.StopPrevious); err != nil {
				return err
			}
		}
		if err := w.Admit(ctx, service.ProbeCandidate); err != nil && !errors.As(err, &rolledBack) {
			return err
		}
		decision = *w.current
		return nil
	})
	if err != nil || decision.ID == "" {
		return result, err
	}

	// The candidate can open only after the writer releases its lock. Keep the
	// decision until ordinary service access confirms that exact version.
	version, err := service.ReadyVersion(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("confirm service readiness, retry wallet-bridge upgrade: %w", err)
	}
	if version != decision.selection().Version {
		return Result{}, fmt.Errorf("expected service %s, found %s", decision.selection().Version, version)
	}
	err = WithUpgrade(ctx, dir, func(w *Upgrade) error {
		// A concurrent resume may have already finished this same switch.
		if w.current == nil || w.current.ID != decision.ID {
			return nil
		}
		if *w.current != decision {
			return UpgradeRecoveryRequired
		}
		if err := CleanupCompletedBackup(dir, decision.ID); err != nil {
			return err
		}
		return os.Remove(filepath.Join(dir, upgradeRecordName))
	})
	if rolledBack != nil {
		return Result{}, errors.Join(rolledBack, err)
	}
	return Result{State: "upgraded", Current: decision.Previous.Version, Latest: version}, err
}
