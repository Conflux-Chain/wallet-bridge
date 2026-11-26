//go:build integration && (windows || darwin)

package update

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

var trialVersion = flag.String("trial-version", "", "local release tag to exercise")
var keepTrials = flag.Bool("keep-trials", false, "leave fresh, interrupted upgrades for manual acceptance")

// The operator gets two stopped, independent services. No installed user data
// or live port is needed to prepare them.
func TestReleaseTrials(t *testing.T) {
	if *trialVersion == "" {
		t.Skip("supply -trial-version to use a local release build")
	}
	if !ValidReleaseTag(*trialVersion) {
		t.Fatal("invalid release version")
	}
	dist, err := filepath.Abs("../../dist")
	if err != nil {
		t.Fatal(err)
	}
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dist, "releases", *trialVersion, name)
	if v, err := programVersion(t.Context(), candidate); err != nil || v != *trialVersion {
		t.Fatal("unexpected local release", v, err)
	}
	program, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(t.TempDir(), name)
	const previous = "v0.0.0-trial"
	buildTestVersion(t, old, previous)
	root := t.TempDir()
	if *keepTrials {
		root = filepath.Join(dist, "upgrade-trials-"+*trialVersion)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []string{"resume", "rollback"} {
		dir := filepath.Join(root, scenario)
		if out, err := exec.CommandContext(t.Context(), old, "install", "--data-dir", dir).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		source := localReleaseSource(t, *trialVersion, program, new(atomic.Int32))
		target, err := stage(t.Context(), source, *trialVersion, dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := credentials.Ensure(dir); err != nil {
			t.Fatal(err)
		}
		db, err := store.Open(t.Context(), dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		err = WithUpgrade(t.Context(), dir, func(u *Upgrade) error {
			// This private fixture has never started a service.
			return u.Prepare(t.Context(), testUpgradeID, target, func(context.Context, WalletCheck) error { return nil })
		})
		if err != nil {
			t.Fatal(err)
		}
		if scenario == "rollback" {
			if err := os.Remove(target.Path); err != nil {
				t.Fatal(err)
			}
		}
		if *keepTrials {
			t.Log("ready for manual recovery:", dir)
			continue
		}
		t.Run(scenario, func(t *testing.T) {
			t.Cleanup(func() {
				if out, err := exec.Command(candidate, "service", "stop", "--data-dir", dir).CombinedOutput(); err != nil {
					t.Error(err, string(out))
				}
			})
			cmd := exec.CommandContext(t.Context(), candidate, "upgrade", "--data-dir", dir, "--json")
			out, err := cmd.CombinedOutput()
			var r struct{ State, Latest, Current, Code, Restored, Target string }
			if json.Unmarshal(out, &r) != nil {
				t.Fatal(err, string(out))
			}
			if scenario == "rollback" {
				if cmd.ProcessState.ExitCode() != 1 || r.Code != "upgrade_rolled_back" || r.Restored != previous || r.Target != *trialVersion {
					t.Fatal(err, string(out))
				}
			} else if err != nil || r.State != "upgraded" || r.Current != previous || r.Latest != *trialVersion {
				t.Fatal(err, string(out))
			}
		})
	}
}
