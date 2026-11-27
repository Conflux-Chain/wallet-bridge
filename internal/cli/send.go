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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const sendUsage = `Usage: wallet-bridge transaction send --session-id <id> --from <address> --chain-id <decimal|0xhex> --to <address> [options]

Amounts and fees accept decimal or 0x hex in smallest units. Omit fees to use the wallet estimate.
The wallet selects the nonce and transaction type.`

const sendSessionIDHelp = "Run wallet-bridge connect --chain-id <chain-id>, approve the connection, then use the Session ID (32 lowercase hexadecimal characters)."

type sendOptions struct {
	input                  api.TransactionCreateRequest
	dir                    string
	noOpen, noWait, asJSON bool
}

func runSend(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	var opts sendOptions
	flags := flag.NewFlagSet("transaction send", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.input.SessionID, "session-id", "", "Required. "+sendSessionIDHelp)
	flags.StringVar(&opts.input.ClientName, "client-name", "Wallet Bridge CLI", "Name displayed to the user")
	flags.StringVar(&opts.input.Intent, "intent", "", "Optional description shown on the transaction review page")
	flags.StringVar(&opts.dir, "data-dir", "", dataDirHelp)
	flags.BoolVar(&opts.noOpen, "no-open", false, "Print the link for manual opening. By default, open the browser.")
	flags.BoolVar(&opts.noWait, "no-wait", false, "Return the request immediately. By default, wait for the wallet result.")
	flags.BoolVar(&opts.asJSON, "json", false, outputJSONHelp)
	fields := make(map[string]string)
	for _, field := range []struct{ option, key, description string }{
		{"chain-id", "chainId", "Required Session network, in decimal or 0x hex"},
		{"from", "from", "0x-prefixed `address` of the Session account"},
		{"to", "to", "Recipient or existing contract `address` (0x-prefixed)"},
		{"value", "value", "Native currency amount in smallest units (default: 0)"},
		{"data", "data", "0x-prefixed calldata in whole bytes (default: 0x)"},
		{"gas", "gas", "Gas limit in gas units. By default, the wallet estimates it."},
		{"gas-price", "gasPrice", "Legacy price in smallest units per gas. Omit when using EIP-1559 fees."},
		{"max-fee-per-gas", "maxFeePerGas", "EIP-1559 fee cap in smallest units per gas. Supply with --max-priority-fee-per-gas."},
		{"max-priority-fee-per-gas", "maxPriorityFeePerGas", "EIP-1559 tip cap in smallest units per gas. Supply with --max-fee-per-gas."},
		{"nonce", "nonce", ""}, {"type", "type", ""}, // Recognize these inputs to report the specific unsupported field.
	} {
		flags.Func(field.option, field.description, func(value string) error { fields[field.key] = value; return nil })
	}
	required := []requiredArgument{{flagName: "session-id"}, {flagName: "from"}, {flagName: "chain-id"}, {flagName: "to"}}
	if code, done := parseFlags(flags, args, sendUsage, stdout, stderr, required...); done {
		return code
	}
	if !validResourceID(opts.input.SessionID) {
		reason := "--session-id has an invalid format. "
		if opts.input.SessionID == "" {
			reason = "--session-id is required. "
		}
		return argumentError(stderr, opts.asJSON, reason+sendSessionIDHelp)
	}
	if flags.NArg() != 0 {
		return unexpectedArguments(flags, stderr)
	}
	// The service checks the actual Session atomically when it creates the request.
	if _, err := app.ParseTransaction(fields); err != nil {
		return argumentError(stderr, opts.asJSON, err.Error())
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
	if err := c.EnsureTransactionApproval(ctx); err != nil {
		return requestCommandError(ctx, stderr, opts.asJSON, "transaction", "", err)
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
