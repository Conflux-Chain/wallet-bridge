//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func connectionCore(t *testing.T, s app.ConnectionServiceStore) *app.ConnectionService {
	t.Helper()
	core, err := app.NewConnectionService(s)
	if err != nil {
		t.Fatal(err)
	}
	return core
}

func coreInput(index int) app.ConnectionInput {
	r := connectionFixture(index)
	return app.ConnectionInput{ID: r.ID, ClientName: r.ClientName, Intent: r.Intent, ChainID: r.ChainID, ChainName: r.ChainName}
}

func coreBinding(chain string) app.ConnectionBinding {
	return app.ConnectionBinding{
		Wallet:  app.WalletDescriptor{Kind: app.WalletEIP6963, Name: "Example Wallet", RDNS: "org.example.wallet"},
		Account: "0x000000000000000000000000000000000000aBcD", ChainID: chain,
	}
}

func TestConnectionCorePersistentCreationRetries(t *testing.T) {
	ctx := context.Background()
	for _, state := range []app.ConnectionState{app.ConnectionPending, app.ConnectionSucceeded,
		app.ConnectionRejected, app.ConnectionCanceled, app.ConnectionExpired} {
		t.Run(string(state), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			core, input := connectionCore(t, s), coreInput(0)
			if state == app.ConnectionExpired {
				old := connectionFixture(0)
				old.CreatedAt = time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Millisecond)
				old.ExpiresAt = old.CreatedAt.Add(app.ConnectionRequestLifetime)
				if err := s.CreateConnectionRequest(ctx, old); err != nil {
					t.Fatal(err)
				}
			}
			created, err := core.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case app.ConnectionSucceeded:
				_, err = core.Complete(ctx, input.ID, coreBinding(input.ChainID))
			case app.ConnectionCanceled:
				_, err = core.Cancel(ctx, input.ID)
			case app.ConnectionRejected:
				_, err = core.Reject(ctx, input.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			original, err := core.Get(ctx, input.ID)
			if err != nil || original.State != state || !original.ExpiresAt.Equal(created.ExpiresAt) {
				t.Fatal("incorrect original outcome", original, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTestStore(t, dir)
			core = connectionCore(t, s)
			input.ChainID = "0x00" + strings.ToUpper(input.ChainID[2:])
			got, err := core.Create(ctx, input)
			if err != nil || !reflect.DeepEqual(got, original) {
				t.Fatal("restart/retry replaced original request", got, original, err)
			}
			if state == app.ConnectionCanceled || state == app.ConnectionRejected || state == app.ConnectionExpired {
				got, err = core.Complete(ctx, input.ID, coreBinding(input.ChainID))
				if !errors.Is(err, app.ErrConnectionConflict) || !reflect.DeepEqual(got, original) {
					t.Fatal("late completion changed terminal request", got, err)
				}
				var count int
				if err := s.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 0 {
					t.Fatal("late completion created Session", count, err)
				}
			}
		})
	}
}

type coreReadBarrier struct {
	app.ConnectionServiceStore
	ready   sync.WaitGroup
	release chan struct{}
}

func (b *coreReadBarrier) GetConnectionRequest(ctx context.Context, id string) (app.ConnectionRequest, error) {
	r, err := b.ConnectionServiceStore.GetConnectionRequest(ctx, id)
	b.ready.Done()
	select {
	case <-b.release:
	case <-ctx.Done():
		return app.ConnectionRequest{}, ctx.Err()
	}
	return r, err
}

func TestConnectionCoreConcurrentCompletion(t *testing.T) {
	for _, conflicting := range []bool{false, true} {
		t.Run(map[bool]string{false: "same binding", true: "different binding"}[conflicting], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			input := coreInput(0)
			if _, err := connectionCore(t, s).Create(ctx, input); err != nil {
				t.Fatal(err)
			}
			barrier := &coreReadBarrier{ConnectionServiceStore: s, release: make(chan struct{})}
			barrier.ready.Add(2)
			core := connectionCore(t, barrier)
			type outcome struct {
				request app.ConnectionRequest
				err     error
			}
			results := make(chan outcome, 2)
			for i := range 2 {
				go func() {
					binding := coreBinding(input.ChainID)
					if conflicting && i == 1 {
						binding.Wallet.Name = "Other Wallet"
					}
					r, err := core.Complete(ctx, input.ID, binding)
					results <- outcome{r, err}
				}()
			}
			barrier.ready.Wait()
			close(barrier.release)
			first, second := <-results, <-results
			failures := 0
			for _, result := range []outcome{first, second} {
				if result.err != nil {
					if !errors.Is(result.err, app.ErrConnectionConflict) {
						t.Fatal(result.err)
					}
					failures++
				}
			}
			if (!conflicting && failures != 0) || (conflicting && failures != 1) ||
				first.request.SessionID == "" || first.request.SessionID != second.request.SessionID {
				t.Fatal("incorrect concurrent result", first, second)
			}
			var count int
			if err := s.db.QueryRow("SELECT count(*) FROM sessions").Scan(&count); err != nil || count != 1 {
				t.Fatal("completion created duplicate Sessions", count, err)
			}
		})
	}
}

func TestConnectionCoreSessionHistory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	core := connectionCore(t, s)
	request := connectionFixture(0)
	request.CreatedAt = time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	request.ExpiresAt = request.CreatedAt.Add(app.ConnectionRequestLifetime)
	if err := s.CreateConnectionRequest(ctx, request); err != nil {
		t.Fatal(err)
	}
	completion := completionFixture(request)
	if _, err := s.CompleteConnectionRequest(ctx, request.ID, completion, request.CreatedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetSession(ctx, completion.SessionID)
	if _, err := core.Get(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := s.GetSession(ctx, completion.SessionID)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("connection read renewed Session", unchanged, before)
	}
	active, err := core.Session(ctx, completion.SessionID)
	if err != nil || active.ClosedAt != nil || !active.LastAccessAt.After(before.LastAccessAt) {
		t.Fatal("client access did not renew active Session", active, err)
	}
	closed, err := core.CloseSession(ctx, completion.SessionID)
	if err != nil || closed.ClosedAt == nil {
		t.Fatal("Session close failed", closed, err)
	}
	binding := app.ConnectionBinding{Wallet: completion.Wallet, Account: completion.Account, ChainID: completion.ChainID}
	result, err := core.Complete(ctx, request.ID, binding)
	if err != nil || result.SessionID != completion.SessionID {
		t.Fatal("completion replay lost closed history", result, err)
	}
	history, err := core.Session(ctx, completion.SessionID)
	if err != nil || !reflect.DeepEqual(history, closed) {
		t.Fatal("query revived closed Session", history, err)
	}
	binding.Wallet.RDNS = "other.example"
	if _, err := core.Complete(ctx, request.ID, binding); !errors.Is(err, app.ErrConnectionConflict) {
		t.Fatal("conflicting replay changed closed binding", err)
	}
}
