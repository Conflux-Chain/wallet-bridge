package app

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type connectionStoreStub struct {
	ConnectionStore
	ConnectionExecutionStore
	create   func(context.Context, ConnectionRequest) error
	get      func(context.Context, string) (ConnectionRequest, error)
	finish   func(context.Context, string, ConnectionState, time.Time) (ConnectionRequest, error)
	complete func(context.Context, string, ConnectionCompletion, time.Time) (ConnectionRequest, error)
}

func (s connectionStoreStub) CreateConnectionRequest(ctx context.Context, r ConnectionRequest) error {
	return s.create(ctx, r)
}
func (s connectionStoreStub) GetConnectionRequest(ctx context.Context, id string) (ConnectionRequest, error) {
	return s.get(ctx, id)
}
func (s connectionStoreStub) FinishConnectionRequest(ctx context.Context, id string, state ConnectionState, now time.Time) (ConnectionRequest, error) {
	return s.finish(ctx, id, state, now)
}
func (s connectionStoreStub) CompleteConnectionRequest(ctx context.Context, id string, c ConnectionCompletion, now time.Time) (ConnectionRequest, error) {
	return s.complete(ctx, id, c, now)
}

func coreFixture(t *testing.T, store ConnectionServiceStore) (*ConnectionService, ConnectionInput, time.Time) {
	t.Helper()
	a, err := NewConnectionService(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 10, 0, 0, 123456789, time.UTC)
	a.now = func() time.Time { return now }
	return a, ConnectionInput{ID: strings.Repeat("12", 16), ClientName: "Example CLI", Intent: "连接用途", ChainID: "0x47"}, now
}

func bindingFixture() ConnectionBinding {
	return ConnectionBinding{
		Wallet:  WalletDescriptor{Kind: WalletEIP6963, Name: "Example Wallet", RDNS: "org.example.wallet"},
		Account: "0x000000000000000000000000000000000000aBcD", ChainID: "0x0047",
	}
}

func TestConnectionCoreCreationResponseLoss(t *testing.T) {
	ctx := context.Background()
	var saved ConnectionRequest
	responseLost := errors.New("response lost after commit")
	a, input, now := coreFixture(t, connectionStoreStub{
		create: func(_ context.Context, r ConnectionRequest) error {
			if saved.ID != "" {
				return ErrConnectionRequestExists
			}
			saved = r
			return responseLost
		},
		get: func(context.Context, string) (ConnectionRequest, error) { return saved, nil },
	})
	input.ChainID = "0x00047"
	if _, err := a.Create(ctx, input); !errors.Is(err, responseLost) {
		t.Fatal("lost response was hidden", err)
	}
	a.now = func() time.Time { return now.Add(time.Minute) }
	got, err := a.Create(ctx, input)
	if err != nil || !reflect.DeepEqual(got, saved) || got.ChainID != "0x47" ||
		!got.CreatedAt.Equal(now.Truncate(time.Millisecond)) ||
		!got.ExpiresAt.Equal(got.CreatedAt.Add(ConnectionRequestLifetime)) {
		t.Fatal("retry replaced request or extended deadline", got, saved, err)
	}
	for _, change := range []func(*ConnectionInput){
		func(i *ConnectionInput) { i.ClientName += " changed" },
		func(i *ConnectionInput) { i.Intent += " changed" },
		func(i *ConnectionInput) { i.ChainID = "0x406" },
		func(i *ConnectionInput) { i.ChainName = "changed" },
	} {
		changed := input
		change(&changed)
		got, err := a.Create(ctx, changed)
		if !errors.Is(err, ErrConnectionConflict) || !reflect.DeepEqual(got, saved) {
			t.Fatal("changed retry did not retain original outcome", got, err)
		}
	}
}

func TestConnectionCoreExpiryReadCompetition(t *testing.T) {
	for _, winner := range []ConnectionState{ConnectionExpired, ConnectionCanceled, ConnectionSucceeded} {
		t.Run(string(winner), func(t *testing.T) {
			var pending, result ConnectionRequest
			a, input, now := coreFixture(t, connectionStoreStub{
				get: func(context.Context, string) (ConnectionRequest, error) { return pending, nil },
				finish: func(_ context.Context, id string, state ConnectionState, at time.Time) (ConnectionRequest, error) {
					if id != inputID() || state != ConnectionExpired || !at.Equal(pending.ExpiresAt) {
						t.Fatal("incorrect expiry decision", id, state, at)
					}
					if winner != ConnectionExpired {
						return result, ErrConnectionConflict
					}
					return result, nil
				},
			})
			pending = ConnectionRequest{ID: input.ID, State: ConnectionPending, CreatedAt: now.Add(-ConnectionRequestLifetime), ExpiresAt: now}
			result = pending
			result.State, result.FinishedAt = winner, &now
			got, err := a.Get(context.Background(), input.ID)
			if err != nil || !reflect.DeepEqual(got, result) {
				t.Fatal("read lost competing durable result", got, err)
			}
		})
	}
}

func inputID() string { return strings.Repeat("12", 16) }

func TestConnectionCoreCompletionConcurrentID(t *testing.T) {
	ctx := context.Background()
	var pending ConnectionRequest
	committedID := strings.Repeat("34", 16)
	calls := 0
	a, input, _ := coreFixture(t, connectionStoreStub{
		get: func(context.Context, string) (ConnectionRequest, error) { return pending, nil },
		complete: func(_ context.Context, _ string, c ConnectionCompletion, _ time.Time) (ConnectionRequest, error) {
			calls++
			if c.Account != strings.ToLower(bindingFixture().Account) || c.ChainID != "0x47" || c.Wallet != bindingFixture().Wallet {
				t.Fatal("incorrect normalized binding", c)
			}
			result := pending
			result.State, result.SessionID = ConnectionSucceeded, committedID
			if calls == 1 {
				if c.SessionID == committedID {
					t.Fatal("fixture did not simulate competing ID")
				}
				return result, ErrConnectionConflict
			}
			if calls != 2 || c.SessionID != committedID {
				t.Fatal("retry did not reuse committed ID", c, calls)
			}
			return result, nil
		},
	})
	pending = ConnectionRequest{ID: input.ID, State: ConnectionPending, ChainID: "0x47", ExpiresAt: a.now().Add(time.Minute)}
	got, err := a.Complete(ctx, input.ID, bindingFixture())
	if err != nil || got.SessionID != committedID || calls != 2 {
		t.Fatal("same concurrent result not recognized", got, err)
	}
}

func TestConnectionCoreValidationAndFailures(t *testing.T) {
	ctx := context.Background()
	a, input, _ := coreFixture(t, connectionStoreStub{}) // Any storage call fails the test.
	for _, change := range []func(*ConnectionInput){
		func(i *ConnectionInput) { i.ID = "" },
		func(i *ConnectionInput) { i.ID = strings.Repeat("AA", 16) },
		func(i *ConnectionInput) { i.ClientName = " \t" },
		func(i *ConnectionInput) { i.ChainID = "71" },
		func(i *ConnectionInput) { i.ChainID = "0x" },
		func(i *ConnectionInput) { i.ChainID = "0x-1" },
	} {
		bad := input
		change(&bad)
		if _, err := a.Create(ctx, bad); !errors.Is(err, ErrInvalidConnectionRequest) {
			t.Fatal("invalid creation accepted", bad, err)
		}
	}
	for _, change := range []func(*ConnectionBinding){
		func(b *ConnectionBinding) { b.ChainID = "0xgg" },
		func(b *ConnectionBinding) { b.Account = "0x1234" },
		func(b *ConnectionBinding) { b.Wallet.Kind = "unknown" },
		func(b *ConnectionBinding) { b.Wallet.Name = " " },
		func(b *ConnectionBinding) { b.Wallet.RDNS = "" },
	} {
		bad := bindingFixture()
		change(&bad)
		if _, err := a.Complete(ctx, input.ID, bad); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("invalid binding accepted", bad, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.Create(canceled, input); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled create accepted", err)
	}
	if _, err := a.Complete(canceled, input.ID, bindingFixture()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled completion accepted", err)
	}
	if _, err := NewConnectionService(nil); err == nil {
		t.Fatal("nil store accepted")
	}

	pending := ConnectionRequest{ID: input.ID, State: ConnectionPending, ChainID: "0x47", ExpiresAt: a.now().Add(time.Minute)}
	a.store = connectionStoreStub{get: func(context.Context, string) (ConnectionRequest, error) { return pending, nil }}
	a.random = strings.NewReader("")
	if _, err := a.Complete(ctx, input.ID, bindingFixture()); !errors.Is(err, io.EOF) {
		t.Fatal("random failure hidden", err)
	}
	pending.State, pending.SessionID = ConnectionSucceeded, strings.Repeat("56", 16)
	a.store = connectionStoreStub{
		get: func(context.Context, string) (ConnectionRequest, error) { return pending, nil },
		complete: func(_ context.Context, _ string, c ConnectionCompletion, _ time.Time) (ConnectionRequest, error) {
			if c.SessionID != pending.SessionID {
				t.Fatal("replay replaced committed Session ID")
			}
			return pending, nil
		},
	}
	if _, err := a.Complete(ctx, input.ID, bindingFixture()); err != nil {
		t.Fatal("replay depended on new randomness", err)
	}
}
