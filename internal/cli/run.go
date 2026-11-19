package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
)

func Run(args []string, info buildinfo.Info, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		fmt.Fprintln(stdout, "Usage: wallet-bridge version [--json]")
		return 0
	}
	if args[0] != "version" {
		fmt.Fprintf(stderr, "Unknown command: %s\nUsage: wallet-bridge version [--json]\n", args[0])
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
