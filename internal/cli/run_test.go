package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

func TestCommandHelp(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"", []string{"Authorize this browser", "transaction send", "obtain a Session ID"}},
		{"connect --help", []string{"Required target EVM chain ID", "default: Wallet Bridge CLI", "wait for the connection result"}},
		{"transaction send --help", []string{"Required Session network", "Required recipient", "default: 0)", "default: 0x)", "wallet estimate", "--max-priority-fee-per-gas"}},
		{"transaction get --help", []string{"<request-id>", "request ID returned by wallet-bridge transaction send"}},
		{"session close --help", []string{"<session-id>", "after approving wallet-bridge connect"}},
		{"browser list --help", []string{"default: 50", "first page"}},
		{"service run --help", []string{"--data-dir", "current user's"}},
		{"update check --help", []string{"--include-preview", "stable versions only"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			var out, diag bytes.Buffer
			if code := Run(strings.Fields(tc.command), Dependencies{}, &out, &diag); code != 0 || diag.Len() != 0 {
				t.Fatalf("exit %d, stderr %q", code, &diag)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help lacks %q: %s", want, &out)
				}
			}
			if strings.Contains(out.String(), "--verify-upgrade") {
				t.Error("help advertises the internal upgrade option")
			}
		})
	}
}

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
		code := Run(tc.args, Dependencies{RunService: func(dir, _ string) error {
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
	if code := Run([]string{"service", "run"}, Dependencies{RunService: func(string, string) error { return errors.New("port_in_use: occupied") }}, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
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
