package client

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func startService(dir string) (*platform.BackgroundProcess, error) {
	selected, err := update.ServiceProgram(dir)
	if err != nil {
		return nil, &Error{"active_program_error", "Cannot use the enabled service program: " + err.Error(), err}
	}
	return platform.StartBackground(selected.Path, dir)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	data, info, err := c.request(ctx, "GET", "/api/v1/status", requestOptions{expected: 200})
	if ctx.Err() != nil {
		return Status{}, &Error{"canceled", "The service query was canceled", ctx.Err()}
	}
	if errors.Is(err, errNotRunning) {
		locked, lockErr := platform.ServiceLocked(c.dir)
		if lockErr != nil {
			return Status{}, &Error{"data_unavailable", "Cannot inspect the data directory lock", lockErr}
		}
		if locked {
			return Status{}, &Error{"service_unavailable", "The data directory is locked but the service is not responding", nil}
		}
		return Status{State: "stopped", Origin: api.Origin}, nil
	}
	if err != nil {
		return Status{}, err
	}
	var response api.StatusResponse
	if json.Unmarshal(data, &response) != nil || response.State != "running" {
		return Status{}, &Error{"request_failed", "The service returned an invalid status", nil}
	}
	if _, err := time.Parse(time.RFC3339Nano, response.StartedAt); err != nil {
		return Status{}, &Error{"request_failed", "The service returned an invalid start time", err}
	}
	return Status{State: "running", Origin: api.Origin, StartedAt: response.StartedAt, Info: &info}, nil
}

func (c *Client) Ensure(ctx context.Context) (Status, error) {
	return c.ensure(ctx, nil)
}

func (c *Client) ensure(ctx context.Context, check *update.WalletCheck) (Status, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, c.ensureTimeout)
	defer cancel()
	var process *platform.BackgroundProcess
	var done <-chan platform.ProcessResult
	for {
		if blocked, err := check.CheckAccess(c.dir); err != nil {
			return Status{}, &Error{"upgrade_state_error", "Cannot verify the local upgrade state", err}
		} else if blocked != "" {
			return Status{}, &Error{string(blocked), blocked.Error(), blocked}
		}
		status, err := c.Status(ctx)
		if ctx.Err() != nil {
			return Status{}, startupDeadline(parent, ctx)
		}
		if err == nil && status.State == "running" {
			return status, nil
		}
		var failure *Error
		if err != nil && (!errors.As(err, &failure) || failure.Code != "service_unavailable") {
			return Status{}, err
		}
		if process == nil && err == nil {
			if err := platform.EnsurePrivateDir(c.dir); err != nil {
				return Status{}, &Error{"data_unavailable", "Cannot establish a private data directory", err}
			}
			if check != nil {
				process, err = check.Start(c.dir)
			} else {
				process, err = c.start(c.dir)
			}
			if err != nil {
				if errors.As(err, &failure) {
					return Status{}, failure
				}
				return Status{}, &Error{"startup_failed", "Cannot launch the background service", err}
			}
			defer process.CloseDiagnostics()
			done = process.Done
		}
		select {
		case result := <-done:
			done = nil
			code, _, _ := strings.Cut(result.Diagnostic, ":")
			switch code {
			case string(update.UpgradeInProgress), string(update.UpgradeRecoveryRequired):
				blocked := update.UpgradeBlock(code)
				return Status{}, &Error{code, blocked.Error(), blocked}
			case "upgrade_state_error":
				return Status{}, &Error{code, "Cannot verify the local upgrade state", result.Err}
			case "already_running": // Wait for the candidate holding the directory lock.
			case "port_in_use":
				winner, err := c.Status(ctx)
				if ctx.Err() != nil {
					return Status{}, startupDeadline(parent, ctx)
				}
				if err == nil && winner.State == "running" {
					return winner, nil
				}
				if err == nil {
					return Status{}, &Error{"port_in_use", "The fixed loopback port is occupied or reserved", result.Err}
				}
				var failure *Error
				if !errors.As(err, &failure) || failure.Code != "service_unavailable" {
					return Status{}, err
				}
			case "credential_error", "data_unavailable", "listen_failed", "configuration_error", "storage_error", "active_program_error":
				return Status{}, &Error{code, "The background service could not initialize; run service run to diagnose locally", result.Err}
			default:
				return Status{}, &Error{"startup_failed", "The background service exited before readiness was confirmed; run service run to diagnose locally", result.Err}
			}
		case <-ctx.Done():
			return Status{}, startupDeadline(parent, ctx)
		case <-time.After(c.interval):
		}
	}
}

func startupDeadline(parent, ctx context.Context) error {
	if parent.Err() != nil {
		return &Error{"canceled", "Service startup waiting was canceled; the candidate was not terminated", parent.Err()}
	}
	return &Error{"startup_timeout", "Service readiness was not confirmed within the startup deadline; query status before retrying", ctx.Err()}
}

func (c *Client) Stop(ctx context.Context) (Status, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, c.stopTimeout)
	defer cancel()
	data, _, err := c.request(ctx, "POST", "/api/v1/control/stop", requestOptions{expected: 202})
	if ctx.Err() != nil {
		return Status{}, stopDeadline(parent, ctx)
	}
	if errors.Is(err, errNotRunning) {
		status, err := c.Status(ctx)
		if ctx.Err() != nil {
			return Status{}, stopDeadline(parent, ctx)
		}
		if err == nil && status.State != "stopped" {
			return Status{}, &Error{"service_unavailable", "The service started during the stop check; retry to verify and stop it explicitly", nil}
		}
		return status, err
	}
	if err != nil {
		return Status{}, err
	}
	return c.confirmStop(parent, ctx, data)
}

func (c *Client) confirmStop(parent, ctx context.Context, data []byte) (Status, error) {
	var response api.StopResponse
	if json.Unmarshal(data, &response) != nil || response.State != "stopping" {
		return Status{}, &Error{"request_failed", "The service returned an invalid stop acknowledgement", nil}
	}
	for {
		locked, err := platform.ServiceLocked(c.dir)
		if err != nil {
			return Status{}, &Error{"data_unavailable", "Cannot confirm release of the service lock", err}
		}
		if !locked {
			return Status{State: "stopped", Origin: api.Origin}, nil
		}
		select {
		case <-ctx.Done():
			return Status{}, stopDeadline(parent, ctx)
		case <-time.After(c.interval):
		}
	}
}

func stopDeadline(parent, ctx context.Context) error {
	if parent.Err() != nil {
		return &Error{"canceled", "Service stop waiting was canceled; no process was killed", parent.Err()}
	}
	return &Error{"stop_timeout", "Service shutdown was not confirmed within the stop deadline; no process was killed", ctx.Err()}
}
