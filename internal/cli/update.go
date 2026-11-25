package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const updateUsage = "Usage: wallet-bridge update check [--include-preview] [--json]"

func runUpdate(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprintln(stdout, updateUsage)
		return 0
	}
	if args[0] != "check" {
		fmt.Fprintln(stderr, updateUsage)
		return 2
	}
	flags := flag.NewFlagSet("update check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	preview := flags.Bool("include-preview", false, "Include prereleases in the version comparison")
	asJSON := flags.Bool("json", false, "Print structured output")
	if code, done := parseFlags(flags, args[1:], updateUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, updateUsage)
		return 2
	}
	if deps.CheckUpdate == nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "configuration_error", Message: "Release checker is unavailable"}, "")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := deps.CheckUpdate(ctx, deps.BuildInfo.Version, *preview)
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "release_query_failed", Message: err.Error()}, "")
	}
	if *asJSON {
		err = json.NewEncoder(stdout).Encode(result)
	} else {
		switch result.State {
		case "no_release":
			message := "No stable release available."
			if *preview {
				message = "No published release available."
			}
			_, err = fmt.Fprintln(stdout, message)
		case "current_unversioned":
			_, err = fmt.Fprintf(stdout, "Latest release: %s (current build %s has no comparable version).\n", result.Latest, result.Current)
		case "update_available":
			_, err = fmt.Fprintf(stdout, "Update available: %s -> %s\n", result.Current, result.Latest)
		default:
			_, err = fmt.Fprintf(stdout, "No newer release. Current: %s, latest: %s\n", result.Current, result.Latest)
		}
	}
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "output_error", Message: "Cannot write release check result"}, "")
	}
	return 0
}
