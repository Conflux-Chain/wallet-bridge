package cli

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const uiUsage = "Usage: wallet-bridge ui [--data-dir <absolute path>] [--print-url]"
const browserUsage = "Usage: wallet-bridge browser list [--limit 1..50] [--after <id>] [--data-dir <absolute path>] [--json]\n       wallet-bridge browser revoke <id>|--all [--data-dir <absolute path>] [--json]"

func runUI(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	printURL := flags.Bool("print-url", false, "Print the one-use authorization URL instead of opening it")
	if code, done := parseFlags(flags, args, uiUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, uiUsage)
		return 2
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
		fmt.Fprintln(stderr, browserUsage)
		return 2
	}
	flags := flag.NewFlagSet("browser "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	jsonOutput := flags.Bool("json", false, "Print structured output")
	limit, after, all := 50, "", false
	if action == "list" {
		flags.IntVar(&limit, "limit", 50, "Maximum authorizations per page")
		flags.StringVar(&after, "after", "", "Grant ID cursor from the previous page")
	} else {
		flags.BoolVar(&all, "all", false, "Revoke all browser authorizations and pending links")
	}
	remaining := args[1:]
	id := ""
	if action == "revoke" && len(remaining) != 0 && !strings.HasPrefix(remaining[0], "-") {
		id, remaining = remaining[0], remaining[1:]
	}
	if code, done := parseFlags(flags, remaining, browserUsage, stdout, stderr); done {
		return code
	}
	if action == "revoke" && id == "" && flags.NArg() == 1 {
		id = flags.Arg(0)
	} else if flags.NArg() != 0 {
		fmt.Fprintln(stderr, browserUsage)
		return 2
	}
	if (*dir != "" && !filepath.IsAbs(*dir)) || (action == "list" && (limit < 1 || limit > 50 || (after != "" && !validResourceID(after)))) ||
		(action == "revoke" && ((id == "" && !all) || (id != "" && (all || !validResourceID(id))))) {
		fmt.Fprintln(stderr, browserUsage)
		return 2
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

func parseFlags(flags *flag.FlagSet, args []string, help string, stdout, stderr io.Writer) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, help)
			return 0, true
		}
		// Flag parser errors may contain user-supplied secrets.
		fmt.Fprintln(stderr, help)
		return 2, true
	}
	return 0, false
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
