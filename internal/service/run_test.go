//go:build windows || darwin

package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func TestLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name            string
		viaHTTP, legacy bool
	}{{"context cancellation", false, false}, {"HTTP stop", true, false}, {"legacy directory", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			legacyToken := strings.Repeat("22", 32)
			var original string
			if tc.legacy {
				if err := credentials.Ensure(dir); err != nil {
					t.Fatal(err)
				}
				original, _ = credentials.Read(dir)
				f, err := platform.CreatePrivateFile(filepath.Join(dir, "control.key"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteString(legacyToken + "\n"); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			ready := make(chan struct{})
			finished := make(chan error, 1)
			var address string
			go func() {
				finished <- run(ctx, Config{DataDir: dir, Version: "test", Ready: func() error { close(ready); return nil }}, func(network, target string) (net.Listener, error) {
					if network != "tcp4" || target != api.Address {
						return nil, errors.New("production listener contract changed")
					}
					listener, err := net.Listen("tcp4", "127.0.0.1:0")
					if err == nil {
						address = listener.Addr().String()
					}
					return listener, err
				}, nil)
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
			case err := <-finished:
				t.Fatal("startup failed:", err)
			case <-time.After(5 * time.Second):
				t.Fatal("readiness timed out")
			}
			if info, err := os.Stat(filepath.Join(dir, store.DatabaseName)); err != nil || info.Size() == 0 {
				t.Fatal("service became ready before database initialization", err)
			}
			token, err := credentials.Read(dir)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{Proxy: nil}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			request := func(method, path, token string) int {
				req, err := http.NewRequest(method, "http://"+address+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Host = api.Address
				req.Header.Set("Authorization", "Bearer "+token)
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, response.Body)
				response.Body.Close()
				return response.StatusCode
			}
			if request("GET", "/api/v1/status", token) != 200 {
				t.Fatal("status unavailable after readiness")
			}
			if request("POST", "/api/v1/control/browser-grants", token) != 201 {
				t.Fatal("shared service credential could not authorize a browser")
			}
			if tc.legacy {
				if token != original {
					t.Fatal("service startup replaced the existing identity")
				}
				if request("GET", "/api/v1/status", legacyToken) != 401 || request("POST", "/api/v1/control/stop", legacyToken) != 401 {
					t.Fatal("service accepted the legacy control credential")
				}
				data, err := os.ReadFile(filepath.Join(dir, "control.key"))
				if err != nil || string(data) != legacyToken+"\n" {
					t.Fatal("service changed the legacy file")
				}
			} else if _, err := os.Stat(filepath.Join(dir, "control.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("fresh service created a control credential")
			}
			if tc.viaHTTP {
				if request("POST", "/api/v1/control/stop", token) != 202 {
					t.Fatal("stop request rejected")
				}
			} else {
				cancel()
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("shutdown timed out")
			}
			lock, err := platform.AcquireServiceLock(dir)
			if err != nil {
				t.Fatal("shutdown retained lock:", err)
			}
			lock.Close()
			listener, err := net.Listen("tcp4", address)
			if err != nil {
				t.Fatal("shutdown retained listener:", err)
			}
			listener.Close()
		})
	}
}

func TestStartupFailureReleasesResources(t *testing.T) {
	for _, stage := range []string{"port", "credentials", "storage", "readiness output"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			config := Config{DataDir: dir, Version: "test"}
			want := "port_in_use"
			if stage == "credentials" || stage == "storage" {
				if err := platform.EnsurePrivateDir(dir); err != nil {
					t.Fatal(err)
				}
				name := "client.key"
				if stage == "storage" {
					name = store.DatabaseName
				}
				f, err := platform.CreatePrivateFile(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("damaged")
				f.Close()
				want = "credential_error"
				if stage == "storage" {
					want = "storage_error"
					config.Ready = func() error { t.Error("reported readiness with corrupt storage"); return nil }
				}
			}
			if stage == "readiness output" {
				config.Ready = func() error { return io.ErrClosedPipe }
				want = "output_error"
			}
			var address string
			err := run(context.Background(), config, func(string, string) (net.Listener, error) {
				if stage == "port" {
					return nil, &net.OpError{Op: "listen", Net: "tcp", Err: syscall.EADDRINUSE}
				}
				listener, err := net.Listen("tcp4", "127.0.0.1:0")
				if err == nil {
					address = listener.Addr().String()
				}
				return listener, err
			}, nil)
			var failure *Error
			if !errors.As(err, &failure) || failure.Code != want {
				t.Fatalf("failure=%v, want %s", err, want)
			}
			lock, err := platform.AcquireServiceLock(dir)
			if err != nil {
				t.Fatal("startup failure retained lock:", err)
			}
			lock.Close()
			if address != "" {
				listener, err := net.Listen("tcp4", address)
				if err != nil {
					t.Fatal("startup failure retained port:", err)
				}
				listener.Close()
			}
			if stage == "port" {
				if _, err := os.Stat(filepath.Join(dir, "client.key")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("credentials created before successful bind")
				}
			}
		})
	}
}
