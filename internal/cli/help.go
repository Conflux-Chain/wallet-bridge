package cli

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

const usage = `Usage: wallet-bridge <command> [options]

Wallet and transactions
  ui                  Authorize this browser and open the management page
  connect             Request a wallet connection and obtain a Session ID
  connection get      Show a connection request and its result
  connection cancel   Cancel a pending connection request
  session get         Show the wallet, account, network and state of a Session
  session close       Close a Session
  transaction send    Submit one transaction for review in the wallet
  transaction get     Show the request state and any returned transaction hash
  transaction cancel  Cancel a transaction before wallet submission
  browser list        List authorized browser profiles
  browser revoke      Revoke one or all browser authorizations

Service and agent
  mcp                 Run the MCP server over standard input and output
  service run         Run the local HTTP service in the foreground
  service ensure      Start or reuse the background service
  service status      Check whether the local service is running
  service stop        Stop the local service

Installation
  version             Show version and build information
  install             Install for this user and print the program path
  update check        Check GitHub Releases for a newer version
  update download     Download and verify a release for this platform
  upgrade             Apply an upgrade or resume an interrupted upgrade

Run wallet-bridge <command> --help for arguments and defaults.`

const dataDirHelp = "Absolute Bridge data directory. Defaults to the current user's Wallet Bridge directory."
const outputJSONHelp = "Write JSON output. By default, print readable text."

type requiredArgument struct {
	flagName    string // Registered flag, including the flag in a mutually exclusive choice.
	label       string // Positional argument or choice, e.g. "<id> | --all".
	description string
}

func writeCommandHelp(out io.Writer, syntax string, flags *flag.FlagSet, required ...requiredArgument) {
	fmt.Fprintln(out, syntax)
	listed := make(map[string]bool, len(required))
	if len(required) > 0 {
		fmt.Fprintln(out, "\nRequired arguments:")
		table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		for _, arg := range required {
			if arg.flagName != "" {
				listed[arg.flagName] = true
			}
			if arg.label != "" {
				fmt.Fprintf(table, "  %s\t%s\n", arg.label, arg.description)
			} else {
				writeHelpFlag(table, flags.Lookup(arg.flagName))
			}
		}
		table.Flush()
	}
	fmt.Fprintln(out, "\nOptions:")
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	flags.VisitAll(func(f *flag.Flag) {
		if f.Usage != "" && !listed[f.Name] {
			writeHelpFlag(table, f)
		}
	})
	table.Flush()
}

func writeHelpFlag(out io.Writer, f *flag.Flag) {
	valueName, description := flag.UnquoteUsage(f)
	fmt.Fprintf(out, "  --%s", f.Name)
	if valueName != "" {
		fmt.Fprintf(out, " <%s>", valueName)
	}
	fmt.Fprintf(out, "\t%s", description)
	if f.DefValue != "" && f.DefValue != "false" {
		fmt.Fprintf(out, " (default: %s)", f.DefValue)
	}
	fmt.Fprintln(out)
}
