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
		{"transaction", "cancel"}, {"transaction", "get", strings.ToUpper(id)}, {"transaction", "close", id},
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
		{"transaction", "cancel", id, "--json", "--data-dir", dir},
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

func TestResourceIDDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args                   []string
		argument, reason, hint string
		json                   bool
	}{
		{[]string{"transaction", "get"}, "<request-id>", "required", "wallet-bridge transaction send", false},
		{[]string{"transaction", "cancel", "--json", "private"}, "<request-id>", "invalid format", "wallet-bridge transaction send", true},
		{[]string{"session", "get", "private", "--json"}, "<session-id>", "invalid format", "after approving wallet-bridge connect", true},
		{[]string{"session", "close", "first", "second"}, "<session-id>", "Too many IDs", "after approving wallet-bridge connect", false},
		{[]string{"connection", "get", "first", "second", "--json"}, "<request-id>", "Too many IDs", "wallet-bridge connect", true},
		{[]string{"connection", "cancel", "--json"}, "<request-id>", "required", "wallet-bridge connect", true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, diag bytes.Buffer
			if code := Run(tc.args, Dependencies{}, &out, &diag); code != 2 || out.Len() != 0 {
				t.Fatalf("exit %d, stdout %q", code, &out)
			}
			message := diag.String()
			if tc.json {
				var failure client.Error
				if err := json.Unmarshal(diag.Bytes(), &failure); err != nil || failure.Code != "bad_request" {
					t.Fatalf("expected JSON bad_request, got %q", &diag)
				}
				message = failure.Message
			}
			if strings.Contains(message, "private") {
				t.Fatal("diagnostic echoed the invalid ID")
			}
			for _, want := range []string{tc.argument, tc.reason, tc.hint, "32 lowercase hexadecimal"} {
				if !strings.Contains(message, want) {
					t.Errorf("%q lacks %q", message, want)
				}
			}
		})
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
	if err := writeTransaction(&out, false, api.TransactionResponse{ClientName: "agent\nname", Intent: "call\rcontract"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name\nline"`, `"\x1b[2J"`, `"wallet\rname"`, "Session: session-id", `"agent\nname"`, `"call\rcontract"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output", want)
		}
	}
}
