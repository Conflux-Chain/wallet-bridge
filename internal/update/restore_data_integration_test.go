//go:build integration && (windows || darwin)

package update_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestPackagedServiceStartupRestoresDataAfterFailure(t *testing.T) {
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
	failure := errors.New("simulated final candidate check failure")
	id := client.NewRequestID()
	err = update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error {
		if err := w.Prepare(ctx, id, target, func(context.Context, update.WalletCheck) error { return nil }); err != nil {
			return err
		}
		return w.Admit(ctx, func(ctx context.Context, id, version string) (bool, error) {
			if version != target.Version {
				return false, errors.New("interrupted before starting previous program")
			}
			ok, err := c.ProbeCandidate(ctx, id, version)
			if ok {
				return false, failure
			}
			return false, err
		})
	})
	if !errors.Is(err, failure) {
		t.Fatal("lost the candidate failure", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "upgrade.json"))
	var record struct{ Phase, Failure string }
	if err != nil || json.Unmarshal(raw, &record) != nil || record.Phase != "data_restored" || record.Failure != failure.Error() {
		t.Fatal("rollback did not persist verified data recovery", record, err)
	}
	if busy, err := platform.ServiceLocked(dir); err != nil || busy {
		t.Fatal("candidate retained the database lock", busy, err)
	}
	// Returning the candidate error alone must not hide a failed restore.
	for _, name := range []string{credentials.FileName, store.DatabaseName} {
		before, err := os.ReadFile(filepath.Join(dir, "backups", id, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(before, got) {
			t.Fatal("live file differs from pre-upgrade backup", name, err)
		}
	}
	if b, err := update.CheckUpgrade(dir); err != nil || b != update.UpgradeRecoveryRequired {
		t.Fatal("ordinary access allowed before old service readiness", b, err)
	}

	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.Stop(shutdown); err != nil {
			t.Error(err)
		}
	})
	resume := func() error {
		return update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error { return w.Admit(ctx, c.ProbeCandidate) })
	}
	var result *update.RolledBackError
	if err := resume(); !errors.As(err, &result) || result.Target != target.Version || result.Restored == target.Version || result.Cause.Error() != failure.Error() {
		t.Fatal("unexpected rollback result", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups", id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("backup remained after rollback", err)
	}
	// Status cannot auto-start a service. Observe the retained program opening.
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		status, err := c.Status(wait)
		if err == nil && status.Info != nil && status.Info.Ready && status.Info.Version == result.Restored {
			break
		}
		if wait.Err() != nil {
			t.Fatal("restored service did not open", status, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	req, err := c.CreateConnection(ctx, api.ConnectionCreateRequest{ID: client.NewRequestID(), ClientName: "rollback test", ChainID: "71"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := resume(); !errors.As(err, &result) {
		t.Fatal("retry lost the rollback outcome", err)
	}
	status, err := c.Ensure(ctx)
	if err != nil || status.Info == nil || status.Info.Version != result.Restored {
		t.Fatal("restart did not select the retained program", status, err)
	}
	got, err := c.GetConnection(ctx, req.ID)
	if err != nil || got.ExpiresAt != req.ExpiresAt || got.ID != req.ID {
		t.Fatal("rollback retry lost a new request", got, err)
	}
}
