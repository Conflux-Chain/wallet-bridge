package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

func TestServiceCommandParsing(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		code   int
		called bool
	}{
		{[]string{"service", "run", "--data-dir", t.TempDir()}, 0, true},
		{[]string{"service", "run"}, 0, true},
		{[]string{"service", "run", "--help"}, 0, false},
		{[]string{"service", "run", "--data-dir", "relative"}, 2, false},
		{[]string{"service", "run", "--port", "1"}, 2, false},
		{[]string{"service", "run", "extra"}, 2, false},
		{[]string{"service", "unknown"}, 2, false},
	} {
		var stdout, stderr bytes.Buffer
		called := false
		code := Run(tc.args, Dependencies{RunService: func(dir string) error {
			called = true
			if dir != "" && !filepath.IsAbs(dir) {
				t.Fatal("runner received relative data path")
			}
			return nil
		}}, &stdout, &stderr)
		if code != tc.code || called != tc.called {
			t.Fatalf("%v: exit=%d called=%t", tc.args, code, called)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"service", "run"}, Dependencies{RunService: func(string) error { return errors.New("port_in_use: occupied") }}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
		t.Fatal("service failure not reported on stderr")
	}
}

func TestServiceManagementOutput(t *testing.T) {
	for _, action := range []string{"ensure", "status", "stop"} {
		var stdout, stderr bytes.Buffer
		dir := t.TempDir()
		code := Run([]string{"service", action, "--data-dir", dir, "--json"}, Dependencies{
			ManageService: func(got, path string) (client.Status, error) {
				if got != action || path != dir {
					t.Fatal("incorrect management dispatch")
				}
				return client.Status{State: "stopped", Origin: "http://127.0.0.1:18765"}, nil
			},
		}, &stdout, &stderr)
		var result client.Status
		if code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || result.State != "stopped" {
			t.Fatal("invalid JSON success output")
		}
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"service", "ensure", "--json"}, Dependencies{
		ManageService: func(string, string) (client.Status, error) {
			return client.Status{}, &client.Error{Code: "identity_unverified", Message: "Cannot verify", Cause: errors.New("hidden diagnostic")}
		},
	}, &stdout, &stderr)
	var failure client.Error
	if code != 1 || stdout.Len() != 0 || json.Unmarshal(stderr.Bytes(), &failure) != nil || failure.Code != "identity_unverified" || bytes.Contains(stderr.Bytes(), []byte("hidden diagnostic")) {
		t.Fatal("invalid JSON error output")
	}
}
