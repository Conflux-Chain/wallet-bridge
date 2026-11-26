package update

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

// VerifyCandidate reuses a matching service or starts it with an explicit CLI
// option. Older binaries reject the option before opening the live database.
func (w *Upgrade) VerifyCandidate(ctx context.Context, probe func(context.Context, string, string) (bool, error)) error {
	if w.current == nil || w.current.Phase != phaseVerifying {
		return errors.New("the upgrade is not ready for candidate verification")
	}
	target, err := w.current.Target.candidate(w.dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	id := w.current.ID
	if ok, err := probe(ctx, id, target.Version); err != nil || ok {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	process, err := platform.StartUpgradeCandidate(target.Path, w.dir, id)
	if err != nil {
		return err
	}
	defer process.CloseDiagnostics()
	done := process.Done
	ticks := time.NewTicker(100 * time.Millisecond)
	defer ticks.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-done:
			if !strings.HasPrefix(result.Diagnostic, "already_running:") {
				return errors.New("candidate exited before verification: " + strings.TrimSpace(result.Diagnostic))
			}
			// A still-starting candidate for the same upgrade can be reused.
			done = nil
		case <-ticks.C:
			if ok, err := probe(ctx, id, target.Version); err != nil || ok {
				return err
			}
		}
	}
}

// CheckCandidate runs under the data lock before listening or migrating.
func CheckCandidate(dir, id, version string) error {
	r, err := ownedRecord(dir, id, phaseVerifying)
	if err != nil {
		return err
	}
	if version != r.Target.Version {
		return errors.New("the candidate version differs from the upgrade target")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return verifyExecutable(exe, r.Target.SHA256)
}
