//go:build integration && (windows || darwin)

package update_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestPackagedServiceStartupWalletCheckAndBackup(t *testing.T) {
	probe, err := net.Listen("tcp4", api.Address)
	if err != nil {
		t.Fatal("wallet check integration requires free loopback port", err)
	}
	probe.Close()
	ctx := t.Context()
	dir, target := update.PreparationFixture(t)
	c, err := client.New(dir)
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
	ref := seedWalletCall(t, dir)
	id := client.NewRequestID()
	prepare := func() error {
		return update.WithUpgrade(ctx, dir, func(w *update.Upgrade) error {
			return w.Prepare(ctx, id, target, c.StopPrevious)
		})
	}
	err = prepare()
	var busy *client.UpgradeBusyError
	if !errors.As(err, &busy) || busy.Request.ID != ref.ConnectionID {
		t.Fatal("lost active wallet request", err)
	}
	if block, err := update.CheckUpgrade(dir); block != "" || err != nil {
		t.Fatal("wallet refusal blocked ordinary clients", block, err)
	}
	if status, err := c.Status(ctx); err != nil || status.State != "running" {
		t.Fatal("wallet refusal stopped the old service", status, err)
	}
	// Model the original wallet returning, then the user's explicit upgrade retry.
	s, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ReturnConnectionCall(ctx, ref, time.Now())
	if err := errors.Join(err, s.Close()); err != nil {
		t.Fatal(err)
	}
	if err := prepare(); err != nil {
		t.Fatal(err)
	}
	if status, err := c.Status(ctx); err != nil || status.State != "stopped" {
		t.Fatal("upgrade did not stop service", status, err)
	}
	key, err := credentials.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved, err := credentials.Read(filepath.Join(dir, "backups", id)); err != nil || saved != key {
		t.Fatal("backup lost the original service credential", err)
	}
}

func seedWalletCall(t *testing.T, dir string) app.ConnectionCallRef {
	t.Helper()
	ctx := t.Context()
	s, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	request := app.ConnectionRequest{ID: client.NewRequestID(), ClientName: "CLI", ChainID: "0x1", State: app.ConnectionPending, CreatedAt: now, ExpiresAt: now.Add(app.ConnectionRequestLifetime)}
	if err := s.CreateConnectionRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	g := app.BrowserGrant{ID: client.NewRequestID(), IssuedAt: now, ExpiresAt: now.Add(app.BrowserGrantLifetime)}
	if err := s.CreateBrowserGrant(ctx, g, sha256.Sum256([]byte(strings.Repeat("a", 64)))); err != nil {
		t.Fatal(err)
	}
	ref := app.ConnectionCallRef{ConnectionExecutionRef: app.ConnectionExecutionRef{ConnectionID: request.ID, BrowserGrantID: g.ID, ID: client.NewRequestID()}, CallID: client.NewRequestID()}
	if _, err := s.ClaimConnectionExecution(ctx, ref.ConnectionExecutionRef, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginConnectionCall(ctx, ref, "", now); err != nil {
		t.Fatal(err)
	}
	return ref
}
