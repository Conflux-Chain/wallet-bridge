//go:build windows || darwin

package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestEnsureReportsStartupFailureOrTimeout(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"launch failed", "startup_failed"}, {"credential_error", "credential_error"}, {"storage_error", "storage_error"}, {"unknown diagnostic", "startup_failed"}, {"already_running", "startup_timeout"}, {"waiting", "startup_timeout"}, {"port_in_use", "port_in_use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
			launches := 0
			c.start = func(string) (*platform.BackgroundProcess, error) {
				launches++
				if tc.name == "launch failed" {
					return nil, errors.New("hidden launch details")
				}
				done := make(chan platform.ProcessResult, 1)
				if tc.name != "waiting" {
					done <- platform.ProcessResult{Err: errors.New("exit 1"), Diagnostic: tc.name + ": secret diagnostic"}
					close(done)
				}
				return &platform.BackgroundProcess{Done: done}, nil
			}
			_, err := c.Ensure(context.Background())
			if codeOf(t, err) != tc.code || launches != 1 {
				t.Fatalf("startup result: %v, launches=%d", err, launches)
			}
		})
	}
}

func TestEnsureWaitsForLockHolderWithoutLaunching(t *testing.T) {
	c := newTestClient(t)
	lock, err := platform.AcquireServiceLock(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	c.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	c.start = func(string) (*platform.BackgroundProcess, error) {
		t.Error("started a competing process despite held lock")
		return nil, errors.New("unexpected")
	}
	_, err = c.Ensure(context.Background())
	if codeOf(t, err) != "startup_timeout" {
		t.Fatalf("unexpected wait result: %v", err)
	}
}

func TestStopWaitsForLockAndDoesNotRepeatRequest(t *testing.T) {
	c := newTestClient(t)
	lock, err := platform.AcquireServiceLock(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	clientToken, _ := credentials.Read(c.dir, credentials.Client)
	controlToken, _ := credentials.Read(c.dir, credentials.Control)
	var stops atomic.Int32
	handler, err := api.New(api.Config{Version: "test", ClientToken: clientToken, ControlToken: controlToken, StartedAt: time.Now(), Stop: func() { stops.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	useServer(t, c, handler)
	_, err = c.Stop(context.Background())
	if codeOf(t, err) != "stop_timeout" || stops.Load() != 1 {
		t.Fatalf("unexpected stop result: %v, requests=%d", err, stops.Load())
	}
	if locked, err := platform.ServiceLocked(c.dir); err != nil || !locked {
		t.Fatal("stop timeout disturbed the lock holder")
	}
}

func TestEnsureCancellationKeepsCandidate(t *testing.T) {
	c := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.dial = func(context.Context, string, string) (net.Conn, error) { return nil, syscall.ECONNREFUSED }
	c.start = func(string) (*platform.BackgroundProcess, error) {
		cancel()
		return &platform.BackgroundProcess{Done: make(chan platform.ProcessResult)}, nil
	}
	_, err := c.Ensure(ctx)
	if codeOf(t, err) != "canceled" {
		t.Fatalf("unexpected cancellation result: %v", err)
	}
}

func TestSlowServiceIsBoundedAndReceivesNoBearer(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Bearer sent without a proof")
		}
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Status(ctx)
	if codeOf(t, err) != "canceled" {
		t.Fatalf("unexpected slow probe result: %v", err)
	}
}

func TestInvalidCredentialIsPreserved(t *testing.T) {
	c := newTestClient(t)
	if err := credentials.Ensure(c.dir); err != nil {
		t.Fatal(err)
	}
	path := c.dir + string(os.PathSeparator) + "client.key"
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	useServer(t, c, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid credential reached HTTP") }))
	_, err := c.Status(context.Background())
	if codeOf(t, err) != "credential_error" {
		t.Fatalf("unexpected credential result: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "damaged" {
		t.Fatal("damaged credential was overwritten")
	}
}
