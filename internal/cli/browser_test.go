package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

func TestUIOpensOrExplicitlyPrintsLink(t *testing.T) {
	const secretURL = api.Origin + "/#authorize=one-use-secret"
	for _, mode := range []string{"open", "print", "open failure", "issue failure", "no page"} {
		t.Run(mode, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			dir, issued, opened := t.TempDir(), 0, 0
			dependencies := Dependencies{
				BuildInfo: buildinfo.Info{UIEmbedded: mode != "no page"},
				BrowserLink: func(path string) (api.BrowserTicketResponse, error) {
					issued++
					if path != dir {
						t.Fatal("wrong data directory")
					}
					if mode == "issue failure" {
						return api.BrowserTicketResponse{}, errors.New(secretURL)
					}
					return api.BrowserTicketResponse{URL: secretURL}, nil
				},
				OpenBrowser: func(url string) error {
					opened++
					if url != secretURL {
						t.Fatal("wrong browser target")
					}
					if mode == "open failure" {
						return errors.New(secretURL)
					}
					return nil
				},
			}
			args := []string{"ui", "--data-dir", dir}
			if mode == "print" {
				args = append(args, "--print-url")
			}
			code := Run(args, dependencies, &stdout, &stderr)
			switch mode {
			case "open":
				if code != 0 || opened != 1 || issued != 1 || stderr.Len() != 0 {
					t.Fatal("default browser was not requested")
				}
			case "print":
				if code != 0 || opened != 0 || issued != 1 || stdout.String() != secretURL+"\n" || stderr.Len() != 0 {
					t.Fatal("explicit link output failed")
				}
			case "no page":
				if code != 1 || issued != 0 || opened != 0 || !strings.Contains(stderr.String(), "ui_unavailable") {
					t.Fatal("no-page build attempted authorization")
				}
			case "issue failure":
				if code != 1 || opened != 0 || stdout.Len() != 0 {
					t.Fatal("issuance failure launched a browser")
				}
			case "open failure":
				if code != 1 || opened != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "browser_open_failed") || !strings.Contains(stderr.String(), "--print-url") {
					t.Fatal("open failure did not explain recovery")
				}
			}
			if mode != "print" && (strings.Contains(stdout.String(), secretURL) || strings.Contains(stderr.String(), secretURL)) {
				t.Fatal("ordinary output disclosed authorization URL")
			}
		})
	}
}

func TestBrowserCommandsRejectArgumentsBeforeSideEffects(t *testing.T) {
	for _, args := range [][]string{
		{"ui", "--data-dir", "relative"}, {"ui", "--unknown=secret"}, {"ui", "extra"}, {"ui", "--json"},
		{"browser", "list", "--limit", "0"}, {"browser", "list", "--limit", "51"}, {"browser", "list", "--after", "bad"},
		{"browser", "list", "--all"}, {"browser", "revoke"}, {"browser", "revoke", "bad"},
		{"browser", "revoke", strings.Repeat("a", 32), "--all"}, {"browser", "revoke", "--all", "extra"},
		{"browser", "revoke", "--all=false"}, {"browser", "revoke", strings.Repeat("a", 32), "extra"},
		{"browser", "revoke", strings.Repeat("A", 32)}, {"browser", "unknown"},
	} {
		var stdout, stderr bytes.Buffer
		dependencies := Dependencies{
			BuildInfo: buildinfo.Info{UIEmbedded: true},
			BrowserLink: func(string) (api.BrowserTicketResponse, error) {
				t.Fatal("invalid command issued a link")
				return api.BrowserTicketResponse{}, nil
			},
			ListBrowsers: func(string, int, string) (api.BrowserGrantListResponse, error) {
				t.Fatal("invalid command listed grants")
				return api.BrowserGrantListResponse{}, nil
			},
			RevokeBrowser: func(string, string, bool) (api.BrowserRevokeResponse, error) {
				t.Fatal("invalid command revoked grants")
				return api.BrowserRevokeResponse{}, nil
			},
		}
		if code := Run(args, dependencies, &stdout, &stderr); code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "secret") {
			t.Fatalf("invalid arguments: %v, code=%d", args, code)
		}
	}
}

func TestBrowserCommandsOutputAndDispatch(t *testing.T) {
	id := strings.Repeat("a", 32)
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	dependencies := Dependencies{
		ListBrowsers: func(path string, limit int, after string) (api.BrowserGrantListResponse, error) {
			if path != dir || limit != 1 || after != id {
				t.Fatal("wrong pagination or directory")
			}
			return api.BrowserGrantListResponse{Grants: []api.BrowserGrantResponse{{ID: id, State: "active"}}, NextAfter: id}, nil
		},
		RevokeBrowser: func(path, got string, all bool) (api.BrowserRevokeResponse, error) {
			if path != dir || (all && got != "") || (!all && got != id) {
				t.Fatal("wrong revoke target")
			}
			return api.BrowserRevokeResponse{State: "revoked"}, nil
		},
	}
	for _, jsonOutput := range []bool{false, true} {
		stdout.Reset()
		stderr.Reset()
		args := []string{"browser", "list", "--limit", "1", "--after", id, "--data-dir", dir}
		if jsonOutput {
			args = append(args, "--json")
		}
		if code := Run(args, dependencies, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatal("list command failed")
		}
		if jsonOutput {
			var result api.BrowserGrantListResponse
			if json.Unmarshal(stdout.Bytes(), &result) != nil || result.NextAfter != id {
				t.Fatal("invalid list JSON")
			}
		} else if !strings.Contains(stdout.String(), "Next page:") || !strings.Contains(stdout.String(), id) {
			t.Fatal("missing list or pagination instructions")
		}
	}
	for _, args := range [][]string{
		{"browser", "revoke", id, "--data-dir", dir, "--json"},
		{"browser", "revoke", "--data-dir", dir, "--json", id},
		{"browser", "revoke", "--all", "--data-dir", dir, "--json"},
	} {
		stdout.Reset()
		stderr.Reset()
		var result api.BrowserRevokeResponse
		if code := Run(args, dependencies, &stdout, &stderr); code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || result.State != "revoked" {
			t.Fatal("invalid revoke output")
		}
	}
	dependencies.ListBrowsers = func(string, int, string) (api.BrowserGrantListResponse, error) {
		return api.BrowserGrantListResponse{}, &client.Error{Code: "service_not_running", Message: "Start service ensure", Cause: errors.New("hidden-secret")}
	}
	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"browser", "list", "--json"}, dependencies, &stdout, &stderr)
	var failure client.Error
	if code != 1 || stdout.Len() != 0 || json.Unmarshal(stderr.Bytes(), &failure) != nil || failure.Code != "service_not_running" || strings.Contains(stderr.String(), "hidden-secret") {
		t.Fatal("error output disclosed internal cause or mixed streams")
	}
}

type failingBrowserWriter struct{}

func (failingBrowserWriter) Write([]byte) (int, error) { return 0, errors.New("hidden output details") }

func TestUIReportsOutputFailure(t *testing.T) {
	var stderr bytes.Buffer
	code := Run([]string{"ui", "--print-url"}, Dependencies{
		BuildInfo: buildinfo.Info{UIEmbedded: true},
		BrowserLink: func(string) (api.BrowserTicketResponse, error) {
			return api.BrowserTicketResponse{URL: "secret-link"}, nil
		},
	}, failingBrowserWriter{}, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "output_error") || strings.Contains(stderr.String(), "secret-link") {
		t.Fatal("output failure exposed URL or reported success")
	}
}
