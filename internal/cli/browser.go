package cli

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const uiUsage = "Usage: wallet-bridge ui [--data-dir <absolute path>] [--print-url]"
const listBrowserUsage = "Usage: wallet-bridge browser list [--limit 1..50] [--after <id>] [--data-dir <absolute path>] [--json]"
const revokeBrowserUsage = "Usage: wallet-bridge browser revoke <id>|--all [--data-dir <absolute path>] [--json]"
const browserUsage = listBrowserUsage + "\n" + revokeBrowserUsage

func runUI(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", dataDirHelp)
	printURL := flags.Bool("print-url", false, "Print the one-use authorization URL instead of opening it")
	if code, done := parseFlags(flags, args, uiUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 {
		return unexpectedArguments(flags, stderr)
	}
	if !dependencies.BuildInfo.UIEmbedded {
		return browserError(stderr, false, &client.Error{Code: "ui_unavailable", Message: "This program has no embedded page; use a complete Wallet Bridge build"})
	}
	if dependencies.BrowserLink == nil || (!*printURL && dependencies.OpenBrowser == nil) {
		return browserError(stderr, false, &client.Error{Code: "configuration_error", Message: "Browser authorization is unavailable"})
	}
	link, err := dependencies.BrowserLink(*dir)
	if err != nil {
		return browserError(stderr, false, err)
	}
	if *printURL {
		_, err = fmt.Fprintln(stdout, link.URL)
	} else {
		if err := dependencies.OpenBrowser(link.URL); err != nil {
			return browserError(stderr, false, &client.Error{Code: "browser_open_failed", Message: "Cannot open the default browser; run ui --print-url with the same data directory and paste the new link into your browser"})
		}
		_, err = fmt.Fprintln(stdout, "Browser launch requested. Complete authorization in the browser.")
	}
	if err != nil {
		return browserError(stderr, false, &client.Error{Code: "output_error", Message: "Cannot write browser launch output"})
	}
	return 0
}

func runBrowser(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprintln(stdout, browserUsage)
		return 0
	}
	action := args[0]
	if action != "list" && action != "revoke" {
		return unknownCommand(stderr, "browser", args[1:])
	}
	flags := flag.NewFlagSet("browser "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", dataDirHelp)
	jsonOutput := flags.Bool("json", false, outputJSONHelp)
	limit, after, all := 50, "", false
	usage := listBrowserUsage
	var required []requiredArgument
	if action == "list" {
		flags.IntVar(&limit, "limit", 50, "Maximum authorizations per page, from 1 to 50")
		flags.StringVar(&after, "after", "", "Grant ID cursor from the previous page. By default, start at the first page.")
	} else {
		usage = revokeBrowserUsage
		required = []requiredArgument{{flagName: "all", label: "<id> | --all", description: "Use one grant ID from browser list, or --all to revoke all authorizations and pending links."}}
		flags.BoolVar(&all, "all", false, "Revoke all browser authorizations and pending links. Otherwise supply one grant ID from browser list.")
	}
	remaining := args[1:]
	id := ""
	if action == "revoke" && len(remaining) != 0 && !strings.HasPrefix(remaining[0], "-") {
		id, remaining = remaining[0], remaining[1:]
	}
	if code, done := parseFlags(flags, remaining, usage, stdout, stderr, required...); done {
		return code
	}
	if action == "revoke" && id == "" && flags.NArg() == 1 {
		id = flags.Arg(0)
	} else if flags.NArg() != 0 {
		if action == "revoke" {
			return argumentError(stderr, diagnosticJSON(flags), "Pass one grant ID from browser list, or --all.")
		}
		return unexpectedArguments(flags, stderr)
	}
	switch {
	case limit < 1 || limit > 50:
		return argumentError(stderr, *jsonOutput, "--limit must be an integer from 1 to 50.")
	case after != "" && !validResourceID(after):
		return argumentError(stderr, *jsonOutput, "--after requires the grant ID cursor from the previous browser list page.")
	case action == "revoke" && id == "" && !all:
		return argumentError(stderr, *jsonOutput, "browser revoke requires a grant ID from browser list, or --all.")
	case all && id != "":
		return argumentError(stderr, *jsonOutput, "Choose either one grant ID or --all for browser revoke.")
	case id != "" && !validResourceID(id):
		return argumentError(stderr, *jsonOutput, "Invalid grant ID. Use a 32-character lowercase hexadecimal ID from browser list.")
	}
	if action == "list" {
		if dependencies.ListBrowsers == nil {
			return browserError(stderr, *jsonOutput, nil)
		}
		result, err := dependencies.ListBrowsers(*dir, limit, after)
		if err != nil {
			return browserError(stderr, *jsonOutput, err)
		}
		if *jsonOutput {
			err = json.NewEncoder(stdout).Encode(result)
		} else {
			table := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "ID\tSTATE\tISSUED AT\tEXPIRES AT\tREVOKED AT")
			for _, grant := range result.Grants {
				fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", grant.ID, grant.State, grant.IssuedAt, grant.ExpiresAt, grant.RevokedAt)
			}
			err = table.Flush()
			if err == nil && result.NextAfter != "" {
				_, err = fmt.Fprintf(stdout, "Next page: browser list --after %s (use the same --data-dir)\n", result.NextAfter)
			}
		}
		return browserOutputResult(err, *jsonOutput, stderr)
	}
	if dependencies.RevokeBrowser == nil {
		return browserError(stderr, *jsonOutput, nil)
	}
	result, err := dependencies.RevokeBrowser(*dir, id, all)
	if err != nil {
		return browserError(stderr, *jsonOutput, err)
	}
	if *jsonOutput {
		err = json.NewEncoder(stdout).Encode(result)
	} else {
		_, err = fmt.Fprintln(stdout, "Browser authorization revoked.")
	}
	return browserOutputResult(err, *jsonOutput, stderr)
}

func argumentError(stderr io.Writer, asJSON bool, message string) int {
	commandError(stderr, asJSON, &client.Error{Code: "bad_request", Message: message}, "")
	return 2
}

func validResourceID(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16 && hex.EncodeToString(value) == id
}

func browserOutputResult(err error, jsonOutput bool, stderr io.Writer) int {
	if err != nil {
		return browserError(stderr, jsonOutput, &client.Error{Code: "output_error", Message: "Cannot write browser authorization output"})
	}
	return 0
}

func browserError(stderr io.Writer, jsonOutput bool, err error) int {
	return commandError(stderr, jsonOutput, err, "Browser authorization operation failed")
}

func commandError(stderr io.Writer, jsonOutput bool, err error, fallback string) int {
	var failure *client.Error
	if !errors.As(err, &failure) {
		failure = &client.Error{Code: "request_failed", Message: fallback}
	}
	if jsonOutput {
		json.NewEncoder(stderr).Encode(failure)
	} else {
		fmt.Fprintln(stderr, failure)
	}
	return 1
}
