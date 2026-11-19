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
	"syscall"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

func TestLifecycle(t *testing.T) {
	for _, viaHTTP := range []bool{false, true} {
		name := "context cancellation"
		if viaHTTP {
			name = "HTTP stop"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
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
				})
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
			clientToken, err := credentials.Read(dir, credentials.Client)
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
			if request("GET", "/api/v1/status", clientToken) != 200 {
				t.Fatal("status unavailable after readiness")
			}
			if viaHTTP {
				controlToken, err := credentials.Read(dir, credentials.Control)
				if err != nil {
					t.Fatal(err)
				}
				if request("POST", "/api/v1/control/stop", controlToken) != 202 {
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
	for _, stage := range []string{"port", "credentials", "readiness output"} {
		t.Run(stage, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			config := Config{DataDir: dir, Version: "test"}
			want := "port_in_use"
			if stage == "credentials" {
				if err := platform.EnsurePrivateDir(dir); err != nil {
					t.Fatal(err)
				}
				f, err := platform.CreatePrivateFile(filepath.Join(dir, "client.key"))
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("damaged")
				f.Close()
				want = "credential_error"
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
			})
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
