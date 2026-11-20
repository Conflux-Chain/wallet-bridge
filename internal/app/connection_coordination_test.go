package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type coordinationStub struct {
	connectionStoreStub
	claim  func(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	decide func(context.Context, ConnectionExecutionRef, ConnectionCompletion, time.Time) (ConnectionRequest, error)
}

func (s coordinationStub) ClaimConnectionExecution(ctx context.Context, ref ConnectionExecutionRef, at time.Time) (ConnectionExecution, error) {
	return s.claim(ctx, ref, at)
}
func (s coordinationStub) CompleteConnectionExecution(ctx context.Context, ref ConnectionExecutionRef, c ConnectionCompletion, at time.Time) (ConnectionRequest, error) {
	return s.decide(ctx, ref, c, at)
}

func TestConnectionCoordinatorSubjectAndValidation(t *testing.T) {
	ctx := context.Background()
	browser := BrowserGrant{ID: strings.Repeat("34", 16)}
	input := ConnectionExecutionInput{ConnectionID: inputID(), ID: strings.Repeat("56", 16)}
	core, _, _ := coreFixture(t, coordinationStub{claim: func(_ context.Context, ref ConnectionExecutionRef, at time.Time) (ConnectionExecution, error) {
		if ref.BrowserGrantID != browser.ID || ref.ConnectionID != input.ConnectionID || ref.ID != input.ID || !at.Equal(nowForCoordinator()) {
			t.Fatal("subject was not bound by the core", ref, at)
		}
		return ConnectionExecution{}, ErrInvalidBrowserCredential
	}})
	core.now = nowForCoordinator
	if _, err := core.ClaimExecution(ctx, browser, input); !errors.Is(err, ErrInvalidBrowserCredential) {
		t.Fatal("store's live authorization error was hidden", err)
	}
	core, _, _ = coreFixture(t, connectionStoreStub{}) // Any storage call is unexpected.
	for _, badBrowser := range []BrowserGrant{{}, {ID: "bad"}} {
		for _, call := range []func() error{
			func() error { _, err := core.Execution(ctx, badBrowser, input.ConnectionID); return err },
			func() error { _, err := core.ClaimExecution(ctx, badBrowser, input); return err },
			func() error { _, err := core.TouchExecution(ctx, badBrowser, input); return err },
			func() error { _, err := core.InterruptExecution(ctx, badBrowser, input); return err },
			func() error { _, err := core.RecoverExecution(ctx, badBrowser, input.ID, input); return err },
			func() error { _, err := core.BeginCall(ctx, badBrowser, input, input.ID, ""); return err },
			func() error { _, err := core.ReturnCall(ctx, badBrowser, input, input.ID); return err },
			func() error { _, err := core.CompleteExecution(ctx, badBrowser, input, bindingFixture()); return err },
			func() error { _, err := core.RejectExecution(ctx, badBrowser, input); return err },
		} {
			if err := call(); !errors.Is(err, ErrInvalidBrowserCredential) {
				t.Fatal(err)
			}
		}
	}
	if _, err := core.CompleteExecution(ctx, browser, ConnectionExecutionInput{}, bindingFixture()); !errors.Is(err, ErrInvalidConnectionExecution) {
		t.Fatal(err)
	}
	bad := bindingFixture()
	bad.Account = "bad"
	if _, err := core.CompleteExecution(ctx, browser, input, bad); !errors.Is(err, ErrInvalidSession) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := core.CompleteExecution(canceled, browser, input, bindingFixture()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func nowForCoordinator() time.Time { return time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC) }

func TestConnectionCoordinatorCompletionRetryAndFailures(t *testing.T) {
	ctx := context.Background()
	browser := BrowserGrant{ID: strings.Repeat("34", 16)}
	input := ConnectionExecutionInput{ConnectionID: inputID(), ID: strings.Repeat("56", 16)}
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent Session ID", true: "history without randomness"}[historical], func(t *testing.T) {
			winner := strings.Repeat("78", 16)
			calls := 0
			request := ConnectionRequest{ID: input.ConnectionID, State: ConnectionPending, ChainID: "0x47"}
			if historical {
				request.State, request.SessionID = ConnectionSucceeded, winner
			}
			core, _, _ := coreFixture(t, coordinationStub{
				connectionStoreStub: connectionStoreStub{get: func(context.Context, string) (ConnectionRequest, error) { return request, nil }},
				decide: func(_ context.Context, ref ConnectionExecutionRef, c ConnectionCompletion, _ time.Time) (ConnectionRequest, error) {
					calls++
					if ref.BrowserGrantID != browser.ID || ref.ID != input.ID || c.ChainID != "0x47" || c.Account != strings.ToLower(bindingFixture().Account) {
						t.Fatal(ref, c)
					}
					result := request
					result.State, result.SessionID = ConnectionSucceeded, winner
					if !historical && calls == 1 {
						if c.SessionID == winner {
							t.Fatal("fixture did not simulate an ID race")
						}
						return result, ErrConnectionConflict
					}
					if c.SessionID != winner {
						t.Fatal("retry replaced committed ID", c)
					}
					return result, nil
				},
			})
			if historical {
				core.random = strings.NewReader("")
			}
			got, err := core.CompleteExecution(ctx, browser, input, bindingFixture())
			want := 2
			if historical {
				want = 1
			}
			if err != nil || got.SessionID != winner || calls != want {
				t.Fatal(got, err, calls)
			}
		})
	}
	core, _, _ := coreFixture(t, coordinationStub{connectionStoreStub: connectionStoreStub{get: func(context.Context, string) (ConnectionRequest, error) { return ConnectionRequest{}, nil }}})
	core.random = strings.NewReader("")
	if _, err := core.CompleteExecution(ctx, browser, input, bindingFixture()); !errors.Is(err, io.EOF) {
		t.Fatal("entropy failure hidden", err)
	}
}
