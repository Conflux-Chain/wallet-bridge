package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
)

type Dependencies struct {
	BuildInfo  buildinfo.Info
	RunService func(dataDir string) error
}

const usage = "Usage: wallet-bridge version [--json]\n       wallet-bridge service run [--data-dir <absolute path>]"

func Run(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	info := dependencies.BuildInfo
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if args[0] == "service" {
		return runService(args[1:], dependencies, stdout, stderr)
	}
	if args[0] != "version" {
		fmt.Fprintf(stderr, "Unknown command: %s\n%s\n", args[0], usage)
		return 2
	}
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonOutput := flags.Bool("json", false, "Print version information as JSON")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, "Usage: wallet-bridge version [--json]")
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "version does not accept positional arguments")
		return 2
	}
	var err error
	if *jsonOutput {
		err = json.NewEncoder(stdout).Encode(info)
	} else {
		_, err = fmt.Fprintf(stdout, "wallet-bridge %s (%s; %s)\n", info.Version, info.Platform, info.GoVersion)
	}
	if err != nil {
		fmt.Fprintf(stderr, "Cannot write version information: %v\n", err)
		return 1
	}
	return 0
}

func runService(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	const help = "Usage: wallet-bridge service run [--data-dir <absolute path>]"
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprintln(stdout, help)
		return 0
	}
	if args[0] != "run" {
		fmt.Fprintln(stderr, help)
		return 2
	}
	flags := flag.NewFlagSet("service run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, help)
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, "service run accepts only an absolute --data-dir and no positional arguments")
		return 2
	}
	if dependencies.RunService == nil {
		fmt.Fprintln(stderr, "configuration_error: service runner is unavailable")
		return 1
	}
	if err := dependencies.RunService(*dir); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
