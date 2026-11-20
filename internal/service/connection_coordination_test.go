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

func seedCoordinationExecution(t *testing.T, s *store.Store, calling, expired, stale bool) (app.ConnectionRequest, app.ConnectionExecution) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	r := maintenanceRequest(1, now.Add(-3*time.Minute))
	seen := now.Add(-time.Second)
	if stale {
		seen = now.Add(-2 * time.Minute)
	}
	if expired {
		r = maintenanceRequest(1, now.Add(-10*time.Minute))
		seen = r.CreatedAt.Add(time.Second)
	}
	if err := s.CreateConnectionRequest(ctx, r); err != nil {
		t.Fatal(err)
	}
	grant := app.BrowserGrant{ID: fmt.Sprintf("%032x", 100), IssuedAt: now.Add(-time.Hour)}
	grant.ExpiresAt = grant.IssuedAt.Add(app.BrowserGrantLifetime)
	if err := s.CreateBrowserGrant(ctx, grant, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	ref := app.ConnectionExecutionRef{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 1000), BrowserGrantID: grant.ID}
	execution, err := s.ClaimConnectionExecution(ctx, ref, seen)
	if err != nil {
		t.Fatal(err)
	}
	if calling {
		permit, err := s.BeginConnectionCall(ctx, app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", 5000)}, "", seen)
		if err != nil || !permit.Invoke {
			t.Fatal(permit, err)
		}
		execution = permit.Execution
	}
	return r, execution
}

func TestConnectionCoordinationStartup(t *testing.T) {
	ctx := context.Background()
	for _, calling := range []bool{false, true} {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprint(calling, "/", expired), func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "data")
				seed, err := store.Open(ctx, dir)
				if err != nil {
					t.Fatal(err)
				}
				r, before := seedCoordinationExecution(t, seed, calling, expired, false)
				seed.Close()
				startMaintenanceService(t, dir, func() error {
					observer, err := store.Open(ctx, dir)
					if err != nil {
						return err
					}
					defer observer.Close()
					got, err := observer.GetConnectionExecution(ctx, r.ID)
					if err != nil {
						return err
					}
					phase := app.ExecutionInterruptedReady
					if expired {
						phase = app.ExecutionFinished
					}
					if calling {
						phase = app.ExecutionInterruptedCalling
					}
					want := before
					want.Phase = phase
					if !reflect.DeepEqual(got, want) {
						return fmt.Errorf("execution not restored before readiness: %+v", got)
					}
					request, err := observer.GetConnectionRequest(ctx, r.ID)
					if err != nil {
						return err
					}
					if request.SessionID != "" || !request.ExpiresAt.Equal(r.ExpiresAt) {
						return errors.New("restart created a Session or extended the deadline")
					}
					if expired && (request.State != app.ConnectionExpired || request.FinishedAt == nil || !request.FinishedAt.Equal(r.ExpiresAt)) {
						return errors.New("restart lost original expiry")
					}
					return nil
				}, nil)
			})
		}
	}
}

func TestConnectionCoordinationPeriodic(t *testing.T) {
	ctx := context.Background()
	for _, calling := range []bool{false, true} {
		for _, reason := range []string{"healthy", "heartbeat", "revoked", "expired"} {
			t.Run(fmt.Sprint(calling, "/", reason), func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "data")
				ticks := make(chan time.Time)
				_, finished := startMaintenanceService(t, dir, func() error { return nil }, ticks)
				observer, err := store.Open(ctx, dir)
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Close()
				r, before := seedCoordinationExecution(t, observer, calling, reason == "expired", reason == "heartbeat")
				if reason == "revoked" {
					if err := observer.RevokeBrowserGrant(ctx, before.BrowserGrantID, time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				probe := maintenanceRequest(2, time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
				if err := observer.CreateConnectionRequest(ctx, probe); err != nil {
					t.Fatal(err)
				}
				ticks <- time.Now()
				deadline := time.Now().Add(5 * time.Second)
				for {
					got, err := observer.GetConnectionRequest(ctx, probe.ID)
					if err != nil {
						t.Fatal(err)
					}
					if got.State == app.ConnectionExpired {
						break
					}
					select {
					case err := <-finished:
						t.Fatal("service stopped", err)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal("coordination tick not processed")
					}
					time.Sleep(10 * time.Millisecond)
				}
				got, err := observer.GetConnectionExecution(ctx, r.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := before
				if reason != "healthy" {
					want.Phase = app.ExecutionInterruptedReady
					if reason == "expired" {
						want.Phase = app.ExecutionFinished
					}
					if calling {
						want.Phase = app.ExecutionInterruptedCalling
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("periodic maintenance revived ownership or released an unknown wallet wait", got, want)
				}
				request, err := observer.GetConnectionRequest(ctx, r.ID)
				if err != nil || request.SessionID != "" {
					t.Fatal("maintenance created a Session", request, err)
				}
			})
		}
	}
}

func coordinationRaw(t *testing.T, dir string) *sql.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(dir, store.DatabaseName))
	if filepath.VolumeName(dir) != "" {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"rw"}, "_pragma": {"journal_mode(PERSIST)", "foreign_keys(ON)", "busy_timeout(1000)", "temp_store(MEMORY)"}}.Encode()}
	raw, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	return raw
}

func TestConnectionCoordinationFailureRecovery(t *testing.T) {
	ctx := context.Background()
	for _, startup := range []bool{false, true} {
		for _, stage := range []string{"write", "commit"} {
			t.Run(fmt.Sprint(startup, "/", stage), func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "data")
				ticks := make(chan time.Time)
				var address string
				var finished <-chan error
				if !startup {
					address, finished = startMaintenanceService(t, dir, func() error { return nil }, ticks)
				}
				observer, err := store.Open(ctx, dir)
				if err != nil {
					t.Fatal(err)
				}
				defer observer.Close()
				due := maintenanceRequest(2, time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
				if err := observer.CreateConnectionRequest(ctx, due); err != nil {
					t.Fatal(err)
				}
				idle := maintenanceRequest(3, due.CreatedAt)
				if err := observer.CreateConnectionRequest(ctx, idle); err != nil {
					t.Fatal(err)
				}
				completion := app.ConnectionCompletion{SessionID: fmt.Sprintf("%032x", 3000), Wallet: app.WalletDescriptor{Kind: app.WalletLegacy, Name: "Example Wallet"}, Account: "0x0000000000000000000000000000000000001234", ChainID: idle.ChainID}
				if _, err := observer.CompleteConnectionRequest(ctx, idle.ID, completion, idle.CreatedAt.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				session, err := observer.GetSession(ctx, completion.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				r, before := seedCoordinationExecution(t, observer, true, false, true)
				raw := coordinationRaw(t, dir)
				trigger := "CREATE TRIGGER fail_coordinator BEFORE UPDATE ON connection_executions BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
				if stage == "commit" {
					trigger = `CREATE TABLE coordinator_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_coordinator AFTER UPDATE ON connection_executions BEGIN INSERT INTO coordinator_guard VALUES ('missing'); END`
				}
				if _, err := raw.Exec(trigger); err != nil {
					t.Fatal(err)
				}
				var failure error
				if startup {
					failure = run(ctx, Config{DataDir: dir, Version: "test", Ready: func() error { t.Error("failed recovery reported ready"); return nil }}, func(string, string) (net.Listener, error) {
						listener, err := net.Listen("tcp4", "127.0.0.1:0")
						if err == nil {
							address = listener.Addr().String()
						}
						return listener, err
					}, nil)
				} else {
					ticks <- time.Now()
					select {
					case failure = <-finished:
					case <-time.After(7 * time.Second):
						t.Fatal("service did not stop after failed coordination")
					}
				}
				var serviceErr *Error
				if !errors.As(failure, &serviceErr) || serviceErr.Code != "storage_error" {
					t.Fatal(failure)
				}
				got, err := observer.GetConnectionRequest(ctx, due.ID)
				if err != nil || !reflect.DeepEqual(got, due) {
					t.Fatal("failed recovery changed request", got, err)
				}
				gotSession, err := observer.GetSession(ctx, session.ID)
				if err != nil || !reflect.DeepEqual(gotSession, session) {
					t.Fatal("failed recovery changed Session", gotSession, err)
				}
				execution, err := observer.GetConnectionExecution(ctx, r.ID)
				if err != nil || !reflect.DeepEqual(execution, before) {
					t.Fatal("failed recovery changed execution", execution, err)
				}
				lock, err := platform.AcquireServiceLock(dir)
				if err != nil {
					t.Fatal("failed recovery retained lock", err)
				}
				lock.Close()
				listener, err := net.Listen("tcp4", address)
				if err != nil {
					t.Fatal("failed recovery retained listener", err)
				}
				listener.Close()
				if _, err := raw.Exec("DROP TRIGGER fail_coordinator"); err != nil {
					t.Fatal(err)
				}
				startMaintenanceService(t, dir, func() error {
					got, err := observer.GetConnectionRequest(ctx, due.ID)
					if err != nil {
						return err
					}
					if got.State != app.ConnectionExpired {
						return errors.New("restart did not expire original request")
					}
					gotSession, err := observer.GetSession(ctx, session.ID)
					if err != nil {
						return err
					}
					if gotSession.ClosedAt == nil || !gotSession.ClosedAt.Equal(session.LastAccessAt.Add(app.SessionIdleLifetime)) {
						return errors.New("restart changed original Session deadline")
					}
					gotExecution, err := observer.GetConnectionExecution(ctx, r.ID)
					if err != nil {
						return err
					}
					if gotExecution.Phase != app.ExecutionInterruptedCalling || gotExecution.ID != before.ID || gotExecution.CallID != before.CallID {
						return errors.New("restart lost wallet waiting record")
					}
					return nil
				}, nil)
			})
		}
	}
}
