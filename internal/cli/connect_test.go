package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

// Unexpected reads fail the test: asynchronous submission never polls or accesses a Session.
type retryConnectClient struct {
	connectionClient
	attempts []api.ConnectionCreateRequest
}

func (c *retryConnectClient) CreateConnection(_ context.Context, input api.ConnectionCreateRequest) (api.ConnectionResponse, error) {
	c.attempts = append(c.attempts, input)
	if len(c.attempts) == 1 {
		return api.ConnectionResponse{}, &client.Error{Code: "request_failed", Message: "Response lost"}
	}
	return api.ConnectionResponse{ID: input.ID, ChainID: input.ChainID, State: "pending"}, nil
}

func TestConnectRetryAndOpenOnce(t *testing.T) {
	input := api.ConnectionCreateRequest{ID: client.NewConnectionID(), ClientName: "CLI", ChainID: "0x47"}
	for _, interrupt := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		c := new(retryConnectClient)
		var out, diag bytes.Buffer
		opens := 0
		open := func(url string) error {
			opens++
			if url != api.Origin+"/?connection="+input.ID {
				t.Fatal("unexpected browser target")
			}
			if interrupt {
				if !strings.Contains(diag.String(), input.ID) {
					t.Fatal("request ID was unavailable before waiting")
				}
				cancel()
			}
			return nil
		}
		code := submitConnect(ctx, c, connectOptions{input: input, noWait: !interrupt, asJSON: true}, open, &out, &diag)
		cancel()
		if len(c.attempts) != 2 || c.attempts[0] != input || c.attempts[1] != input || opens != 1 {
			t.Fatal("retry changed inputs or reopened the browser")
		}
		if interrupt {
			if code != 130 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"interrupted"`) {
				t.Fatal("interrupted CLI reported an incorrect outcome")
			}
		} else {
			var receipt connectResult
			if code != 0 || diag.Len() != 0 || json.Unmarshal(out.Bytes(), &receipt) != nil || receipt.ID != input.ID || receipt.Session != nil {
				t.Fatal("invalid asynchronous receipt")
			}
		}
	}
}

func TestConnectValidatesBeforeStartup(t *testing.T) {
	for _, args := range [][]string{
		{"connect"}, {"connect", "--chain-id", "-1"}, {"connect", "--chain-id", "0xz"},
		{"connect", "--chain-id", "1e5"}, {"connect", "--chain-id", "1", "--client-name", "  "},
		{"connect", "--chain-id", "1", "--data-dir", "relative"}, {"connect", "--chain-id", "1", "extra"},
	} {
		var out, diag bytes.Buffer
		if code := Run(args, Dependencies{}, &out, &diag); code != 2 || out.Len() != 0 {
			t.Fatalf("invalid args: %v, exit %d", args, code)
		}
	}
}
