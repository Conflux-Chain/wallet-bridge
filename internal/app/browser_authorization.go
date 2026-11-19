package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	BrowserTicketLifetime = 2 * time.Minute
	MaxBrowserTickets     = 64
)

var (
	ErrInvalidBrowserTicket     = errors.New("browser authorization ticket is invalid or expired")
	ErrBrowserTicketLimit       = errors.New("too many outstanding browser authorization tickets")
	ErrInvalidBrowserCredential = errors.New("browser authorization is invalid or expired")
)

// BrowserTicket contains a one-time secret, returned only to the issuing caller.
type BrowserTicket struct {
	Token     string `json:"-"`
	ExpiresAt time.Time
}

// BrowserAuthorizationResult returns the cookie secret separately from metadata.
// Callers must not include Token in status, list or diagnostic output.
type BrowserAuthorizationResult struct {
	Token string `json:"-"`
	Grant BrowserGrant
}

// BrowserAuthorization owns the one-time tickets for one running service.
// Construct one instance per service startup; persistent grants live in Store.
type BrowserAuthorization struct {
	store   BrowserGrantStore
	now     func() time.Time
	random  io.Reader
	mu      sync.Mutex
	tickets map[[32]byte]time.Time
}

func NewBrowserAuthorization(store BrowserGrantStore) (*BrowserAuthorization, error) {
	if store == nil {
		return nil, errors.New("browser authorization requires a store")
	}
	return &BrowserAuthorization{
		store: store, now: time.Now, random: rand.Reader, tickets: make(map[[32]byte]time.Time),
	}, nil
}

func (a *BrowserAuthorization) Issue(ctx context.Context) (BrowserTicket, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BrowserTicket{}, err
	}
	now := a.now()
	a.expireTickets(now)
	if len(a.tickets) >= MaxBrowserTickets {
		return BrowserTicket{}, ErrBrowserTicketLimit
	}
	token, digest, err := a.secret()
	if err != nil {
		return BrowserTicket{}, err
	}
	// A collision must never replace or extend an existing ticket.
	if _, exists := a.tickets[digest]; exists {
		return BrowserTicket{}, errors.New("browser ticket generation collided")
	}
	expires := now.Add(BrowserTicketLifetime)
	a.tickets[digest] = expires
	return BrowserTicket{Token: token, ExpiresAt: expires}, nil
}

func (a *BrowserAuthorization) Authorize(ctx context.Context, ticket string) (BrowserAuthorizationResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BrowserAuthorizationResult{}, err
	}
	a.expireTickets(a.now())
	digest, valid := browserSecretHash(ticket)
	if !valid {
		return BrowserAuthorizationResult{}, ErrInvalidBrowserTicket
	}
	if _, exists := a.tickets[digest]; !exists {
		return BrowserAuthorizationResult{}, ErrInvalidBrowserTicket
	}
	// Consumption is final even when a later write fails or its response is lost.
	delete(a.tickets, digest)
	token, cookieHash, err := a.secret()
	if err != nil {
		return BrowserAuthorizationResult{}, err
	}
	var id [16]byte
	if _, err := io.ReadFull(a.random, id[:]); err != nil {
		return BrowserAuthorizationResult{}, fmt.Errorf("generate browser grant ID: %w", err)
	}
	issued := a.now().UTC().Truncate(time.Millisecond)
	grant := BrowserGrant{ID: hex.EncodeToString(id[:]), IssuedAt: issued, ExpiresAt: issued.Add(BrowserGrantLifetime)}
	if err := a.store.CreateBrowserGrant(ctx, grant, cookieHash); err != nil {
		return BrowserAuthorizationResult{}, err
	}
	return BrowserAuthorizationResult{Token: token, Grant: grant}, nil
}

func (a *BrowserAuthorization) Authenticate(ctx context.Context, token string) (BrowserGrant, error) {
	digest, valid := browserSecretHash(token)
	if !valid {
		return BrowserGrant{}, ErrInvalidBrowserCredential
	}
	grant, err := a.store.FindActiveBrowserGrant(ctx, digest, a.now())
	if errors.Is(err, ErrBrowserGrantNotFound) {
		return BrowserGrant{}, ErrInvalidBrowserCredential
	}
	return grant, err
}

func (a *BrowserAuthorization) List(ctx context.Context) ([]BrowserGrant, error) {
	return a.store.ListBrowserGrants(ctx)
}

func (a *BrowserAuthorization) Logout(ctx context.Context, token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	grant, err := a.Authenticate(ctx, token)
	if err != nil {
		return err
	}
	return a.store.RevokeBrowserGrant(ctx, grant.ID, a.now())
}

func (a *BrowserAuthorization) Revoke(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.store.RevokeBrowserGrant(ctx, id, a.now())
}

func (a *BrowserAuthorization) RevokeAll(ctx context.Context) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	count, err := a.store.RevokeAllBrowserGrants(ctx, a.now())
	if err != nil {
		return 0, err
	}
	clear(a.tickets)
	return count, nil
}

func (a *BrowserAuthorization) expireTickets(now time.Time) {
	for digest, expires := range a.tickets {
		if !now.Before(expires) {
			delete(a.tickets, digest)
		}
	}
}

func (a *BrowserAuthorization) secret() (string, [32]byte, error) {
	var value [32]byte
	if _, err := io.ReadFull(a.random, value[:]); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate browser secret: %w", err)
	}
	return hex.EncodeToString(value[:]), sha256.Sum256(value[:]), nil
}

func browserSecretHash(token string) ([32]byte, bool) {
	if len(token) != 64 {
		return [32]byte{}, false
	}
	value, err := hex.DecodeString(token)
	if err != nil || hex.EncodeToString(value) != token {
		return [32]byte{}, false
	}
	return sha256.Sum256(value), true
}
