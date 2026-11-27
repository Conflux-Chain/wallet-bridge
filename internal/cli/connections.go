package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const connectionUsage = "Usage: wallet-bridge connection get|cancel <request-id> [--data-dir <absolute path>] [--json]"
const sessionUsage = "Usage: wallet-bridge session get|close <session-id> [--data-dir <absolute path>] [--json]"
const transactionUsage = "Usage: wallet-bridge transaction get|cancel <request-id> [--data-dir <absolute path>] [--json]\n" + sendUsage

func runResource(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	group := args[0]
	usage, change := connectionUsage, "cancel"
	switch group {
	case "session":
		usage, change = sessionUsage, "close"
	case "transaction":
		usage = transactionUsage
	}
	if len(args) == 1 || (len(args) == 2 && (args[1] == "--help" || args[1] == "-h")) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	action := args[1]
	if action != "get" && action != change {
		return unknownCommand(stderr, group, args[2:])
	}
	flags := flag.NewFlagSet(group+" "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", dataDirHelp)
	asJSON := flags.Bool("json", false, outputJSONHelp)
	// Accept the ID before or after the options, as with browser revoke.
	remaining := args[2:]
	var ids []string
	for len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
		ids = append(ids, remaining[0])
		remaining = remaining[1:]
	}
	idArgument := "<request-id>"
	idHelp := "Use the request ID returned by wallet-bridge connect."
	if group == "session" {
		idArgument = "<session-id>"
		idHelp = "Use the Session ID returned after approving wallet-bridge connect."
	} else if group == "transaction" {
		idHelp = "Use the request ID returned by wallet-bridge transaction send."
	}
	actionUsage := "Usage: wallet-bridge " + flags.Name() + " " + idArgument + " [--data-dir <absolute path>] [--json]"
	if code, done := parseFlags(flags, remaining, actionUsage, stdout, stderr, requiredArgument{label: idArgument, description: idHelp}); done {
		return code
	}
	ids = append(ids, flags.Args()...)
	hint := " Expected 32 lowercase hexadecimal characters. " + idHelp
	switch {
	case len(ids) == 0:
		return argumentError(stderr, *asJSON, idArgument+" is required."+hint)
	case len(ids) > 1:
		return argumentError(stderr, *asJSON, "Too many IDs. Pass exactly one "+idArgument+"."+hint)
	case !validResourceID(ids[0]):
		return argumentError(stderr, *asJSON, idArgument+" has an invalid format."+hint)
	}
	if deps.NewClient == nil {
		return commandError(stderr, *asJSON, nil, "Service client is unavailable")
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
	switch group {
	case "connection":
		operation := c.GetConnection
		if action == "cancel" {
			operation = c.CancelConnection
		}
		result, err := operation(ctx, id)
		if err != nil {
			return commandError(stderr, *asJSON, err, "Connection request operation failed")
		}
		writeErr = writeConnection(stdout, *asJSON, result)
	case "session":
		operation := c.GetSession
		if action == "close" {
			operation = c.CloseSession
		}
		result, err := operation(ctx, id)
		if err != nil {
			return commandError(stderr, *asJSON, err, "Session operation failed")
		}
		writeErr = writeSession(stdout, *asJSON, result)
	case "transaction":
		operation := c.GetTransaction
		if action == "cancel" {
			operation = c.CancelTransaction
		}
		result, err := operation(ctx, id)
		if err != nil {
			return commandError(stderr, *asJSON, err, "Transaction request operation failed")
		}
		writeErr = writeTransaction(stdout, *asJSON, result)
	}
	if writeErr != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "output_error", Message: "Cannot write the operation result"}, "")
	}
	return 0
}

func writeTransaction(out io.Writer, asJSON bool, result api.TransactionResponse) error {
	if asJSON {
		return json.NewEncoder(out).Encode(result)
	}
	tx := result.Transaction
	_, err := fmt.Fprintf(out, "Transaction %s (%s)\nSession: %s\nClient: %q\nIntent: %q\nChain: %s\nFrom: %s\nTo: %s\nValue (smallest units, hex): %s\n",
		result.ID, result.State, result.SessionID, result.ClientName, result.Intent, tx.ChainID, tx.From, tx.To, tx.Value)
	if err != nil {
		return err
	}
	switch result.State {
	case "pending":
		_, err = fmt.Fprintln(out, "Approval expires at:", result.ExpiresAt)
	case "awaiting_wallet":
		_, err = fmt.Fprintln(out, "The wallet is processing this transaction. Check this request again for its result.")
	case "uncertain":
		_, err = fmt.Fprintln(out, "Broadcast status is uncertain. Check this request again for a late transaction hash.")
	case "sent":
		_, err = fmt.Fprintln(out, "Transaction hash:", result.TxHash)
	}
	return err
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
