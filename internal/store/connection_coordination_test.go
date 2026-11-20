//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func coordinatorBrowser(t *testing.T, s *Store) app.BrowserGrant {
	t.Helper()
	ctx := context.Background()
	authorization, err := app.NewBrowserAuthorization(s)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := authorization.Issue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := authorization.Authorize(ctx, ticket.Token)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := authorization.Authenticate(ctx, result.Token)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func TestConnectionCoordinatorFlow(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	core := connectionCore(t, s)
	browser, other := coordinatorBrowser(t, s), coordinatorBrowser(t, s)
	r, err := core.Create(ctx, coreInput(0))
	if err != nil {
		t.Fatal(err)
	}
	input := app.ConnectionExecutionInput{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 1000)}
	claimed, err := core.ClaimExecution(ctx, browser, input)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := core.Execution(ctx, other, r.ID)
	if err != nil || !reflect.DeepEqual(observed, claimed) {
		t.Fatal("other authorized page could not observe progress", observed, err)
	}
	if _, err := core.TouchExecution(ctx, other, input); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("other browser obtained ownership", err)
	}
	callID := fmt.Sprintf("%032x", 5000)
	permit, err := core.BeginCall(ctx, browser, input, callID, "")
	if err != nil || !permit.Invoke {
		t.Fatal(permit, err)
	}
	if replay, err := core.BeginCall(ctx, browser, input, callID, ""); err != nil || replay.Invoke {
		t.Fatal(replay, err)
	}
	if _, err := core.CompleteExecution(ctx, browser, input, coreBinding(r.ChainID)); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("wallet handoff granted consent", err)
	}
	if _, err := core.ReturnCall(ctx, browser, input, callID); err != nil {
		t.Fatal(err)
	}
	if _, err := core.InterruptExecution(ctx, browser, input); err != nil {
		t.Fatal(err)
	}
	previous := input.ID
	input.ID = fmt.Sprintf("%032x", 1001)
	if _, err := core.RecoverExecution(ctx, browser, previous, input); err != nil {
		t.Fatal(err)
	}
	old := input
	old.ID = previous
	if _, err := core.ReturnCall(ctx, browser, old, callID); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal(err)
	}
	if _, err := core.TouchExecution(ctx, browser, input); err != nil {
		t.Fatal(err)
	}
	binding := coreBinding("0x00" + strings.ToUpper(r.ChainID[2:]))
	complete, err := core.CompleteExecution(ctx, browser, input, binding)
	if err != nil || complete.State != app.ConnectionSucceeded {
		t.Fatal(complete, err)
	}
	session, err := core.CloseSession(ctx, complete.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := core.CompleteExecution(ctx, browser, input, binding)
	if err != nil || !reflect.DeepEqual(replay, complete) {
		t.Fatal(replay, err)
	}
	assertSessionUnchanged(t, s, session)
	if session.Account != strings.ToLower(binding.Account) || session.ChainID != r.ChainID {
		t.Fatal("binding not normalized", session)
	}
	if _, err := core.RejectExecution(ctx, browser, input); !errors.Is(err, app.ErrConnectionConflict) {
		t.Fatal(err)
	}
	if err := s.RevokeBrowserGrant(ctx, browser.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := core.CompleteExecution(ctx, browser, input, binding); !errors.Is(err, app.ErrInvalidBrowserCredential) {
		t.Fatal("stale authenticated subject bypassed revocation", err)
	}
	// Rejection is also exposed only through a claimed, authenticated execution.
	r, err = core.Create(ctx, coreInput(1))
	if err != nil {
		t.Fatal(err)
	}
	input = app.ConnectionExecutionInput{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 1002)}
	if _, err := core.ClaimExecution(ctx, other, input); err != nil {
		t.Fatal(err)
	}
	if rejected, err := core.RejectExecution(ctx, other, input); err != nil || rejected.State != app.ConnectionRejected {
		t.Fatal(rejected, err)
	}
}

func TestConnectionCoordinatorConcurrentConsent(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	core := connectionCore(t, s)
	browser := coordinatorBrowser(t, s)
	r, err := core.Create(ctx, coreInput(0))
	if err != nil {
		t.Fatal(err)
	}
	input := app.ConnectionExecutionInput{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 1000)}
	if _, err := core.ClaimExecution(ctx, browser, input); err != nil {
		t.Fatal(err)
	}
	barrier := &coreReadBarrier{ConnectionServiceStore: s, release: make(chan struct{})}
	barrier.ready.Add(2)
	core = connectionCore(t, barrier)
	type outcome struct {
		r   app.ConnectionRequest
		err error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			r, err := core.CompleteExecution(ctx, browser, input, coreBinding(r.ChainID))
			results <- outcome{r, err}
		}()
	}
	barrier.ready.Wait()
	close(barrier.release)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.r.SessionID == "" || !reflect.DeepEqual(first.r, second.r) {
		t.Fatal(first, second)
	}
	assertSessionCount(t, s, r.ID, 1)
}

func TestConnectionCoordinatorMaintenanceAtomicity(t *testing.T) {
	ctx := context.Background()
	for _, restart := range []bool{false, true} {
		for _, failure := range []string{"none", "connection", "session", "execution", "commit"} {
			t.Run(fmt.Sprint(restart, "/", failure), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				now := time.Now().UTC().Truncate(time.Millisecond)
				due := connectionFixture(0)
				due.CreatedAt = now.Add(-10 * time.Minute)
				due.ExpiresAt = due.CreatedAt.Add(app.ConnectionRequestLifetime)
				if err := s.CreateConnectionRequest(ctx, due); err != nil {
					t.Fatal(err)
				}
				idle := connectionFixture(1)
				idle.CreatedAt, idle.ExpiresAt = due.CreatedAt, due.ExpiresAt
				if err := s.CreateConnectionRequest(ctx, idle); err != nil {
					t.Fatal(err)
				}
				completion := completionFixture(idle)
				if _, err := s.CompleteConnectionRequest(ctx, idle.ID, completion, idle.CreatedAt.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				session, err := s.GetSession(ctx, completion.SessionID)
				if err != nil {
					t.Fatal(err)
				}
				live := connectionFixture(2)
				live.CreatedAt = now.Add(-2 * time.Minute)
				live.ExpiresAt = live.CreatedAt.Add(app.ConnectionRequestLifetime)
				if err := s.CreateConnectionRequest(ctx, live); err != nil {
					t.Fatal(err)
				}
				grant := app.BrowserGrant{ID: fmt.Sprintf("%032x", 500), IssuedAt: now.Add(-time.Hour)}
				grant.ExpiresAt = grant.IssuedAt.Add(app.BrowserGrantLifetime)
				if err := s.CreateBrowserGrant(ctx, grant, [32]byte{1}); err != nil {
					t.Fatal(err)
				}
				ref := app.ConnectionExecutionRef{ConnectionID: live.ID, ID: fmt.Sprintf("%032x", 1000), BrowserGrantID: grant.ID}
				original := claimExecution(t, s, ref, live.CreatedAt.Add(time.Second))
				if failure != "none" {
					table := "connection_requests"
					switch failure {
					case "session":
						table = "sessions"
					case "execution", "commit":
						table = "connection_executions"
					}
					trigger := "CREATE TRIGGER fail_coordination BEFORE UPDATE ON " + table + " BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
					if failure == "commit" {
						trigger = `CREATE TABLE coordination_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_coordination AFTER UPDATE ON connection_executions BEGIN INSERT INTO coordination_guard VALUES ('missing'); END`
					}
					if _, err := s.db.Exec(trigger); err != nil {
						t.Fatal(err)
					}
				}
				core := connectionCore(t, s)
				var counts app.ConnectionMaintenanceCounts
				if restart {
					counts, err = core.Restore(ctx)
				} else {
					counts, err = core.Maintain(ctx)
				}
				if failure != "none" {
					if err == nil || counts != (app.ConnectionMaintenanceCounts{}) {
						t.Fatal(counts, err)
					}
					assertConnectionUnchanged(t, s, due)
					assertSessionUnchanged(t, s, session)
					assertExecution(t, s, original)
					return
				}
				if err != nil || counts.ConnectionRequests != 1 || counts.Sessions != 1 || counts.Executions != 1 {
					t.Fatal(counts, err)
				}
				current, err := s.GetConnectionExecution(ctx, live.ID)
				if err != nil || current.Phase != app.ExecutionInterruptedReady || !current.LastSeenAt.Equal(original.LastSeenAt) {
					t.Fatal(current, err)
				}
				assertConnectionUnchanged(t, s, live)
			})
		}
	}
}

func TestConnectionCoordinatorCancelRollback(t *testing.T) {
	ctx := context.Background()
	for _, calling := range []bool{false, true} {
		for _, stage := range []string{"write", "commit", "none"} {
			t.Run(fmt.Sprint(calling, "/", stage), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				core := connectionCore(t, s)
				browser := coordinatorBrowser(t, s)
				r, err := core.Create(ctx, coreInput(0))
				if err != nil {
					t.Fatal(err)
				}
				input := app.ConnectionExecutionInput{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 1000)}
				if _, err := core.ClaimExecution(ctx, browser, input); err != nil {
					t.Fatal(err)
				}
				if calling {
					if _, err := core.BeginCall(ctx, browser, input, fmt.Sprintf("%032x", 5000), ""); err != nil {
						t.Fatal(err)
					}
				}
				original, err := s.GetConnectionExecution(ctx, r.ID)
				if err != nil {
					t.Fatal(err)
				}
				if stage != "none" {
					trigger := "CREATE TRIGGER fail_cancel BEFORE UPDATE ON connection_executions BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
					if stage == "commit" {
						trigger = `CREATE TABLE coordination_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_cancel AFTER UPDATE ON connection_executions BEGIN INSERT INTO coordination_guard VALUES ('missing'); END`
					}
					if _, err := s.db.Exec(trigger); err != nil {
						t.Fatal(err)
					}
				}
				got, err := core.Cancel(ctx, r.ID)
				if stage != "none" {
					if err == nil || got.ID != "" {
						t.Fatal(got, err)
					}
					assertConnectionUnchanged(t, s, r)
					assertExecution(t, s, original)
					return
				}
				current, readErr := s.GetConnectionExecution(ctx, r.ID)
				phase := app.ExecutionFinished
				if calling {
					phase = app.ExecutionInterruptedCalling
				}
				if err != nil || got.State != app.ConnectionCanceled || readErr != nil || current.Phase != phase {
					t.Fatal(got, current, err, readErr)
				}
				if _, err := core.CompleteExecution(ctx, browser, input, coreBinding(r.ChainID)); err == nil {
					t.Fatal("cancellation regained consent")
				}
				assertSessionCount(t, s, r.ID, 0)
			})
		}
	}
}
