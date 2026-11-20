//go:build windows || darwin

package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func startConnectionCall(t *testing.T, s *Store, ref app.ConnectionExecutionRef, id, previous string, now time.Time) app.ConnectionCallRef {
	t.Helper()
	call := app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: id}
	permit, err := s.BeginConnectionCall(context.Background(), call, previous, now)
	if err != nil || !permit.Invoke || permit.Execution.Phase != app.ExecutionCalling || permit.Execution.CallID != id {
		t.Fatal("new handoff did not grant one invocation", permit, err)
	}
	return call
}

func TestConnectionWalletHandoffReplay(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	r, ref, now := executionFixture(t, s, 0)
	claimExecution(t, s, ref, now)
	call := startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5000), "", now)
	for _, at := range []time.Time{now, now.Add(time.Second)} {
		permit, err := s.BeginConnectionCall(ctx, call, "", at)
		if err != nil || permit.Invoke || !permit.Execution.LastSeenAt.Equal(now) {
			t.Fatal("handoff retry renewed liveness or granted a second invocation", permit, err)
		}
	}
	returned, err := s.ReturnConnectionCall(ctx, call, now.Add(time.Second))
	if err != nil || returned.Phase != app.ExecutionReady || returned.CallID != call.CallID || !returned.LastSeenAt.Equal(now) {
		t.Fatal("call return did not preserve execution and call identity", returned, err)
	}
	if got, err := s.ReturnConnectionCall(ctx, call, now.Add(2*time.Second)); err != nil || !reflect.DeepEqual(got, returned) {
		t.Fatal("call return replay changed execution", got, err)
	}
	s.Close()
	s = openTestStore(t, dir)
	if permit, err := s.BeginConnectionCall(ctx, call, "", now); err != nil || permit.Invoke {
		t.Fatal("reopened store reissued invocation", permit, err)
	}
	next := startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5001), call.CallID, now.Add(3*time.Second))
	if _, err := s.ReturnConnectionCall(ctx, call, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("late return ended the next call", err)
	}
	if _, err := s.ReturnConnectionCall(ctx, next, now); err != nil {
		t.Fatal(err)
	}
	// Even after the next call returns, the old handoff cannot become new work.
	if permit, err := s.BeginConnectionCall(ctx, call, "", now); !errors.Is(err, app.ErrConnectionExecutionConflict) || permit.Invoke {
		t.Fatal("delayed old handoff obtained a new invocation", permit, err)
	}
	assertConnectionUnchanged(t, s, r)
	assertSessionCount(t, s, r.ID, 0)
}

func TestConnectionWalletHandoffInterruptions(t *testing.T) {
	ctx := context.Background()
	for _, reason := range []string{"page", "heartbeat", "restart", "cancel", "expiry", "revoked", "grant expiry", "manual recovery"} {
		t.Run(reason, func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			r, ref, now := executionFixture(t, s, 0)
			claimExecution(t, s, ref, now)
			call := startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5000), "", now)
			at, phase := now, app.ExecutionInterruptedReady
			switch reason {
			case "page":
				if _, err := s.InterruptConnectionExecution(ctx, ref, at); err != nil {
					t.Fatal(err)
				}
			case "heartbeat":
				at = now.Add(app.ConnectionExecutionHeartbeatTimeout)
			case "restart":
				if _, err := s.InterruptAllConnectionExecutions(ctx, at); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if _, err := s.FinishConnectionRequest(ctx, r.ID, app.ConnectionCanceled, at); err != nil {
					t.Fatal(err)
				}
				phase = app.ExecutionFinished
			case "expiry":
				at, phase = r.ExpiresAt, app.ExecutionFinished
			case "revoked":
				if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, at); err != nil {
					t.Fatal(err)
				}
			case "grant expiry":
				if _, err := s.db.Exec("UPDATE browser_grants SET issued_at=?, expires_at=? WHERE id=?", at.Add(-app.BrowserGrantLifetime).UnixMilli(), at.UnixMilli(), ref.BrowserGrantID); err != nil {
					t.Fatal(err)
				}
			case "manual recovery":
				if _, err := s.InterruptConnectionExecution(ctx, ref, at); err != nil {
					t.Fatal(err)
				}
				next := ref
				next.ID = fmt.Sprintf("%032x", 9000)
				if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, at); err != nil {
					t.Fatal(err)
				}
				newCall := startConnectionCall(t, s, next, fmt.Sprintf("%032x", 5001), "", at)
				if _, err := s.ReturnConnectionCall(ctx, call, at); !errors.Is(err, app.ErrConnectionExecutionConflict) {
					t.Fatal("old execution returned a new execution's call", err)
				}
				if permit, err := s.BeginConnectionCall(ctx, call, "", at); !errors.Is(err, app.ErrConnectionExecutionConflict) || permit.Invoke {
					t.Fatal("old execution obtained invocation permission", permit, err)
				}
				current, err := s.GetConnectionExecution(ctx, r.ID)
				if err != nil || current.ConnectionExecutionRef != next || current.CallID != newCall.CallID || current.Phase != app.ExecutionCalling {
					t.Fatal(current, err)
				}
				return
			}
			if reason == "revoked" || reason == "grant expiry" {
				if _, err := s.ReturnConnectionCall(ctx, call, at); !errors.Is(err, app.ErrInvalidBrowserCredential) {
					t.Fatal("invalid browser authorization returned a wallet call", err)
				}
				_, next, _ := executionFixture(t, s, 1)
				next.ConnectionID = ref.ConnectionID
				if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, at); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ReturnConnectionCall(ctx, call, at); !errors.Is(err, app.ErrInvalidBrowserCredential) {
					t.Fatal(err)
				}
				current, err := s.GetConnectionExecution(ctx, r.ID)
				if err != nil || current.ConnectionExecutionRef != next || current.Phase != app.ExecutionReady {
					t.Fatal(current, err)
				}
			} else {
				// Replaying a lost handoff response never grants another invocation.
				permit, err := s.BeginConnectionCall(ctx, call, "", at)
				if err != nil || permit.Invoke || permit.Execution.Phase != app.ExecutionInterruptedCalling {
					t.Fatal(permit, err)
				}
				returned, err := s.ReturnConnectionCall(ctx, call, at)
				if err != nil || returned.Phase != phase || !returned.LastSeenAt.Equal(now) {
					t.Fatal(returned, err)
				}
				if got, err := s.ReturnConnectionCall(ctx, call, at); err != nil || !reflect.DeepEqual(got, returned) {
					t.Fatal(got, err)
				}
				if _, err := s.CompleteConnectionExecution(ctx, ref, completionFixture(r), at); err == nil {
					t.Fatal("call return restored consent")
				}
			}
			assertSessionCount(t, s, r.ID, 0)
		})
	}
}

func TestConnectionWalletHandoffInputs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	r, ref, now := executionFixture(t, s, 0)
	original := claimExecution(t, s, ref, now)
	call := app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", 5000)}
	for _, tc := range []struct {
		name     string
		change   func(*app.ConnectionCallRef)
		previous string
		at       time.Time
		want     error
	}{
		{"bad call", func(c *app.ConnectionCallRef) { c.CallID = "bad" }, "", now, app.ErrInvalidConnectionExecution},
		{"bad predecessor", func(c *app.ConnectionCallRef) {}, "bad", now, app.ErrInvalidConnectionExecution},
		{"same predecessor", func(c *app.ConnectionCallRef) {}, call.CallID, now, app.ErrInvalidConnectionExecution},
		{"wrong predecessor", func(c *app.ConnectionCallRef) {}, fmt.Sprintf("%032x", 5001), now, app.ErrConnectionExecutionConflict},
		{"wrong page", func(c *app.ConnectionCallRef) { c.ID = fmt.Sprintf("%032x", 9000) }, "", now, app.ErrConnectionExecutionConflict},
		{"unknown grant", func(c *app.ConnectionCallRef) { c.BrowserGrantID = fmt.Sprintf("%032x", 9000) }, "", now, app.ErrInvalidBrowserCredential},
		{"unknown request", func(c *app.ConnectionCallRef) { c.ConnectionID = fmt.Sprintf("%032x", 9000) }, "", now, app.ErrConnectionExecutionNotFound},
		{"invalid time", func(c *app.ConnectionCallRef) {}, "", time.Time{}, app.ErrInvalidConnectionExecution},
		{"clock rollback", func(c *app.ConnectionCallRef) {}, "", r.CreatedAt.Add(-time.Millisecond), app.ErrConnectionExecutionConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := call
			tc.change(&candidate)
			if permit, err := s.BeginConnectionCall(ctx, candidate, tc.previous, tc.at); !errors.Is(err, tc.want) || permit.Invoke {
				t.Fatal(permit, err)
			}
			assertExecution(t, s, original)
		})
	}
	call = startConnectionCall(t, s, ref, call.CallID, "", now)
	for _, tc := range []struct {
		name   string
		change func(*app.ConnectionCallRef)
		at     time.Time
		want   error
	}{
		{"bad call", func(c *app.ConnectionCallRef) { c.CallID = "bad" }, now, app.ErrInvalidConnectionExecution},
		{"wrong call", func(c *app.ConnectionCallRef) { c.CallID = fmt.Sprintf("%032x", 9000) }, now, app.ErrConnectionExecutionConflict},
		{"wrong page", func(c *app.ConnectionCallRef) { c.ID = fmt.Sprintf("%032x", 9000) }, now, app.ErrConnectionExecutionConflict},
		{"clock rollback", func(c *app.ConnectionCallRef) {}, r.CreatedAt.Add(-time.Millisecond), app.ErrInvalidConnectionExecution},
	} {
		t.Run("return/"+tc.name, func(t *testing.T) {
			candidate := call
			tc.change(&candidate)
			if _, err := s.ReturnConnectionCall(ctx, candidate, tc.at); !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if permit, err := s.BeginConnectionCall(canceled, call, "", now); !errors.Is(err, context.Canceled) || permit.Invoke {
		t.Fatal(permit, err)
	}
	if _, err := s.ReturnConnectionCall(canceled, call, now); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	current, err := s.GetConnectionExecution(ctx, r.ID)
	if err != nil || current.Phase != app.ExecutionCalling {
		t.Fatal(current, err)
	}
}

func TestConnectionWalletHandoffRollback(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"begin", "return"} {
		for _, stage := range []string{"write", "commit"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				r, ref, now := executionFixture(t, s, 0)
				original := claimExecution(t, s, ref, now)
				call := app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", 5000)}
				if operation == "return" {
					startConnectionCall(t, s, ref, call.CallID, "", now)
					var err error
					original, err = s.GetConnectionExecution(ctx, r.ID)
					if err != nil {
						t.Fatal(err)
					}
				}
				trigger := "CREATE TRIGGER fail_handoff BEFORE UPDATE ON connection_executions BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
				if stage == "commit" {
					trigger = `CREATE TABLE handoff_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_handoff AFTER UPDATE ON connection_executions BEGIN INSERT INTO handoff_guard VALUES ('missing'); END`
				}
				if _, err := s.db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
				if operation == "begin" {
					if permit, err := s.BeginConnectionCall(ctx, call, "", now); err == nil || permit.Invoke || permit.Execution.ID != "" {
						t.Fatal(permit, err)
					}
				} else {
					if got, err := s.ReturnConnectionCall(ctx, call, now); err == nil || got.ID != "" {
						t.Fatal(got, err)
					}
				}
				assertExecution(t, s, original)
				assertConnectionUnchanged(t, s, r)
				assertSessionCount(t, s, r.ID, 0)
			})
		}
	}
}

func TestConnectionWalletHandoffConcurrent(t *testing.T) {
	ctx := context.Background()
	for _, same := range []bool{false, true} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			stores := []*Store{openTestStore(t, dir), openTestStore(t, dir)}
			r, ref, now := executionFixture(t, stores[0], 0)
			claimExecution(t, stores[0], ref, now)
			type outcome struct {
				permit app.ConnectionCallPermit
				err    error
			}
			results := make(chan outcome, 8)
			start := make(chan struct{})
			var group sync.WaitGroup
			for i := range 8 {
				group.Go(func() {
					<-start
					id := 5000
					if !same {
						id += i
					}
					permit, err := stores[i%2].BeginConnectionCall(ctx, app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", id)}, "", now)
					results <- outcome{permit, err}
				})
			}
			close(start)
			group.Wait()
			close(results)
			invocations := 0
			for result := range results {
				if result.err != nil && result.permit.Invoke {
					t.Fatal("failed handoff granted an invocation", result)
				}
				if result.err != nil && !errors.Is(result.err, app.ErrConnectionExecutionConflict) {
					t.Fatal(result.err)
				}
				if same && result.err != nil {
					t.Fatal(result.err)
				}
				if result.permit.Invoke {
					invocations++
				}
			}
			if invocations != 1 {
				t.Fatal("concurrent handoffs granted more than one invocation", invocations)
			}
			current, err := stores[0].GetConnectionExecution(ctx, r.ID)
			if err != nil || current.Phase != app.ExecutionCalling {
				t.Fatal(current, err)
			}
		})
	}
}
