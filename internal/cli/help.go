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

func writeCommandHelp(out io.Writer, syntax string, flags *flag.FlagSet) {
	fmt.Fprintln(out, syntax)
	fmt.Fprintln(out, "\nOptions:")
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	flags.VisitAll(func(f *flag.Flag) {
		if f.Usage == "" {
			return
		}
		valueName, description := flag.UnquoteUsage(f)
		fmt.Fprintf(table, "  --%s", f.Name)
		if valueName != "" {
			fmt.Fprintf(table, " <%s>", valueName)
		}
		fmt.Fprintf(table, "\t%s", description)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(table, " (default: %s)", f.DefValue)
		}
		fmt.Fprintln(table)
	})
	table.Flush()
}
