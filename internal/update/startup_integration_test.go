//go:build integration && (windows || darwin)

package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func buildTestVersion(t *testing.T, path, version string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X github.com/Conflux-Chain/wallet-bridge/internal/buildinfo.Version="+version, "-o", path, "./cmd/wallet-bridge")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build second version: %v: %s", err, out)
	}
}

func TestPackagedServiceStartup(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "dist", name)
	work := t.TempDir()
	dir := filepath.Join(work, "data")
	newer := filepath.Join(work, name)
	buildTestVersion(t, newer, stagedVersion)
	raw, err := exec.CommandContext(t.Context(), old, "install", "--data-dir", dir).Output()
	if err != nil {
		t.Fatal(err)
	}
	fixed := strings.TrimSpace(string(raw))
	program, err := os.ReadFile(newer)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := stage(t.Context(), stageSource(t, program, new(atomic.Int32)), stagedVersion, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishSelection(dir, releaseSelection{Version: selected.Version, SHA256: selected.SHA256}); err != nil {
		t.Fatal(err)
	}

	t.Run("foreground", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, old, "service", "run", "--data-dir", dir).CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || !bytes.Contains(out, []byte(selected.Path)) {
			t.Fatalf("expected diagnostic with enabled path: %v: %s", err, out)
		}
		if _, err := os.Stat(filepath.Join(dir, "bridge.db")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old service reached database initialization", err)
		}
	})

	t.Run("background", func(t *testing.T) {
		listener, err := net.Listen("tcp4", api.Address)
		if err != nil {
			t.Fatalf("test requires free %s: %v", api.Address, err)
		}
		listener.Close()
		control := func(ctx context.Context, action string) string {
			t.Helper()
			out, err := exec.CommandContext(ctx, old, "service", action, "--data-dir", dir, "--json").CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v: %s", action, err, out)
			}
			var status struct{ Info api.InfoResponse }
			if err := json.Unmarshal(out, &status); err != nil {
				t.Fatal(err)
			}
			return status.Info.Version
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			control(ctx, "stop")
		})
		if got := control(t.Context(), "ensure"); got != stagedVersion {
			t.Fatalf("old CLI started %s instead of %s", got, stagedVersion)
		}
		control(t.Context(), "stop")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		agent := mcp.NewClient(&mcp.Implementation{Name: "startup-test", Version: "1"}, nil)
		session, err := agent.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, fixed, "mcp", "--data-dir", dir)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "create_connection", Arguments: map[string]string{
			"chainId": "71", "requestId": strings.Repeat("a", 32),
		}})
		if err != nil || result.IsError {
			t.Fatal("MCP connection request", result, err)
		}
		if got := control(ctx, "status"); got != stagedVersion {
			t.Fatalf("fixed MCP entry started %s instead of %s", got, stagedVersion)
		}
	})
}

func TestPackagedServiceStartupDuringUpgrade(t *testing.T) {
	name, err := nativeAssetName()
	if err != nil {
		t.Fatal(err)
	}
	program, err := filepath.Abs(filepath.Join("..", "..", "dist", name))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "data")
	check := func(want UpgradeBlock) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for _, args := range [][]string{{"connect", "--chain-id", "71", "--no-open"}, {"service", "run"}} {
			out, err := exec.CommandContext(ctx, program, append(args, "--data-dir", dir)...).CombinedOutput()
			var failure *exec.ExitError
			if !errors.As(err, &failure) || failure.ExitCode() != 1 || !bytes.Contains(out, []byte(string(want))) || !bytes.Contains(out, []byte(want.Error())) {
				t.Fatalf("%v: %s %v", args, out, err)
			}
		}
		agent := mcp.NewClient(&mcp.Implementation{Name: "upgrade-test", Version: "1"}, nil)
		s, err := agent.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, program, "mcp", "--data-dir", dir)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "create_connection", Arguments: map[string]string{
			"chainId": "71", "requestId": strings.Repeat("a", 32),
		}})
		if err != nil || !res.IsError || len(res.Content) == 0 {
			t.Fatal("missing MCP tool error", res, err)
		}
		msg, ok := res.Content[0].(*mcp.TextContent)
		if !ok || !strings.Contains(msg.Text, string(want)) || !strings.Contains(msg.Text, want.Error()) {
			t.Fatal("unexpected MCP diagnostic", res.Content)
		}
		if _, err := os.Stat(filepath.Join(dir, "bridge.db")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("ordinary startup opened the database", err)
		}
	}
	err = WithUpgrade(t.Context(), dir, func(w *Upgrade) error {
		if err := w.begin(testUpgradeID, releaseSelection{"dev", strings.Repeat("1", 64)}, releaseSelection{stagedVersion, strings.Repeat("2", 64)}); err != nil {
			return err
		}
		check(UpgradeInProgress)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	check(UpgradeRecoveryRequired)
}
