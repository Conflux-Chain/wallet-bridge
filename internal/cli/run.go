package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

type Dependencies struct {
	BuildInfo     buildinfo.Info
	Install       func(dataDir string) (string, error)
	CheckUpdate   func(context.Context, string, bool) (update.Result, error)
	StageUpdate   func(context.Context, string, string) (update.Candidate, error)
	RunMCP        func(dataDir string) error
	NewClient     func(dataDir string) (*client.Client, error)
	RunService    func(dataDir, verifyingUpgrade string) error
	ManageService func(action, dataDir string) (client.Status, error)
	BrowserLink   func(dataDir string) (api.BrowserTicketResponse, error)
	ListBrowsers  func(dataDir string, limit int, after string) (api.BrowserGrantListResponse, error)
	RevokeBrowser func(dataDir, id string, all bool) (api.BrowserRevokeResponse, error)
	OpenBrowser   func(url string) error
}

const serviceUsage = "Usage: wallet-bridge service run [--data-dir <absolute path>]\n       wallet-bridge service ensure|status [--data-dir <absolute path>] [--json]\n       wallet-bridge service stop [--for-upgrade] [--data-dir <absolute path>] [--json]"
const mcpUsage = "Usage: wallet-bridge mcp [--data-dir <absolute path>]"
const usage = "Usage: wallet-bridge version [--json]\n" + installUsage + "\n" + updateUsage + "\n" + upgradeUsage + "\n" + mcpUsage + "\n" + serviceUsage + "\n" + uiUsage + "\n" + browserUsage + "\n" + connectUsage + "\n" + connectionUsage + "\n" + sessionUsage + "\n" + transactionUsage

func Run(args []string, dependencies Dependencies, stdout, stderr io.Writer) int {
	info := dependencies.BuildInfo
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")) {
		fmt.Fprintln(stdout, usage)
		return 0
	}
	if args[0] == "service" {
		return runService(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "install" {
		return runInstall(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "update" {
		return runUpdate(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "upgrade" {
		return runUpgrade(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "mcp" {
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		dir := flags.String("data-dir", "", "Use an absolute private data directory")
		if code, done := parseFlags(flags, args[1:], mcpUsage, stdout, stderr); done {
			return code
		}
		if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
			fmt.Fprintln(stderr, mcpUsage)
			return 2
		}
		if dependencies.RunMCP == nil {
			fmt.Fprintln(stderr, "configuration_error: MCP runner is unavailable")
			return 1
		}
		if err := dependencies.RunMCP(*dir); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if args[0] == "ui" {
		return runUI(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "browser" {
		return runBrowser(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "connect" {
		return runConnect(args[1:], dependencies, stdout, stderr)
	}
	if args[0] == "transaction" && len(args) > 1 && args[1] == "send" {
		return runSend(args[2:], dependencies, stdout, stderr)
	}
	if args[0] == "connection" || args[0] == "session" || args[0] == "transaction" {
		return runResource(args, dependencies, stdout, stderr)
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
	const help = serviceUsage
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprintln(stdout, help)
		return 0
	}
	action := args[0]
	if action != "run" && action != "ensure" && action != "status" && action != "stop" {
		fmt.Fprintln(stderr, help)
		return 2
	}
	flags := flag.NewFlagSet("service "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	var jsonOutput bool
	var forUpgrade bool
	var verifyingUpgrade string
	if action == "run" {
		flags.Func("verify-upgrade", "Start the candidate for this upgrade", func(id string) error {
			if id == "" {
				return errors.New("upgrade ID is required")
			}
			verifyingUpgrade = id
			return nil
		})
	}
	if action == "stop" {
		flags.BoolVar(&forUpgrade, "for-upgrade", false, "Check wallet occupancy before stopping for an upgrade")
	}
	if action != "run" {
		flags.BoolVar(&jsonOutput, "json", false, "Print structured output")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, help)
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, "service commands accept only an absolute --data-dir and no positional arguments")
		return 2
	}
	if action != "run" {
		var result client.Status
		var err error
		if dependencies.ManageService == nil {
			err = &client.Error{Code: "configuration_error", Message: "Service client is unavailable"}
		} else {
			if forUpgrade {
				action = "stop-for-upgrade"
			}
			result, err = dependencies.ManageService(action, *dir)
		}
		if err != nil {
			var failure *client.Error
			if !errors.As(err, &failure) {
				failure = &client.Error{Code: "request_failed", Message: "Service operation failed"}
			}
			if jsonOutput {
				var busy *client.UpgradeBusyError
				if errors.As(err, &busy) {
					json.NewEncoder(stderr).Encode(api.UpgradeBusyResponse{
						ErrorResponse: api.ErrorResponse{Code: failure.Code, Message: failure.Message}, ActiveWalletRequest: busy.Request,
					})
				} else {
					json.NewEncoder(stderr).Encode(failure)
				}
			} else {
				fmt.Fprintln(stderr, failure)
			}
			return 1
		}
		if jsonOutput {
			err = json.NewEncoder(stdout).Encode(result)
		} else {
			_, err = fmt.Fprintf(stdout, "wallet-bridge %s at %s\n", result.State, result.Origin)
		}
		if err != nil {
			fmt.Fprintln(stderr, "output_error: Cannot write service status")
			return 1
		}
		return 0
	}
	if dependencies.RunService == nil {
		fmt.Fprintln(stderr, "configuration_error: service runner is unavailable")
		return 1
	}
	if verifyingUpgrade != "" && *dir == "" {
		fmt.Fprintln(stderr, "service verification requires an upgrade ID and an absolute data directory")
		return 2
	}
	if err := dependencies.RunService(*dir, verifyingUpgrade); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
