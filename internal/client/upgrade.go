package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

// ProbeCandidate polls only the public, signed identity challenge.
func (c *Client) ProbeCandidate(ctx context.Context, id, version string) (bool, error) {
	if !validHex(id, 16) || version == "" {
		return false, errors.New("invalid candidate probe")
	}
	_, _, err := c.request(ctx, "", "", requestOptions{verifyingUpgrade: id, serviceVersion: version})
	var failure *Error
	if errors.Is(err, errNotRunning) || errors.As(err, &failure) && failure.Code == "service_unavailable" {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) ReadyVersion(ctx context.Context) (string, error) {
	status, err := c.Ensure(ctx)
	if err != nil {
		return "", err
	}
	return status.Info.Version, nil
}

func verifyCandidateInfo(info api.InfoResponse, id, version string) error {
	if info.Ready || info.VerifyingUpgrade != id || info.Version != version ||
		info.APIVersion.Major != 1 || info.APIVersion.Minor < 16 {
		return &Error{"incompatible_service", "The service does not match the expected upgrade candidate", nil}
	}
	for _, capability := range api.ServiceCapabilities() {
		if !slices.Contains(info.Capabilities, capability) {
			return &Error{"incompatible_service", "The upgrade candidate lacks capability " + capability, nil}
		}
	}
	return nil
}

// StopForUpgrade restores the enabled service before asking it to check the wallet.
func (c *Client) StopForUpgrade(parent context.Context) (Status, error) {
	return c.stopForUpgrade(parent, nil)
}

// StopPrevious performs the wallet check for the upgrade holding the writer lock.
func (c *Client) StopPrevious(ctx context.Context, check update.WalletCheck) error {
	_, err := c.stopForUpgrade(ctx, &check)
	var failure *Error
	if errors.As(err, &failure) && (failure.Code == "wallet_busy" || failure.Code == "incompatible_service") {
		return errors.Join(err, update.ErrStopRefused)
	}
	return err
}

func (c *Client) stopForUpgrade(parent context.Context, check *update.WalletCheck) (Status, error) {
	if _, err := c.ensure(parent, check); err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.Code == "incompatible_service" {
			return Status{}, &Error{failure.Code, "The running service cannot check the wallet for an upgrade", err}
		}
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(parent, c.stopTimeout)
	defer cancel()
	data, _, err := c.request(ctx, "POST", "/api/v1/control/stop-for-upgrade", requestOptions{
		expected: http.StatusAccepted, capability: upgradeStopCapability, serviceVersion: check.Version(),
	})
	if ctx.Err() != nil {
		return Status{}, stopDeadline(parent, ctx)
	}
	if errors.Is(err, errNotRunning) {
		return Status{}, &Error{"service_unavailable", "The service stopped before it confirmed the wallet check", err}
	}
	if err != nil {
		return Status{}, err
	}
	return c.confirmStop(parent, ctx, data)
}

type UpgradeBusyError struct {
	Request api.ActiveWalletRequestResponse
}

func (e *UpgradeBusyError) Error() string {
	r := e.Request
	return fmt.Sprintf("Handle the %s request %s (%s) at %s/?%s=%s and retry the upgrade", r.Kind, r.ID, r.Phase, api.Origin, r.Kind, r.ID)
}

func upgradeStopError(err error) error {
	var response *responseError
	var failure api.UpgradeBusyResponse
	if errors.As(err, &response) && json.Unmarshal(response.body, &failure) == nil {
		r := failure.ActiveWalletRequest
		switch {
		case response.status == 409 && failure.Code == "wallet_busy" &&
			(r.Kind == "connection" || r.Kind == "transaction") && validHex(r.ID, 16) &&
			(r.Phase == "ready" || r.Phase == "calling" || r.Phase == "interrupted_calling"):
			busy := &UpgradeBusyError{Request: r}
			return &Error{failure.Code, busy.Error(), busy}
		case response.status == 503 && failure.Code == "upgrade_in_progress":
			return &Error{failure.Code, "An upgrade is already stopping this service", err}
		case response.status == 500 && failure.Code == "storage_error":
			return &Error{failure.Code, "The service could not check wallet occupancy", err}
		}
	}
	return &Error{"request_failed", "The service did not confirm the upgrade stop", err}
}
