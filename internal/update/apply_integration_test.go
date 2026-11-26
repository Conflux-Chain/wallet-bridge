//go:build integration && (windows || darwin)

package update_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/cli"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestPackagedServiceStartupUpgradeCommand(t *testing.T) {
	dir, target := update.PreparationFixture(t)
	c, err := client.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Stop(shutdown); err != nil {
			t.Error(err)
		}
	})
	// Only the unpublished release source is substituted. Both services, the
	// HTTP client, CLI dispatch and local version selection are real.
	deps := cli.Dependencies{NewClient: client.New,
		CheckUpdate: func(_ context.Context, current string, preview bool) (update.Result, error) {
			if !preview {
				t.Fatal("lost explicit preview selection")
			}
			state := "current_unversioned"
			if current == target.Version {
				state = "up_to_date"
			}
			return update.Result{Current: current, Latest: target.Version, State: state}, nil
		},
		StageUpdate: func(_ context.Context, tag, dest string) (update.Candidate, error) {
			if tag != target.Version || dest != dir {
				t.Fatal(tag, dest)
			}
			return target, nil
		},
	}
	args := []string{"upgrade", "--data-dir", dir, "--include-preview", "--json"}
	run := func(want string) {
		t.Helper()
		var out, diag bytes.Buffer
		code := cli.Run(args, deps, &out, &diag)
		var got update.Result
		if code != 0 || diag.Len() != 0 || json.Unmarshal(out.Bytes(), &got) != nil || got.State != want || got.Latest != target.Version {
			t.Fatal(code, out.String(), diag.String())
		}
	}
	run("upgraded")
	run("up_to_date") // The original CLI must compare the now-enabled version.
	if _, err := os.Stat(filepath.Join(dir, "upgrade.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed record still prevents the next upgrade", err)
	}
}

func TestPackagedServiceStartupUpgradeCommandResume(t *testing.T) {
	dir, target := update.PreparationFixture(t)
	ctx := t.Context()
	c, err := client.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Stop(shutdown); err != nil {
			t.Error(err)
		}
	})
	id := client.NewRequestID()
	if err := update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error {
		return w.Prepare(ctx, id, target, c.StopPrevious)
	}); err != nil {
		t.Fatal(err)
	}
	// Lose the staged candidate after shutdown and backup. The packaged CLI
	// must restore the old service offline and return a script-readable failure.
	if err := os.Remove(target.Path); err != nil {
		t.Fatal(err)
	}
	program, err := update.ServiceProgram(dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, program.Path, "upgrade", "--data-dir", dir, "--json")
	var diag bytes.Buffer
	cmd.Stderr = &diag
	out, err := cmd.Output()
	var exit *exec.ExitError
	var report struct{ Code, Target, Restored, Reason string }
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(out) != 0 || json.Unmarshal(diag.Bytes(), &report) != nil ||
		report.Code != "upgrade_rolled_back" || report.Target != target.Version || report.Restored == "" || report.Reason == "" {
		t.Fatal("missing rollback report", err, string(out), diag.String())
	}
	status, err := c.Status(ctx)
	if err != nil || status.Info == nil || status.Info.Version != report.Restored {
		t.Fatal("reported service was not ready", status, err)
	}
	for _, path := range []string{"upgrade.json", filepath.Join("backups", id)} {
		if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("incomplete cleanup", path, err)
		}
	}
}
