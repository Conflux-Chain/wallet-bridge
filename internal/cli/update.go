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

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

const downloadUsage = "Usage: wallet-bridge update download <version> [--data-dir <absolute path>] [--json]"
const checkUpdateUsage = "Usage: wallet-bridge update check [--include-preview] [--json]"
const updateUsage = checkUpdateUsage + "\n" + downloadUsage

func runUpdate(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprintln(stdout, updateUsage)
		return 0
	}
	if args[0] == "download" {
		return runDownload(args[1:], deps, stdout, stderr)
	}
	if args[0] != "check" {
		fmt.Fprintln(stderr, updateUsage)
		return 2
	}
	flags := flag.NewFlagSet("update check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	preview := flags.Bool("include-preview", false, "Include prereleases. By default, compare stable versions only.")
	asJSON := flags.Bool("json", false, outputJSONHelp)
	if code, done := parseFlags(flags, args[1:], checkUpdateUsage, stdout, stderr); done {
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
	return printUpdateResult(stdout, stderr, *asJSON, *preview, result)
}

func printUpdateResult(stdout, stderr io.Writer, asJSON, preview bool, result update.Result) int {
	var err error
	if asJSON {
		err = json.NewEncoder(stdout).Encode(result)
	} else {
		switch result.State {
		case "no_release":
			message := "No stable release available."
			if preview {
				message = "No published release available."
			}
			_, err = fmt.Fprintln(stdout, message)
		case "current_unversioned":
			_, err = fmt.Fprintf(stdout, "Latest release: %s (current build %s has no comparable version).\n", result.Latest, result.Current)
		case "update_available":
			_, err = fmt.Fprintf(stdout, "Update available: %s -> %s\n", result.Current, result.Latest)
		case "upgraded":
			_, err = fmt.Fprintf(stdout, "Upgraded %s -> %s\n", result.Current, result.Latest)
		default:
			_, err = fmt.Fprintf(stdout, "No newer release. Current: %s, latest: %s\n", result.Current, result.Latest)
		}
	}
	if err != nil {
		return commandError(stderr, asJSON, &client.Error{Code: "output_error", Message: "Cannot write update result"}, "")
	}
	return 0
}

func runDownload(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("update download", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", dataDirHelp)
	asJSON := flags.Bool("json", false, outputJSONHelp)
	var versions []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		versions = []string{args[0]}
		args = args[1:]
	}
	versionArg := requiredArgument{label: "<version>", description: "Published release tag, for example v1.2.3."}
	if code, done := parseFlags(flags, args, downloadUsage, stdout, stderr, versionArg); done {
		return code
	}
	versions = append(versions, flags.Args()...)
	if len(versions) != 1 || !update.ValidReleaseTag(versions[0]) || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, downloadUsage)
		return 2
	}
	if deps.StageUpdate == nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "configuration_error", Message: "Release downloader is unavailable"}, "")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := deps.StageUpdate(ctx, versions[0], *dir)
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "release_download_failed", Message: err.Error()}, "")
	}
	if *asJSON {
		err = json.NewEncoder(stdout).Encode(result)
	} else {
		_, err = fmt.Fprintf(stdout, "Candidate %s: %s\nSHA-256: %s\n", result.Version, result.Path, result.SHA256)
	}
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "output_error", Message: "Cannot write release download result"}, "")
	}
	return 0
}
