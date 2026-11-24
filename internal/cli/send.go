package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const sendUsage = "Usage: wallet-bridge transaction send --session-id <id> --from <address> --chain-id <decimal|0xhex> --to <address> [--value <amount>] [--data <0xbytes>] [--gas <limit>] [--gas-price <price> | --max-fee-per-gas <price> --max-priority-fee-per-gas <price>] [--client-name <name>] [--intent <text>] [--no-open] [--no-wait] [--json] [--data-dir <absolute path>]\nAmounts and fees use smallest units. Explicit nonce and type are unsupported."

type sendOptions struct {
	input                  api.TransactionCreateRequest
	dir                    string
	noOpen, noWait, asJSON bool
}

func runSend(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	var opts sendOptions
	flags := flag.NewFlagSet("transaction send", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.input.SessionID, "session-id", "", "Existing Session ID")
	flags.StringVar(&opts.input.ClientName, "client-name", "Wallet Bridge CLI", "Name displayed to the user")
	flags.StringVar(&opts.input.Intent, "intent", "", "Reason for the transaction")
	flags.StringVar(&opts.dir, "data-dir", "", "Absolute service data directory")
	flags.BoolVar(&opts.noOpen, "no-open", false, "Print the URL for manual opening")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Return immediately after creation")
	flags.BoolVar(&opts.asJSON, "json", false, "Write structured output")
	fields := make(map[string]string)
	for option, field := range map[string]string{
		"chain-id": "chainId", "from": "from", "to": "to", "value": "value", "data": "data", "gas": "gas",
		"gas-price": "gasPrice", "max-fee-per-gas": "maxFeePerGas", "max-priority-fee-per-gas": "maxPriorityFeePerGas",
		"nonce": "nonce", "type": "type", // Report the specific unsupported field.
	} {
		flags.Func(option, field, func(value string) error { fields[field] = value; return nil })
	}
	if code, done := parseFlags(flags, args, sendUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 || !validResourceID(opts.input.SessionID) || strings.TrimSpace(opts.input.ClientName) == "" ||
		(opts.dir != "" && !filepath.IsAbs(opts.dir)) {
		fmt.Fprintln(stderr, sendUsage)
		return 2
	}
	// The service checks the actual Session atomically when it creates the request.
	if _, err := app.ParseTransaction(fields); err != nil {
		commandError(stderr, opts.asJSON, &client.Error{Code: "bad_request", Message: err.Error()}, "")
		return 2
	}
	opts.input.Transaction = make(map[string]json.RawMessage, len(fields))
	for field, value := range fields {
		opts.input.Transaction[field], _ = json.Marshal(value)
	}
	if !deps.BuildInfo.UIEmbedded {
		return commandError(stderr, opts.asJSON, &client.Error{Code: "ui_unavailable", Message: "Use a build with the embedded transaction review page"}, "")
	}
	if deps.NewClient == nil || (!opts.noOpen && deps.OpenBrowser == nil) {
		return commandError(stderr, opts.asJSON, nil, "Transaction client is unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := deps.NewClient(opts.dir)
	if err != nil {
		return requestCommandError(ctx, stderr, opts.asJSON, "transaction", "", err)
	}
	status, err := c.Ensure(ctx)
	if err != nil {
		return requestCommandError(ctx, stderr, opts.asJSON, "transaction", "", err)
	}
	for _, capability := range []string{"browser.transactions.read", "browser.transactions.coordination", "browser.transactions.decisions"} {
		if status.Info == nil || !slices.Contains(status.Info.Capabilities, capability) {
			return commandError(stderr, opts.asJSON, &client.Error{Code: "incompatible_service", Message: "The service does not support browser transaction approval"}, "")
		}
	}
	opts.input.ID = client.NewRequestID()
	return submitTransaction(ctx, c, opts, deps.OpenBrowser, stdout, stderr)
}

type transactionClient interface {
	CreateTransaction(context.Context, api.TransactionCreateRequest) (api.TransactionResponse, error)
	GetTransaction(context.Context, string) (api.TransactionResponse, error)
}

type sendResult struct {
	api.TransactionResponse
	URL          string `json:"url"`
	CheckCommand string `json:"checkCommand,omitempty"`
}

func submitTransaction(ctx context.Context, c transactionClient, opts sendOptions, open func(string) error, stdout, stderr io.Writer) int {
	id := opts.input.ID
	fail := func(err error) int { return requestCommandError(ctx, stderr, opts.asJSON, "transaction", id, err) }
	request, err := c.CreateTransaction(ctx, opts.input)
	var failure *client.Error
	if ctx.Err() == nil && errors.As(err, &failure) && failure.Code == "request_failed" {
		request, err = c.CreateTransaction(ctx, opts.input)
	}
	if err != nil {
		return fail(err)
	}
	result := sendResult{TransactionResponse: request, URL: api.Origin + "/?transaction=" + id}
	if !opts.noWait {
		if err := writeSendReceipt(stderr, opts.asJSON, result); err != nil {
			return fail(&client.Error{Code: "output_error", Message: "Cannot write the transaction request ID"})
		}
	}
	if !opts.noOpen && result.State == "pending" && ctx.Err() == nil {
		if err := open(result.URL); err != nil {
			return fail(&client.Error{Code: "browser_open_failed", Message: "Open the request URL in the authorized browser"})
		}
	}
	code := 0
	if !opts.noWait {
		result.TransactionResponse, err = waitTransaction(ctx, c, request)
		if err != nil {
			return fail(err)
		}
		switch result.State {
		case "sent":
		case "uncertain":
			code = 3
			result.CheckCommand = transactionCheckCommand(id, opts.dir)
		default:
			code = 1
		}
	}
	if opts.asJSON {
		err = json.NewEncoder(stdout).Encode(result)
	} else {
		err = writeTransaction(stdout, false, result.TransactionResponse)
		if err == nil && opts.noWait {
			_, err = fmt.Fprintln(stdout, "Open:", result.URL)
		}
		if err == nil && result.CheckCommand != "" {
			_, err = fmt.Fprintln(stdout, "Check:", result.CheckCommand)
		}
	}
	if err != nil {
		return fail(&client.Error{Code: "output_error", Message: "Cannot write the transaction result"})
	}
	return code
}

func waitTransaction(ctx context.Context, c transactionClient, request api.TransactionResponse) (api.TransactionResponse, error) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for request.State == "pending" || request.State == "awaiting_wallet" {
		select {
		case <-ctx.Done():
			return request, ctx.Err()
		case <-tick.C:
			next, err := c.GetTransaction(ctx, request.ID)
			if err != nil {
				return request, err
			}
			request = next
		}
	}
	return request, nil
}

func writeSendReceipt(out io.Writer, asJSON bool, result sendResult) error {
	if asJSON {
		return json.NewEncoder(out).Encode(struct {
			ID        string `json:"id"`
			State     string `json:"state"`
			URL       string `json:"url"`
			ExpiresAt string `json:"expiresAt"`
		}{result.ID, result.State, result.URL, result.ExpiresAt})
	}
	_, err := fmt.Fprintf(out, "Transaction %s (%s)\nOpen: %s\n", result.ID, result.State, result.URL)
	return err
}

func transactionCheckCommand(id, dir string) string {
	program := "wallet-bridge"
	if path, err := os.Executable(); err == nil {
		program = quoteShellArg(path)
		if runtime.GOOS == "windows" {
			program = "& " + program
		}
	}
	command := program + " transaction get " + id
	if dir != "" {
		command += " --data-dir " + quoteShellArg(dir)
	}
	return command
}

func quoteShellArg(value string) string {
	if runtime.GOOS == "windows" {
		// PowerShell also recognizes typographical single quotes as delimiters.
		return "'" + strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛").Replace(value) + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
