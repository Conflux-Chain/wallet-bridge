//go:build integration && (windows || darwin)

package update_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

const reconnectCandidate = "WALLET_BRIDGE_TEST_RECONNECT_CANDIDATE"

func TestBrowserUpgradeReconnect(t *testing.T) {
	dir, target := update.PreparationFixture(t)
	program, err := update.ServiceProgram(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.Ensure(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if status.Info.Version == target.Version {
		t.Fatal("the fixture needs distinct service versions")
	}
	runner, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", "node_modules/@playwright/test/cli.js", "test",
		"--config", "playwright.upgrade.config.ts", "--project=chromium")
	cmd.Dir = filepath.Join("..", "..", "web")
	cmd.Env = append(os.Environ(), "WALLET_BRIDGE_TEST_DATA="+dir, "WALLET_BRIDGE_TEST_EXECUTABLE="+program.Path,
		"WALLET_BRIDGE_TEST_PREPARE="+runner, reconnectCandidate+"="+string(data))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

// The browser calls this subprocess once it has released wallet occupancy.
// Leave the real switch at its backed-up phase for the packaged upgrade CLI.
func TestPrepareBrowserSwitch(t *testing.T) {
	raw := os.Getenv(reconnectCandidate)
	if raw == "" {
		t.Skip("invoked by TestBrowserUpgradeReconnect")
	}
	var target update.Candidate
	if err := json.Unmarshal([]byte(raw), &target); err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("WALLET_BRIDGE_TEST_DATA")
	c, err := client.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := update.WithUpgrade(t.Context(), dir, func(w *update.Upgrade) error {
		return w.Prepare(t.Context(), client.NewRequestID(), target, c.StopPrevious)
	}); err != nil {
		t.Fatal(err)
	}
}
