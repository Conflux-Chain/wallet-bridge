//go:build integration && (windows || darwin)

package update_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestPackagedServiceStartupCandidate(t *testing.T) {
	ctx := t.Context()
	dir, target := update.PreparationFixture(t)
	c, err := client.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	id := client.NewRequestID()
	err = update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error {
		// The fixture store is already stopped. This test starts the real target.
		if err := w.Prepare(ctx, id, target, func(context.Context, update.WalletCheck) error { return nil }); err != nil {
			return err
		}
		for range 2 {
			if err := w.VerifyCandidate(ctx, c.ProbeCandidate); err != nil {
				return err
			}
		}
		if _, err := c.Ensure(ctx); !errors.Is(err, update.UpgradeInProgress) {
			t.Fatalf("ordinary client reached candidate: %v", err)
		}
		path := "/api/v1/browser/transactions/" + strings.Repeat("2", 32) + "/result"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, api.Origin+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Origin", api.Origin)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("candidate dispatched a wallet result: HTTP %d", response.StatusCode)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		busy, err := platform.ServiceLocked(dir)
		if err != nil || time.Now().After(deadline) {
			t.Fatal("candidate did not release the data lock after owner exit", err)
		}
		if !busy {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if blocked, err := update.CheckUpgrade(dir); err != nil || blocked != update.UpgradeRecoveryRequired {
		t.Fatal("lost interrupted upgrade", blocked, err)
	}
}
