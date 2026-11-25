package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestUpdateCheckCommand(t *testing.T) {
	var out, diag bytes.Buffer
	deps := Dependencies{
		BuildInfo: buildinfo.Info{Version: "v1.0.0"},
		CheckUpdate: func(_ context.Context, current string, preview bool) (update.Result, error) {
			if current != "v1.0.0" || !preview {
				t.Fatal("unexpected check inputs", current, preview)
			}
			return update.Result{Current: current, Latest: "v1.2.0", State: "update_available"}, nil
		},
	}
	if code := Run([]string{"update", "check", "--include-preview", "--json"}, deps, &out, &diag); code != 0 || diag.Len() != 0 {
		t.Fatal(code, diag.String())
	}
	var got update.Result
	if json.Unmarshal(out.Bytes(), &got) != nil || got.Latest != "v1.2.0" || got.State != "update_available" {
		t.Fatal(out.String())
	}
	out.Reset()
	deps.CheckUpdate = func(context.Context, string, bool) (update.Result, error) {
		return update.Result{}, errors.New("HTTP 403")
	}
	if code := Run([]string{"update", "check", "--json"}, deps, &out, &diag); code != 1 || out.Len() != 0 || !json.Valid(diag.Bytes()) {
		t.Fatal(code, out.String(), diag.String())
	}
	if code := Run([]string{"update", "check", "extra"}, deps, &out, &diag); code != 2 {
		t.Fatal(code)
	}
}

func TestUpdateDownloadCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bridge")
	want := update.Candidate{Version: "v1.2.0-rc.1", Path: filepath.Join(dir, "program"), SHA256: "digest"}
	calls := 0
	deps := Dependencies{StageUpdate: func(_ context.Context, tag, gotDir string) (update.Candidate, error) {
		calls++
		if tag != want.Version || gotDir != dir {
			t.Fatal(tag, gotDir)
		}
		return want, nil
	}}
	for _, args := range [][]string{
		{"update", "download", want.Version, "--data-dir", dir, "--json"},
		{"update", "download", "--data-dir", dir, "--json", want.Version},
	} {
		var out, diag bytes.Buffer
		code := Run(args, deps, &out, &diag)
		var got update.Candidate
		if code != 0 || diag.Len() != 0 || json.Unmarshal(out.Bytes(), &got) != nil || got != want {
			t.Fatal(code, out.String(), diag.String())
		}
	}
	var out, diag bytes.Buffer
	if code := Run([]string{"update", "download", "../invalid"}, deps, &out, &diag); code != 2 || calls != 2 {
		t.Fatal(code, calls)
	}
}
