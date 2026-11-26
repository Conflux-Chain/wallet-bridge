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
}
