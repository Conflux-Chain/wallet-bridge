package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

func TestResourceArgumentsBeforeServiceStartup(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, args := range [][]string{
		{"connection", "cancel"}, {"connection", "get", id, "extra"}, {"connection", "get", "", id},
		{"connection", "close", id}, {"session", "cancel", id}, {"session", "close", strings.ToUpper(id)},
		{"session", "get", id, "--data-dir", "relative"}, {"session", "get", id, "--unknown=private"},
	} {
		var out, diag bytes.Buffer
		c := Run(args, Dependencies{}, &out, &diag)
		if c != 2 || out.Len() != 0 || strings.Contains(diag.String(), "private") {
			t.Fatalf("invalid arguments: %v, exit %d", args, c)
		}
	}
}

func TestResourceCommandErrors(t *testing.T) {
	id, dir := strings.Repeat("b", 32), t.TempDir()
	deps := Dependencies{NewClient: func(got string) (*client.Client, error) {
		if got != dir {
			t.Fatal("incorrect data directory")
		}
		return nil, &client.Error{Code: "data_unavailable", Message: "Cannot open data", Cause: errors.New("private cause")}
	}}
	for _, args := range [][]string{
		{"connection", "get", id, "--json", "--data-dir", dir},
		{"session", "get", "--json", "--data-dir", dir, id},
	} {
		var out, diag bytes.Buffer
		var failure client.Error
		code := Run(args, deps, &out, &diag)
		if code != 1 || out.Len() != 0 || json.Unmarshal(diag.Bytes(), &failure) != nil ||
			failure.Code != "data_unavailable" || strings.Contains(diag.String(), "private cause") {
			t.Fatal("wrong exit code, error payload or output stream")
		}
	}
}

func TestResourceTextQuotesDisplayNames(t *testing.T) {
	var out bytes.Buffer
	if err := writeConnection(&out, false, api.ConnectionResponse{ClientName: "name\nline", Intent: "\x1b[2J", SessionID: "session-id"}); err != nil {
		t.Fatal(err)
	}
	if err := writeSession(&out, false, api.SessionResponse{Wallet: api.WalletResponse{Name: "wallet\rname"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name\nline"`, `"\x1b[2J"`, `"wallet\rname"`, "Session: session-id"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output", want)
		}
	}
}
