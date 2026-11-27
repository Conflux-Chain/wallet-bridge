package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

type sendStub struct {
	transactionClient // An unexpected poll fails the test.
	attempts          []api.TransactionCreateRequest
	state             string
	loseResponse      bool
}

func (c *sendStub) CreateTransaction(_ context.Context, input api.TransactionCreateRequest) (api.TransactionResponse, error) {
	c.attempts = append(c.attempts, input)
	if c.loseResponse && len(c.attempts) == 1 {
		return api.TransactionResponse{}, &client.Error{Code: "request_failed", Message: "Response lost"}
	}
	return api.TransactionResponse{ID: input.ID, State: c.state}, nil
}

func TestSendRetryAndOpenOnce(t *testing.T) {
	input := api.TransactionCreateRequest{ID: client.NewRequestID(), SessionID: client.NewRequestID(),
		Transaction: map[string]json.RawMessage{"value": json.RawMessage(`"13"`)}}
	for _, interrupt := range []bool{false, true} {
		ctx, stop := context.WithCancel(t.Context())
		c := &sendStub{state: "pending", loseResponse: true}
		var out, diag bytes.Buffer
		url := api.Origin + "/?transaction=" + input.ID
		opens := 0
		open := func(target string) error {
			opens++
			if target != url {
				t.Fatalf("opened %q", target)
			}
			if interrupt {
				if !strings.Contains(diag.String(), input.ID) {
					t.Fatal("missing receipt before interrupt")
				}
				stop()
			}
			return nil
		}
		code := submitTransaction(ctx, c, sendOptions{input: input, noWait: !interrupt, asJSON: true}, open, &out, &diag)
		stop()
		if opens != 1 || !reflect.DeepEqual(c.attempts, []api.TransactionCreateRequest{input, input}) {
			t.Fatal("expected identical create attempts and a single browser open")
		}
		if interrupt {
			if code != 130 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"interrupted"`) {
				t.Fatalf("interrupt: exit %d, %s, %s", code, out.String(), diag.String())
			}
		} else {
			var receipt sendResult
			if code != 0 || diag.Len() != 0 || json.Unmarshal(out.Bytes(), &receipt) != nil || receipt.ID != input.ID || receipt.State != "pending" || receipt.URL != url {
				t.Fatalf("async receipt: exit %d, %s, %s", code, out.String(), diag.String())
			}
		}
	}
}

func TestSendTerminalOutcome(t *testing.T) {
	opts := sendOptions{input: api.TransactionCreateRequest{ID: client.NewRequestID()}, dir: t.TempDir()}
	for state, want := range map[string]int{"sent": 0, "uncertain": 3, "rejected": 1, "expired": 1, "canceled": 1} {
		var out, diag bytes.Buffer
		c := &sendStub{state: state}
		code := submitTransaction(t.Context(), c, opts, nil, &out, &diag)
		if code != want || !strings.Contains(out.String(), opts.input.ID+" ("+state+")") {
			t.Fatalf("%s: exit %d, %s", state, code, out.String())
		}
		if state == "uncertain" && !strings.Contains(out.String(), " transaction get "+opts.input.ID+" --data-dir "+quoteShellArg(opts.dir)) {
			t.Fatalf("missing follow-up command: %s", out.String())
		}
	}
}

func TestSendValidatesFlagsBeforeStartup(t *testing.T) {
	address := "0x" + strings.Repeat("ab", 20)
	base := []string{"transaction", "send", "--session-id", client.NewRequestID(), "--chain-id", "71", "--from", address}
	for _, tc := range []struct {
		fields []string
		want   string
	}{
		{nil, "to is required"},
		{[]string{"--to", address, "--nonce", "0"}, "nonce"},
		{[]string{"--to", address, "--type", "0x2"}, "type"},
		{[]string{"--to", address, "--value="}, "value"},
		{[]string{"--to", address, "--max-fee-per-gas", "2"}, "supplied together"},
		{[]string{"--to", address, "--gas-price", "1", "--max-fee-per-gas", "2", "--max-priority-fee-per-gas", "0"}, "conflicts"},
	} {
		var out, diag bytes.Buffer
		if code := Run(append(base, tc.fields...), Dependencies{}, &out, &diag); code != 2 || !strings.Contains(diag.String(), tc.want) {
			t.Fatalf("%v: exit %d, %s", tc.fields, code, diag.String())
		}
	}
}

func TestSendSessionIDDiagnostic(t *testing.T) {
	const address = "0x69a44e15f5718853e757866d000a98141d49da0d"
	for _, tc := range []struct {
		name  string
		flags []string
		want  string
	}{
		{"omitted", nil, "required"},
		{"empty", []string{"--session-id="}, "required"},
		{"missing value", []string{"--session-id"}, "requires a value"},
		{"short", []string{"--session-id", "aaa"}, "32 lowercase hex"},
		{"uppercase", []string{"--session-id", strings.Repeat("AB", 16)}, "32 lowercase hex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, asJSON := range []bool{false, true} {
				args := []string{"transaction", "send", "--chain-id", "71", "--from", address, "--to", address}
				if asJSON {
					args = append(args, "--json")
				}
				var out, diag bytes.Buffer
				code := Run(append(args, tc.flags...), Dependencies{}, &out, &diag)
				if code != 2 || out.Len() != 0 {
					t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &diag)
				}
				message := diag.String()
				if asJSON {
					var failure client.Error
					if err := json.Unmarshal(diag.Bytes(), &failure); err != nil || failure.Code != "bad_request" {
						t.Fatalf("expected JSON bad_request, got %q", &diag)
					}
					message = failure.Message
				}
				for _, want := range []string{"--session-id", tc.want, "32 lowercase hex", "wallet-bridge connect --chain-id"} {
					if !strings.Contains(message, want) {
						t.Errorf("stderr %q lacks %q", message, want)
					}
				}
			}
		})
	}
}
