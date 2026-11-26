//go:build integration && (windows || darwin)

package update_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestPackagedServiceStartupAdmission(t *testing.T) {
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
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Stop(shutdown); err != nil {
			t.Error(err)
		}
	})
	id := client.NewRequestID()
	err = update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error {
		if err := w.Prepare(ctx, id, target, func(context.Context, update.WalletCheck) error { return nil }); err != nil {
			return err
		}
		return w.Admit(ctx, c.ProbeCandidate)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Status cannot start another program. The verified candidate itself must open.
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		status, err := c.Status(wait)
		if err == nil && status.Info != nil && status.Info.Version == target.Version && status.Info.Ready && status.Info.VerifyingUpgrade == "" {
			break
		}
		if wait.Err() != nil {
			t.Fatalf("candidate did not open: %v %v", status, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	req, err := c.CreateConnection(ctx, api.ConnectionCreateRequest{ID: client.NewRequestID(), ClientName: "admission test", ChainID: "71"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := update.RollbackFromBackup(ctx, dir, id, func(string) error {
		t.Fatal("restored an old snapshot after accepting a request")
		return nil
	}); !errors.Is(err, update.ErrRollbackAfterAdmission) {
		t.Fatal(err)
	}
	status, err := c.Ensure(ctx)
	if err != nil || status.Info == nil || status.Info.Version != target.Version {
		t.Fatal("restart did not select committed version", status, err)
	}
	got, err := c.GetConnection(ctx, req.ID)
	if err != nil || got.ID != req.ID || got.ExpiresAt != req.ExpiresAt {
		t.Fatal("lost a request accepted by the new version", got, err)
	}
}
