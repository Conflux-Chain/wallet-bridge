package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const connectionUsage = "Usage: wallet-bridge connection get|cancel <request-id> [--data-dir <absolute path>] [--json]"
const sessionUsage = "Usage: wallet-bridge session get|close <session-id> [--data-dir <absolute path>] [--json]"

func runResource(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	group := args[0]
	usage, change := connectionUsage, "cancel"
	if group == "session" {
		usage, change = sessionUsage, "close"
	}
	if len(args) == 1 || (len(args) == 2 && (args[1] == "--help" || args[1] == "-h")) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	action := args[1]
	if action != "get" && action != change {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	flags := flag.NewFlagSet(group+" "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	asJSON := flags.Bool("json", false, "Print structured output")
	// Accept the ID before or after the options, as with browser revoke.
	remaining := args[2:]
	var ids []string
	if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		ids = []string{remaining[0]}
		remaining = remaining[1:]
	}
	if code, done := parseFlags(flags, remaining, usage, stdout, stderr); done {
		return code
	}
	ids = append(ids, flags.Args()...)
	if len(ids) != 1 || !validResourceID(ids[0]) || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if deps.NewClient == nil {
		return commandError(stderr, *asJSON, nil, "Connection client is unavailable")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := deps.NewClient(*dir)
	if err == nil {
		_, err = c.Ensure(ctx)
	}
	if err != nil {
		return commandError(stderr, *asJSON, err, "Cannot connect to the local service")
	}
	id := ids[0]
	var writeErr error
	if group == "connection" {
		operation := c.GetConnection
		if action == "cancel" {
			operation = c.CancelConnection
		}
		result, err := operation(ctx, id)
		if err != nil {
			return commandError(stderr, *asJSON, err, "Connection request operation failed")
		}
		writeErr = writeConnection(stdout, *asJSON, result)
	} else {
		operation := c.GetSession
		if action == "close" {
			operation = c.CloseSession
		}
		result, err := operation(ctx, id)
		if err != nil {
			return commandError(stderr, *asJSON, err, "Session operation failed")
		}
		writeErr = writeSession(stdout, *asJSON, result)
	}
	if writeErr != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "output_error", Message: "Cannot write the operation result"}, "")
	}
	return 0
}

func writeConnection(out io.Writer, asJSON bool, result api.ConnectionResponse) error {
	if asJSON {
		return json.NewEncoder(out).Encode(result)
	}
	_, err := fmt.Fprintf(out, "Connection %s (%s)\nClient: %q\nIntent: %q\nChain: %s\nExpires at: %s\n",
		result.ID, result.State, result.ClientName, result.Intent, result.ChainID, result.ExpiresAt)
	if err == nil && result.SessionID != "" {
		_, err = fmt.Fprintln(out, "Session:", result.SessionID)
	}
	return err
}

func writeSession(out io.Writer, asJSON bool, result api.SessionResponse) error {
	if asJSON {
		return json.NewEncoder(out).Encode(result)
	}
	_, err := fmt.Fprintf(out, "Session %s (%s)\nConnection: %s\nWallet: %q\nAccount: %s\nChain: %s\nLast access: %s\n",
		result.ID, result.State, result.ConnectionID, result.Wallet.Name, result.Account, result.ChainID, result.LastAccessAt)
	if err == nil && result.ClosedAt != "" {
		_, err = fmt.Fprintln(out, "Closed at:", result.ClosedAt)
	}
	return err
}
