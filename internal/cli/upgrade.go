package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

const upgradeUsage = "Usage: wallet-bridge upgrade [--include-preview] [--data-dir <absolute path>] [--json]"

func runUpgrade(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", "Use an absolute private data directory")
	preview := flags.Bool("include-preview", false, "Include prereleases in the version comparison")
	asJSON := flags.Bool("json", false, "Print structured output")
	if code, done := parseFlags(flags, args, upgradeUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, upgradeUsage)
		return 2
	}
	if deps.CheckUpdate == nil || deps.StageUpdate == nil || deps.NewClient == nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "configuration_error", Message: "Upgrade is unavailable"}, "")
	}
	if *dir == "" {
		path, err := platform.DataDir()
		if err != nil {
			return reportUpgradeError(stderr, *asJSON, err)
		}
		*dir = path
	}
	c, err := deps.NewClient(*dir)
	if err != nil {
		return reportUpgradeError(stderr, *asJSON, err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	result, err := update.Apply(ctx, *dir, c, func(current string) (update.Result, update.Candidate, error) {
		choice, err := deps.CheckUpdate(ctx, current, *preview)
		if err != nil {
			return choice, update.Candidate{}, &client.Error{Code: "release_query_failed", Message: err.Error(), Cause: err}
		}
		if choice.State == "no_release" || choice.State == "up_to_date" {
			return choice, update.Candidate{}, nil
		}
		candidate, err := deps.StageUpdate(ctx, choice.Latest, *dir)
		if err != nil {
			return choice, candidate, &client.Error{Code: "release_download_failed", Message: err.Error(), Cause: err}
		}
		return choice, candidate, nil
	})
	if err != nil {
		return reportUpgradeError(stderr, *asJSON, err)
	}
	return printUpdateResult(stdout, stderr, *asJSON, *preview, result)
}

func reportUpgradeError(stderr io.Writer, asJSON bool, err error) int {
	var rollback *update.RolledBackError
	var busy *client.UpgradeBusyError
	var blocked update.UpgradeBlock
	var serviceErr *client.Error
	failure := &client.Error{Code: "upgrade_failed", Message: err.Error()}
	switch {
	case errors.As(err, &rollback):
		failure.Code = "upgrade_rolled_back"
		if asJSON {
			json.NewEncoder(stderr).Encode(struct {
				*client.Error
				Target   string `json:"target"`
				Restored string `json:"restored"`
				Reason   string `json:"reason"`
			}{failure, rollback.Target, rollback.Restored, rollback.Cause.Error()})
			return 1
		}
	case errors.As(err, &busy):
		failure.Code, failure.Message = "wallet_busy", busy.Error()
		if asJSON {
			json.NewEncoder(stderr).Encode(api.UpgradeBusyResponse{
				ErrorResponse: api.ErrorResponse{Code: failure.Code, Message: failure.Message}, ActiveWalletRequest: busy.Request,
			})
			return 1
		}
	case errors.As(err, &blocked):
		failure.Code = string(blocked)
	case errors.As(err, &serviceErr):
		failure.Code = serviceErr.Code
	}
	return commandError(stderr, asJSON, failure, "")
}
