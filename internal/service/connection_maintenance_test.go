//go:build windows || darwin

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func maintenanceRequest(index int, created time.Time) app.ConnectionRequest {
	return app.ConnectionRequest{
		ID: fmt.Sprintf("%032x", index), ClientName: "Example CLI", Intent: "连接用途",
		ChainID: "0x47", State: app.ConnectionPending, CreatedAt: created,
		ExpiresAt: created.Add(app.ConnectionRequestLifetime),
	}
}

func TestConnectionMaintenanceStartupAndTicks(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	seed, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	requests := []app.ConnectionRequest{
		maintenanceRequest(1, now.Add(-10*time.Minute)),
		maintenanceRequest(2, now.Add(-10*time.Minute)),
		maintenanceRequest(3, now.Add(-time.Minute)),
		maintenanceRequest(4, now.Add(-time.Minute)),
	}
	var sessions []app.Session
	for i, r := range requests {
		if err := seed.CreateConnectionRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
		if i%2 == 1 {
			completion := app.ConnectionCompletion{
				SessionID: fmt.Sprintf("%032x", i+100), Wallet: app.WalletDescriptor{Kind: app.WalletLegacy, Name: "Example Wallet"},
				Account: "0x0000000000000000000000000000000000001234", ChainID: r.ChainID,
			}
			if _, err := seed.CompleteConnectionRequest(ctx, r.ID, completion, r.CreatedAt.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			session, err := seed.GetSession(ctx, completion.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			sessions = append(sessions, session)
		}
	}
	seed.Close()
	var observer *store.Store
	ready := func() error {
		var err error
		observer, err = store.Open(ctx, dir)
		if err != nil {
			return err
		}
		// These reads execute inside Ready: recovery must finish before readiness.
		for i, r := range requests {
			got, err := observer.GetConnectionRequest(ctx, r.ID)
			if err != nil || !got.CreatedAt.Equal(r.CreatedAt) || !got.ExpiresAt.Equal(r.ExpiresAt) {
				return fmt.Errorf("recovery changed request times: %v", err)
			}
			if i == 0 && (got.State != app.ConnectionExpired || got.FinishedAt == nil || !got.FinishedAt.Equal(r.ExpiresAt)) {
				return errors.New("overdue connection not expired before readiness")
			}
			if i == 2 && got.State != app.ConnectionPending {
				return errors.New("unexpired connection was not retained")
			}
		}
		for i, before := range sessions {
			got, err := observer.GetSession(ctx, before.ID)
			if err != nil {
				return err
			}
			if i == 0 {
				due := before.LastAccessAt.Add(app.SessionIdleLifetime)
				before.ClosedAt = &due
			}
			if !reflect.DeepEqual(got, before) {
				return errors.New("recovery reset Session time or binding")
			}
		}
		return nil
	}
	ticks := make(chan time.Time)
	_, finished := startMaintenanceService(t, dir, ready, ticks)
	defer observer.Close()
	late := maintenanceRequest(5, now.Add(-20*time.Minute))
	if err := observer.CreateConnectionRequest(ctx, late); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := observer.GetConnectionRequest(ctx, late.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == app.ConnectionExpired {
			if got.FinishedAt == nil || !got.FinishedAt.Equal(late.ExpiresAt) {
				t.Fatal("periodic expiry reset original deadline", got)
			}
			break
		}
		select {
		case err := <-finished:
			t.Fatal("service stopped during maintenance", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("maintenance tick did not expire request")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestConnectionMaintenanceFailureReleasesResources(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	ticks := make(chan time.Time)
	address, finished := startMaintenanceService(t, dir, func() error { return nil }, ticks)
	observer, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	request := maintenanceRequest(1, time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
	if err := observer.CreateConnectionRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(filepath.Join(dir, store.DatabaseName))
	if filepath.VolumeName(dir) != "" {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{
		"mode": {"rw"}, "_pragma": {"journal_mode(PERSIST)", "temp_store(MEMORY)", "busy_timeout(1000)", "foreign_keys(ON)"},
	}.Encode()}
	raw, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`CREATE TRIGGER fail_expiry BEFORE UPDATE ON connection_requests
BEGIN SELECT RAISE(ABORT, 'injected expiry failure'); END`); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	select {
	case err := <-finished:
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "storage_error" {
			t.Fatal("maintenance failure not reported", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("service did not stop after maintenance failure")
	}
	got, err := observer.GetConnectionRequest(ctx, request.ID)
	if err != nil || !reflect.DeepEqual(got, request) {
		t.Fatal("failed maintenance changed durable request", got, err)
	}
	lock, err := platform.AcquireServiceLock(dir)
	if err != nil {
		t.Fatal("maintenance failure retained lock", err)
	}
	lock.Close()
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("maintenance failure retained listener", err)
	}
	listener.Close()
	if _, err := raw.Exec("DROP TRIGGER fail_expiry"); err != nil {
		t.Fatal(err)
	}
	// Restart with the same data retries recovery without extending the deadline.
	startMaintenanceService(t, dir, func() error {
		r, err := observer.GetConnectionRequest(ctx, request.ID)
		if err == nil && (r.State != app.ConnectionExpired || r.FinishedAt == nil || !r.FinishedAt.Equal(request.ExpiresAt)) {
			return errors.New("restart did not recover original expiry")
		}
		return err
	}, nil)
}

func startMaintenanceService(t *testing.T, dir string, checkReady func() error, ticks <-chan time.Time) (string, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	finished := make(chan error, 1)
	var address string
	go func() {
		finished <- run(ctx, Config{DataDir: dir, Version: "test", Ready: func() error {
			if err := checkReady(); err != nil {
				return err
			}
			close(ready)
			return nil
		}}, func(string, string) (net.Listener, error) {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err == nil {
				address = listener.Addr().String()
			}
			return listener, err
		}, ticks)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(7 * time.Second):
			t.Error("service did not stop")
		}
	})
	select {
	case <-ready:
		return address, finished
	case err := <-finished:
		t.Fatal("startup failed", err)
	case <-time.After(7 * time.Second):
		t.Fatal("startup timeout")
	}
	return "", finished
}
