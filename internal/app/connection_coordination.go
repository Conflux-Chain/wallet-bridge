package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// ConnectionExecutionInput comes from the page. Browser grant IDs are supplied
// separately by the authenticated adapter subject, never by this input.
type ConnectionExecutionInput struct {
	ConnectionID string
	ID           string
}

func executionRef(browser BrowserGrant, input ConnectionExecutionInput) (ConnectionExecutionRef, error) {
	if !connectionID(browser.ID) {
		return ConnectionExecutionRef{}, ErrInvalidBrowserCredential
	}
	if !connectionID(input.ConnectionID) || !connectionID(input.ID) {
		return ConnectionExecutionRef{}, ErrInvalidConnectionExecution
	}
	return ConnectionExecutionRef{ConnectionID: input.ConnectionID, ID: input.ID, BrowserGrantID: browser.ID}, nil
}

// Reads rely on the adapter's authenticated browser subject. They neither claim
// ownership nor renew liveness; writes additionally check the live grant in SQL.
func (a *ConnectionService) Execution(ctx context.Context, browser BrowserGrant, id string) (ConnectionExecution, error) {
	if !connectionID(browser.ID) {
		return ConnectionExecution{}, ErrInvalidBrowserCredential
	}
	if !connectionID(id) {
		return ConnectionExecution{}, ErrInvalidConnectionExecution
	}
	return a.store.GetConnectionExecution(ctx, id)
}

func (a *ConnectionService) ActiveWalletRequest(ctx context.Context, browser BrowserGrant) (*ActiveWalletRequest, error) {
	if !connectionID(browser.ID) {
		return nil, ErrInvalidBrowserCredential
	}
	return a.store.GetActiveWalletRequest(ctx)
}

func (a *ConnectionService) PendingWalletRequests(ctx context.Context, browser BrowserGrant) ([]PendingWalletRequest, error) {
	if !connectionID(browser.ID) {
		return nil, ErrInvalidBrowserCredential
	}
	return a.store.ListPendingWalletRequests(ctx, a.now())
}

// UpgradeBlocker serves the authenticated local service control endpoint.
func (a *ConnectionService) UpgradeBlocker(ctx context.Context) (*ActiveWalletRequest, error) {
	return a.store.GetActiveWalletRequest(ctx)
}

func (a *ConnectionService) ClaimExecution(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput) (ConnectionExecution, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionExecution{}, err
	}
	return a.store.ClaimConnectionExecution(ctx, ref, a.now())
}

func (a *ConnectionService) TouchExecution(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput) (ConnectionExecution, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionExecution{}, err
	}
	return a.store.TouchConnectionExecution(ctx, ref, a.now())
}

func (a *ConnectionService) InterruptExecution(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput) (ConnectionExecution, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionExecution{}, err
	}
	return a.store.InterruptConnectionExecution(ctx, ref, a.now())
}

func (a *ConnectionService) RecoverExecution(ctx context.Context, browser BrowserGrant, previousID string, input ConnectionExecutionInput) (ConnectionExecution, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionExecution{}, err
	}
	return a.store.RecoverConnectionExecution(ctx, previousID, ref, a.now())
}

func (a *ConnectionService) BeginCall(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput, callID, previousCallID string) (ConnectionCallPermit, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionCallPermit{}, err
	}
	return a.store.BeginConnectionCall(ctx, ConnectionCallRef{ConnectionExecutionRef: ref, CallID: callID}, previousCallID, a.now())
}

func (a *ConnectionService) ReturnCall(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput, callID string) (ConnectionExecution, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionExecution{}, err
	}
	return a.store.ReturnConnectionCall(ctx, ConnectionCallRef{ConnectionExecutionRef: ref, CallID: callID}, a.now())
}

func (a *ConnectionService) CompleteExecution(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput, binding ConnectionBinding) (ConnectionRequest, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionRequest{}, err
	}
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionRequest{}, err
	}
	binding, err = normalizeConnectionBinding(binding)
	if err != nil {
		return ConnectionRequest{}, err
	}
	// Read only to reuse an already committed Session ID. Every outcome, including
	// cancellation or expiry, still passes through the guarded transaction below.
	request, err := a.store.GetConnectionRequest(ctx, input.ConnectionID)
	if err != nil {
		return ConnectionRequest{}, err
	}
	sessionID := request.SessionID
	if sessionID == "" {
		var value [16]byte
		if _, err := io.ReadFull(a.random, value[:]); err != nil {
			return ConnectionRequest{}, fmt.Errorf("generate Session ID: %w", err)
		}
		sessionID = hex.EncodeToString(value[:])
	}
	completion := ConnectionCompletion{SessionID: sessionID, Wallet: binding.Wallet, Account: binding.Account, ChainID: binding.ChainID}
	result, err := a.store.CompleteConnectionExecution(ctx, ref, completion, a.now())
	if errors.Is(err, ErrConnectionConflict) && result.State == ConnectionSucceeded && result.SessionID != sessionID {
		completion.SessionID = result.SessionID
		return a.store.CompleteConnectionExecution(ctx, ref, completion, a.now())
	}
	return result, err
}

func (a *ConnectionService) RejectExecution(ctx context.Context, browser BrowserGrant, input ConnectionExecutionInput) (ConnectionRequest, error) {
	ref, err := executionRef(browser, input)
	if err != nil {
		return ConnectionRequest{}, err
	}
	return a.store.RejectConnectionExecution(ctx, ref, a.now())
}
