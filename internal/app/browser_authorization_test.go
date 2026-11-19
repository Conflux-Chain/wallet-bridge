package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type grantStoreStub struct {
	BrowserGrantStore
	create    func(context.Context, BrowserGrant, [32]byte) error
	find      func(context.Context, [32]byte, time.Time) (BrowserGrant, error)
	revokeAll func(context.Context, time.Time) (int64, error)
}

func (s *grantStoreStub) CreateBrowserGrant(ctx context.Context, grant BrowserGrant, hash [32]byte) error {
	return s.create(ctx, grant, hash)
}

func (s *grantStoreStub) FindActiveBrowserGrant(ctx context.Context, hash [32]byte, now time.Time) (BrowserGrant, error) {
	return s.find(ctx, hash, now)
}

func (s *grantStoreStub) RevokeAllBrowserGrants(ctx context.Context, now time.Time) (int64, error) {
	return s.revokeAll(ctx, now)
}

func authorizationFixture(t *testing.T, store BrowserGrantStore) (*BrowserAuthorization, *time.Time) {
	t.Helper()
	a, err := NewBrowserAuthorization(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 19, 12, 0, 0, 123456789, time.UTC)
	a.now = func() time.Time { return now }
	return a, &now
}

func issueFixture(t *testing.T, a *BrowserAuthorization) BrowserTicket {
	t.Helper()
	ticket, err := a.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestExchangeIssuesIndependentFixedLifetimeCredential(t *testing.T) {
	ctx := context.Background()
	var recorded BrowserGrant
	var storedHash [32]byte
	store := &grantStoreStub{create: func(_ context.Context, grant BrowserGrant, hash [32]byte) error {
		recorded, storedHash = grant, hash
		return nil
	}}
	a, now := authorizationFixture(t, store)
	ticket := issueFixture(t, a)
	if !ticket.ExpiresAt.Equal(now.Add(BrowserTicketLifetime)) {
		t.Fatal("ticket expiration changed")
	}
	*now = ticket.ExpiresAt.Add(-time.Nanosecond)
	result, err := a.Authorize(ctx, ticket.Token)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := hex.DecodeString(result.Token)
	if err != nil || len(secret) != 32 || result.Token == ticket.Token || storedHash != sha256.Sum256(secret) {
		t.Fatal("exchange did not create an independent hashed credential")
	}
	id, err := hex.DecodeString(result.Grant.ID)
	if err != nil || len(id) != 16 || recorded != result.Grant ||
		!recorded.IssuedAt.Equal(now.Truncate(time.Millisecond)) ||
		!recorded.ExpiresAt.Equal(recorded.IssuedAt.Add(BrowserGrantLifetime)) {
		t.Fatal("grant metadata or lifetime is invalid")
	}
	if _, err := a.Authorize(ctx, ticket.Token); !errors.Is(err, ErrInvalidBrowserTicket) {
		t.Fatal("consumed ticket accepted", err)
	}
}

func TestTicketExpiryAndCapacity(t *testing.T) {
	a, now := authorizationFixture(t, &grantStoreStub{})
	ticket := issueFixture(t, a)
	for range MaxBrowserTickets - 1 {
		issueFixture(t, a)
	}
	if _, err := a.Issue(context.Background()); !errors.Is(err, ErrBrowserTicketLimit) {
		t.Fatal("unbounded outstanding tickets", err)
	}
	*now = ticket.ExpiresAt
	if _, err := a.Authorize(context.Background(), ticket.Token); !errors.Is(err, ErrInvalidBrowserTicket) {
		t.Fatal("ticket accepted at its expiration", err)
	}
	issueFixture(t, a)
	if len(a.tickets) != 1 {
		t.Fatal("expired tickets retained capacity")
	}
}

func TestConcurrentTicketExchangeCreatesOneGrant(t *testing.T) {
	writes := 0
	a, _ := authorizationFixture(t, &grantStoreStub{create: func(context.Context, BrowserGrant, [32]byte) error {
		writes++
		return nil
	}})
	ticket := issueFixture(t, a)
	results := make(chan error, 16)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := a.Authorize(context.Background(), ticket.Token)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInvalidBrowserTicket) {
			t.Fatal(err)
		}
	}
	if writes != 1 || successes != 1 {
		t.Fatalf("concurrent exchange: writes=%d successes=%d", writes, successes)
	}
}

func TestFailedExchangeCannotReplayTicket(t *testing.T) {
	for _, stage := range []string{"storage", "cookie randomness", "ID randomness"} {
		t.Run(stage, func(t *testing.T) {
			writes := 0
			a, _ := authorizationFixture(t, &grantStoreStub{create: func(context.Context, BrowserGrant, [32]byte) error {
				writes++
				return errors.New("storage unavailable")
			}})
			ticket := issueFixture(t, a)
			if stage == "cookie randomness" {
				a.random = bytes.NewReader(nil)
			} else if stage == "ID randomness" {
				a.random = bytes.NewReader(make([]byte, 32))
			}
			result, err := a.Authorize(context.Background(), ticket.Token)
			if err == nil || result.Token != "" || strings.Contains(err.Error(), ticket.Token) {
				t.Fatal("failed exchange returned or disclosed a secret")
			}
			if stage != "storage" && writes != 0 {
				t.Fatal("randomness failure reached storage")
			}
			if _, err := a.Authorize(context.Background(), ticket.Token); !errors.Is(err, ErrInvalidBrowserTicket) {
				t.Fatal("failed exchange could be replayed", err)
			}
		})
	}
}

func TestCanceledCallsAndIssueRandomnessFailure(t *testing.T) {
	a, _ := authorizationFixture(t, &grantStoreStub{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Issue(ctx); !errors.Is(err, context.Canceled) || len(a.tickets) != 0 {
		t.Fatal("canceled issue retained a ticket", err)
	}
	ticket := issueFixture(t, a)
	if _, err := a.Authorize(ctx, ticket.Token); !errors.Is(err, context.Canceled) || len(a.tickets) != 1 {
		t.Fatal("already canceled exchange consumed a ticket", err)
	}
	a.random = bytes.NewReader(nil)
	if _, err := a.Issue(context.Background()); !errors.Is(err, io.EOF) || len(a.tickets) != 1 {
		t.Fatal("randomness failure issued a ticket", err)
	}
}

func TestRevokeAllClearsTicketsOnlyAfterStorageSuccess(t *testing.T) {
	fail := true
	store := &grantStoreStub{revokeAll: func(context.Context, time.Time) (int64, error) {
		if fail {
			return 0, errors.New("storage unavailable")
		}
		return 2, nil
	}}
	a, _ := authorizationFixture(t, store)
	ticket := issueFixture(t, a)
	if _, err := a.RevokeAll(context.Background()); err == nil || len(a.tickets) != 1 {
		t.Fatal("failed revoke-all reported success or changed tickets")
	}
	fail = false
	count, err := a.RevokeAll(context.Background())
	if err != nil || count != 2 || len(a.tickets) != 0 {
		t.Fatal("successful revoke-all left outstanding tickets", err)
	}
	if _, err := a.Authorize(context.Background(), ticket.Token); !errors.Is(err, ErrInvalidBrowserTicket) {
		t.Fatal("old link reauthorized after revoke-all", err)
	}
}

func TestRevokeAllWaitsForInFlightExchange(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	written := false
	a, _ := authorizationFixture(t, &grantStoreStub{
		create: func(context.Context, BrowserGrant, [32]byte) error {
			close(started)
			<-finish
			written = true
			return nil
		},
		revokeAll: func(context.Context, time.Time) (int64, error) {
			if !written {
				return 0, errors.New("revocation raced ahead of authorization persistence")
			}
			return 1, nil
		},
	})
	ticket := issueFixture(t, a)
	exchanged := make(chan error, 1)
	go func() { _, err := a.Authorize(context.Background(), ticket.Token); exchanged <- err }()
	<-started
	revoked := make(chan error, 1)
	go func() { _, err := a.RevokeAll(context.Background()); revoked <- err }()
	close(finish)
	if err := <-exchanged; err != nil {
		t.Fatal(err)
	}
	if err := <-revoked; err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticationAlwaysConsultsPersistedState(t *testing.T) {
	calls := 0
	store := &grantStoreStub{}
	a, now := authorizationFixture(t, store)
	token, hash, err := a.secret()
	if err != nil {
		t.Fatal(err)
	}
	store.find = func(_ context.Context, got [32]byte, at time.Time) (BrowserGrant, error) {
		calls++
		if got != hash || !at.Equal(*now) {
			t.Fatal("lookup did not use the credential digest and current time")
		}
		if calls == 1 {
			return BrowserGrant{ID: "first"}, nil
		}
		return BrowserGrant{}, ErrBrowserGrantNotFound
	}
	if _, err := a.Authenticate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(context.Background(), token); !errors.Is(err, ErrInvalidBrowserCredential) {
		t.Fatal("stale authorization was cached", err)
	}
	for _, malformed := range []string{"", "short", strings.Repeat("AB", 32), token + "00"} {
		if _, err := a.Authenticate(context.Background(), malformed); !errors.Is(err, ErrInvalidBrowserCredential) {
			t.Fatal("malformed credential accepted", err)
		}
	}
	if calls != 2 {
		t.Fatal("malformed credential reached storage")
	}
}
