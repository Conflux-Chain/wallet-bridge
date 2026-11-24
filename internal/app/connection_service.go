package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ConnectionInput identifies one logical request across transport retries.
// Only the service assigns timestamps and lifecycle state.
type ConnectionInput struct {
	ID         string
	ClientName string
	Intent     string
	ChainID    string
	ChainName  string
}

type ConnectionBinding struct {
	Wallet  WalletDescriptor
	Account string
	ChainID string
}

// MatchesSession compares the page's latest wallet observations with the fixed binding.
func (binding ConnectionBinding) MatchesSession(session Session) bool {
	observed, err := normalizeConnectionBinding(binding)
	return err == nil && observed.Wallet == session.Wallet && observed.Account == session.Account && observed.ChainID == session.ChainID
}

// ConnectionService is internal orchestration, not an authorization boundary.
// Adapters authenticate callers; completion/rejection additionally require the
// browser's consent and execution ownership before invoking these methods.
type ConnectionService struct {
	store  ConnectionServiceStore
	now    func() time.Time
	random io.Reader
}

type ConnectionServiceStore interface {
	ConnectionStore
	ConnectionExecutionStore
	ListActivity(context.Context, time.Time, *ActivityKey, int) ([]Activity, error)
}

func NewConnectionService(store ConnectionServiceStore) (*ConnectionService, error) {
	if store == nil {
		return nil, errors.New("connection service requires a store")
	}
	return &ConnectionService{store: store, now: time.Now, random: rand.Reader}, nil
}

func (a *ConnectionService) Create(ctx context.Context, input ConnectionInput) (ConnectionRequest, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionRequest{}, err
	}
	chain, ok := canonicalHexQuantity(input.ChainID)
	if !connectionID(input.ID) || strings.TrimSpace(input.ClientName) == "" || !ok {
		return ConnectionRequest{}, ErrInvalidConnectionRequest
	}
	now := a.now().UTC().Truncate(time.Millisecond)
	request := ConnectionRequest{
		ID: input.ID, ClientName: input.ClientName, Intent: input.Intent,
		ChainID: chain, ChainName: input.ChainName, State: ConnectionPending,
		CreatedAt: now, ExpiresAt: now.Add(ConnectionRequestLifetime),
	}
	err := a.store.CreateConnectionRequest(ctx, request)
	if !errors.Is(err, ErrConnectionRequestExists) {
		if err != nil {
			return ConnectionRequest{}, err
		}
		return request, nil
	}
	original, err := a.Get(ctx, input.ID)
	if err != nil {
		return ConnectionRequest{}, err
	}
	if original.ClientName != request.ClientName || original.Intent != request.Intent ||
		original.ChainID != request.ChainID || original.ChainName != request.ChainName {
		return original, ErrConnectionConflict
	}
	return original, nil
}

// Get reads the original connection without renewing its resulting Session.
func (a *ConnectionService) Get(ctx context.Context, id string) (ConnectionRequest, error) {
	if !connectionID(id) {
		return ConnectionRequest{}, ErrInvalidConnectionRequest
	}
	request, err := a.store.GetConnectionRequest(ctx, id)
	if err != nil {
		return ConnectionRequest{}, err
	}
	now := a.now()
	if request.State == ConnectionPending && !now.Before(request.ExpiresAt) {
		request, err = a.store.FinishConnectionRequest(ctx, id, ConnectionExpired, now)
		if errors.Is(err, ErrConnectionConflict) {
			// A competing decision won. A read returns that durable outcome.
			err = nil
		}
	}
	return request, err
}

func (a *ConnectionService) Complete(ctx context.Context, id string, binding ConnectionBinding) (ConnectionRequest, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionRequest{}, err
	}
	if !connectionID(id) {
		return ConnectionRequest{}, ErrInvalidConnectionRequest
	}
	binding, err := normalizeConnectionBinding(binding)
	if err != nil {
		return ConnectionRequest{}, err
	}
	request, err := a.Get(ctx, id)
	if err != nil {
		return ConnectionRequest{}, err
	}
	if request.State != ConnectionPending && request.State != ConnectionSucceeded {
		return request, ErrConnectionConflict
	}
	if binding.ChainID != request.ChainID {
		return request, ErrInvalidSession
	}
	sessionID := request.SessionID
	if request.State == ConnectionPending {
		var value [16]byte
		if _, err := io.ReadFull(a.random, value[:]); err != nil {
			return ConnectionRequest{}, fmt.Errorf("generate Session ID: %w", err)
		}
		sessionID = hex.EncodeToString(value[:])
	}
	completion := ConnectionCompletion{SessionID: sessionID, Wallet: binding.Wallet, Account: binding.Account, ChainID: binding.ChainID}
	result, err := a.store.CompleteConnectionRequest(ctx, id, completion, a.now())
	if errors.Is(err, ErrConnectionConflict) && result.State == ConnectionSucceeded && result.SessionID != sessionID {
		// Another completion may have chosen its ID after our read. Reuse that
		// ID; the transaction still verifies every immutable binding field.
		completion.SessionID = result.SessionID
		return a.store.CompleteConnectionRequest(ctx, id, completion, a.now())
	}
	return result, err
}

func (a *ConnectionService) Cancel(ctx context.Context, id string) (ConnectionRequest, error) {
	return a.store.FinishConnectionRequest(ctx, id, ConnectionCanceled, a.now())
}

func (a *ConnectionService) Reject(ctx context.Context, id string) (ConnectionRequest, error) {
	return a.store.FinishConnectionRequest(ctx, id, ConnectionRejected, a.now())
}

// Session is a client access: only a still-active Session renews its idle time.
// Closed records remain queryable and must not be used for wallet operations.
func (a *ConnectionService) Session(ctx context.Context, id string) (Session, error) {
	if !connectionID(id) {
		return Session{}, ErrInvalidSession
	}
	return a.store.AccessSession(ctx, id, a.now())
}

func (a *ConnectionService) CloseSession(ctx context.Context, id string) (Session, error) {
	if !connectionID(id) {
		return Session{}, ErrInvalidSession
	}
	return a.store.CloseSession(ctx, id, a.now())
}

// Restore runs once before service readiness. Maintain never revives an execution.
// Transaction support must add unfinished-request protection before Session GC.
func (a *ConnectionService) Restore(ctx context.Context) (ConnectionMaintenanceCounts, error) {
	return a.store.MaintainConnectionState(ctx, a.now(), true)
}

func (a *ConnectionService) Maintain(ctx context.Context) (ConnectionMaintenanceCounts, error) {
	return a.store.MaintainConnectionState(ctx, a.now(), false)
}

func normalizeConnectionBinding(binding ConnectionBinding) (ConnectionBinding, error) {
	chain, ok := canonicalHexQuantity(binding.ChainID)
	account := strings.ToLower(binding.Account)
	decoded, err := hex.DecodeString(strings.TrimPrefix(account, "0x"))
	if !ok || !strings.HasPrefix(account, "0x") || len(decoded) != 20 || err != nil ||
		strings.TrimSpace(binding.Wallet.Name) == "" ||
		(binding.Wallet.Kind != WalletEIP6963 && binding.Wallet.Kind != WalletLegacy) ||
		(binding.Wallet.Kind == WalletEIP6963 && strings.TrimSpace(binding.Wallet.RDNS) == "") {
		return ConnectionBinding{}, ErrInvalidSession
	}
	binding.Account, binding.ChainID = account, chain
	return binding, nil
}

func connectionID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == value
}

func canonicalHexQuantity(value string) (string, bool) {
	if len(value) < 3 || !strings.HasPrefix(value, "0x") {
		return "", false
	}
	digits := strings.ToLower(value[2:])
	for _, c := range digits {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	return "0x" + digits, true
}
