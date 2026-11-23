package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const connectUsage = "Usage: wallet-bridge connect --chain-id <decimal|0xhex> [--client-name <name>] [--intent <text>] [--chain-name <name>] [--no-wait] [--no-open] [--json] [--data-dir <absolute path>]"

type connectOptions struct {
	input                  api.ConnectionCreateRequest
	noWait, noOpen, asJSON bool
}

var chainArgument = regexp.MustCompile(`^([0-9]+|0x[0-9a-fA-F]+)$`)

func runConnect(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	var opts connectOptions
	flags := flag.NewFlagSet("connect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	chain := flags.String("chain-id", "", "Target EVM chain ID")
	flags.StringVar(&opts.input.ClientName, "client-name", "Wallet Bridge CLI", "Name displayed to the user")
	flags.StringVar(&opts.input.Intent, "intent", "", "Reason for connecting")
	flags.StringVar(&opts.input.ChainName, "chain-name", "", "Display name for the target chain")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Return the request immediately")
	flags.BoolVar(&opts.noOpen, "no-open", false, "Let the user open the request link")
	flags.BoolVar(&opts.asJSON, "json", false, "Print structured output")
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	if code, done := parseFlags(flags, args, connectUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 || strings.TrimSpace(opts.input.ClientName) == "" || (*dir != "" && !filepath.IsAbs(*dir)) ||
		len(*chain) > api.MaxConnectionBody || !chainArgument.MatchString(*chain) {
		fmt.Fprintln(stderr, connectUsage)
		return 2
	}
	base := 10
	digits, hex := strings.CutPrefix(*chain, "0x")
	if hex {
		base = 16
	}
	number, _ := new(big.Int).SetString(digits, base)
	opts.input.ChainID = "0x" + number.Text(16)
	if !deps.BuildInfo.UIEmbedded {
		return commandError(stderr, opts.asJSON, &client.Error{Code: "ui_unavailable", Message: "Use a Wallet Bridge build with the embedded connection page"}, "")
	}
	if deps.NewClient == nil || (!opts.noOpen && deps.OpenBrowser == nil) {
		return commandError(stderr, opts.asJSON, nil, "Connection client is unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := deps.NewClient(*dir)
	if err == nil {
		var status client.Status
		status, err = c.Ensure(ctx)
		if err == nil && (status.Info == nil || !slices.Contains(status.Info.Capabilities, "browser.connections.decisions")) {
			err = &client.Error{Code: "incompatible_service", Message: "The service does not support browser connection approval"}
		}
	}
	if err != nil {
		return connectError(ctx, stderr, opts.asJSON, "", err)
	}
	opts.input.ID = client.NewConnectionID()
	return submitConnect(ctx, c, opts, deps.OpenBrowser, stdout, stderr)
}

type connectionClient interface {
	CreateConnection(context.Context, api.ConnectionCreateRequest) (api.ConnectionResponse, error)
	GetConnection(context.Context, string) (api.ConnectionResponse, error)
	GetSession(context.Context, string) (api.SessionResponse, error)
}

type connectResult struct {
	api.ConnectionResponse
	URL     string               `json:"url"`
	Session *api.SessionResponse `json:"session,omitempty"`
}

func submitConnect(ctx context.Context, c connectionClient, opts connectOptions, open func(string) error, stdout, stderr io.Writer) int {
	id := opts.input.ID
	request, err := c.CreateConnection(ctx, opts.input)
	var failure *client.Error
	if ctx.Err() == nil && errors.As(err, &failure) && failure.Code == "request_failed" {
		request, err = c.CreateConnection(ctx, opts.input)
	}
	if err != nil {
		return connectError(ctx, stderr, opts.asJSON, id, err)
	}
	result := connectResult{ConnectionResponse: request, URL: api.Origin + "/?connection=" + id}
	if !opts.noWait {
		if err := writeConnectResult(stderr, opts.asJSON, result); err != nil {
			return connectError(ctx, stderr, opts.asJSON, id, err)
		}
	}
	if !opts.noOpen && request.State == "pending" && ctx.Err() == nil {
		if err := open(result.URL); err != nil {
			return connectError(ctx, stderr, opts.asJSON, id, &client.Error{Code: "browser_open_failed", Message: "Open the request URL in the authorized browser"})
		}
	}
	if !opts.noWait {
		result.ConnectionResponse, err = waitConnection(ctx, c, request)
		if err != nil {
			return connectError(ctx, stderr, opts.asJSON, id, err)
		}
		if result.State != "succeeded" {
			return connectError(ctx, stderr, opts.asJSON, id, &client.Error{Code: "connection_" + result.State, Message: "Connection " + result.State})
		}
		session, err := c.GetSession(ctx, result.SessionID)
		if err != nil {
			return connectError(ctx, stderr, opts.asJSON, id, err)
		}
		if session.ConnectionID != id || session.ChainID != opts.input.ChainID || result.ChainID != opts.input.ChainID {
			return connectError(ctx, stderr, opts.asJSON, id, &client.Error{Code: "request_failed", Message: "The Session does not match the original connection"})
		}
		if session.State != "active" {
			return connectError(ctx, stderr, opts.asJSON, id, &client.Error{Code: "session_closed", Message: "The resulting Session has already closed"})
		}
		result.Session = &session
	}
	if err := writeConnectResult(stdout, opts.asJSON, result); err != nil {
		return connectError(ctx, stderr, opts.asJSON, id, err)
	}
	return 0
}

func waitConnection(ctx context.Context, c connectionClient, request api.ConnectionResponse) (api.ConnectionResponse, error) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for request.State == "pending" {
		select {
		case <-ctx.Done():
			return request, ctx.Err()
		case <-tick.C:
			next, err := c.GetConnection(ctx, request.ID)
			if err != nil {
				return request, err
			}
			request = next
		}
	}
	return request, nil
}

func writeConnectResult(out io.Writer, asJSON bool, result connectResult) error {
	var err error
	switch {
	case asJSON:
		err = json.NewEncoder(out).Encode(result)
	case result.Session != nil:
		err = writeSession(out, false, *result.Session)
	default:
		_, err = fmt.Fprintf(out, "Connection %s (%s)\nOpen: %s\nExpires at: %s\n", result.ID, result.State, result.URL, result.ExpiresAt)
	}
	if err != nil {
		return &client.Error{Code: "output_error", Message: "Cannot write the connection result"}
	}
	return nil
}

func connectError(ctx context.Context, stderr io.Writer, asJSON bool, id string, err error) int {
	code := 1
	var failure *client.Error
	if ctx.Err() != nil {
		code = 130
		failure = &client.Error{Code: "interrupted", Message: "The local connection command was interrupted"}
	} else if !errors.As(err, &failure) {
		failure = &client.Error{Code: "request_failed", Message: "Connection operation failed"}
	}
	url := ""
	if id != "" {
		url = api.Origin + "/?connection=" + id
	}
	if asJSON {
		json.NewEncoder(stderr).Encode(struct {
			*client.Error
			ID  string `json:"id,omitempty"`
			URL string `json:"url,omitempty"`
		}{failure, id, url})
	} else {
		fmt.Fprintln(stderr, failure)
		if id != "" {
			fmt.Fprintf(stderr, "Check: wallet-bridge connection get %s (use the same data directory)\nOpen: %s\n", id, url)
		}
	}
	return code
}
