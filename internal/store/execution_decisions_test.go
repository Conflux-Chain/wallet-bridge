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

func TestConnectionGuardedDecisionReplay(t *testing.T) {
	ctx := context.Background()
	for _, approve := range []bool{false, true} {
		t.Run(fmt.Sprint(approve), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			r, ref, now := executionFixture(t, s, 0)
			claimExecution(t, s, ref, now)
			call := startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5000), "", now)
			if _, err := s.ReturnConnectionCall(ctx, call, now); err != nil {
				t.Fatal(err)
			}
			completion := completionFixture(r)
			decide := func(at time.Time) (app.ConnectionRequest, error) {
				if approve {
					return s.CompleteConnectionExecution(ctx, ref, completion, at)
				}
				return s.RejectConnectionExecution(ctx, ref, at)
			}
			first, err := decide(now)
			want := app.ConnectionRejected
			if approve {
				want = app.ConnectionSucceeded
			}
			if err != nil || first.State != want || first.FinishedAt == nil || !first.FinishedAt.Equal(now) {
				t.Fatal(first, err)
			}
			execution, err := s.GetConnectionExecution(ctx, r.ID)
			if err != nil || execution.Phase != app.ExecutionFinished || execution.CallID != call.CallID {
				t.Fatal(execution, err)
			}
			var session app.Session
			if approve {
				session, err = s.CloseSession(ctx, first.SessionID, now.Add(time.Second))
				if err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			s = openTestStore(t, dir)
			got, err := decide(r.ExpiresAt.Add(time.Hour))
			if err != nil || !reflect.DeepEqual(got, first) {
				t.Fatal("terminal replay did not preserve original outcome", got, err)
			}
			if approve {
				assertSessionUnchanged(t, s, session)
				for _, change := range []func(*app.ConnectionCompletion){
					func(c *app.ConnectionCompletion) { c.SessionID = fmt.Sprintf("%032x", 9000) },
					func(c *app.ConnectionCompletion) { c.Account = "0x1111111111111111111111111111111111111111" },
					func(c *app.ConnectionCompletion) { c.Wallet.Name += " other" },
					func(c *app.ConnectionCompletion) { c.Wallet.RDNS += ".other" },
					func(c *app.ConnectionCompletion) { c.ChainID = "0x1" },
				} {
					candidate := completion
					change(&candidate)
					if _, err := s.CompleteConnectionExecution(ctx, ref, candidate, now); !errors.Is(err, app.ErrConnectionConflict) {
						t.Fatal("conflicting binding replay accepted", err)
					}
				}
				if _, err := s.RejectConnectionExecution(ctx, ref, now); !errors.Is(err, app.ErrConnectionConflict) {
					t.Fatal(err)
				}
			} else {
				if _, err := s.CompleteConnectionExecution(ctx, ref, completion, now); !errors.Is(err, app.ErrConnectionConflict) {
					t.Fatal(err)
				}
			}
			other := ref
			other.ID = fmt.Sprintf("%032x", 9000)
			if _, err := s.CompleteConnectionExecution(ctx, other, completion, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
				t.Fatal("other execution replayed success", err)
			}
			if _, err := s.RejectConnectionExecution(ctx, other, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
				t.Fatal("other execution replayed rejection", err)
			}
			if permit, err := s.BeginConnectionCall(ctx, call, "", now); err != nil || permit.Invoke || permit.Execution.Phase != app.ExecutionFinished {
				t.Fatal(permit, err)
			}
			if _, err := s.ReturnConnectionCall(ctx, call, now); err != nil {
				t.Fatal(err)
			}
			assertExecution(t, s, execution)
			assertConnectionUnchanged(t, s, first)
		})
	}
}

func TestConnectionGuardedDecisionChecks(t *testing.T) {
	ctx := context.Background()
	for _, approve := range []bool{false, true} {
		for _, reason := range []string{"unclaimed", "wrong owner", "wrong browser", "calling", "interrupted", "recovered", "expiry", "expiry calling", "canceled", "revoked", "grant expiry", "invalid time", "clock rollback", "context", "invalid binding", "wrong chain"} {
			if !approve && (reason == "invalid binding" || reason == "wrong chain") {
				continue
			}
			t.Run(fmt.Sprint(approve, "/", reason), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				r, ref, now := executionFixture(t, s, 0)
				at := now
				if reason != "unclaimed" {
					claimExecution(t, s, ref, now)
				}
				completion := completionFixture(r)
				candidate := ref
				callctx := ctx
				want := app.ErrConnectionExecutionConflict
				switch reason {
				case "unclaimed":
					want = app.ErrConnectionExecutionNotFound
				case "wrong owner":
					candidate.ID = fmt.Sprintf("%032x", 9000)
				case "wrong browser":
					_, other, _ := executionFixture(t, s, 1)
					candidate.BrowserGrantID = other.BrowserGrantID
				case "calling", "expiry calling":
					startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5000), "", now)
					if reason == "expiry calling" {
						at = r.ExpiresAt
						want = app.ErrConnectionConflict
					}
				case "interrupted", "recovered":
					if _, err := s.InterruptConnectionExecution(ctx, ref, now); err != nil {
						t.Fatal(err)
					}
					if reason == "recovered" {
						next := ref
						next.ID = fmt.Sprintf("%032x", 9000)
						if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, now); err != nil {
							t.Fatal(err)
						}
					}
				case "expiry":
					at = r.ExpiresAt
					want = app.ErrConnectionConflict
				case "canceled":
					if _, err := s.FinishConnectionRequest(ctx, r.ID, app.ConnectionCanceled, now); err != nil {
						t.Fatal(err)
					}
					want = app.ErrConnectionConflict
				case "revoked":
					if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, now); err != nil {
						t.Fatal(err)
					}
					want = app.ErrInvalidBrowserCredential
				case "grant expiry":
					if _, err := s.db.Exec("UPDATE browser_grants SET issued_at=?, expires_at=? WHERE id=?", now.Add(-app.BrowserGrantLifetime).UnixMilli(), now.UnixMilli(), ref.BrowserGrantID); err != nil {
						t.Fatal(err)
					}
					want = app.ErrInvalidBrowserCredential
				case "invalid time":
					at = time.Time{}
					want = app.ErrInvalidConnectionExecution
				case "clock rollback":
					at = r.CreatedAt.Add(-time.Millisecond)
					want = app.ErrInvalidConnectionRequest
				case "context":
					var cancel context.CancelFunc
					callctx, cancel = context.WithCancel(ctx)
					cancel()
					want = context.Canceled
				case "invalid binding":
					completion.Account = "bad"
					want = app.ErrInvalidSession
				case "wrong chain":
					completion.ChainID = "0x123456"
					want = app.ErrInvalidSession
				}
				var got app.ConnectionRequest
				var err error
				if approve {
					got, err = s.CompleteConnectionExecution(callctx, candidate, completion, at)
				} else {
					got, err = s.RejectConnectionExecution(callctx, candidate, at)
				}
				if !errors.Is(err, want) {
					t.Fatal("unexpected guarded decision", got, err, want)
				}
				assertSessionCount(t, s, r.ID, 0)
				if reason == "expiry" || reason == "expiry calling" {
					if got.State != app.ConnectionExpired || got.FinishedAt == nil || !got.FinishedAt.Equal(r.ExpiresAt) {
						t.Fatal("deadline not durably normalized", got)
					}
					current, err := s.GetConnectionExecution(ctx, r.ID)
					phase := app.ExecutionFinished
					if reason == "expiry calling" {
						phase = app.ExecutionInterruptedCalling
					}
					if err != nil || current.Phase != phase {
						t.Fatal("expiry released outstanding call", current, err)
					}
				}
			})
		}
	}
}

func TestConnectionGuardedDecisionRollback(t *testing.T) {
	ctx := context.Background()
	for _, approve := range []bool{false, true} {
		for _, stage := range []string{"connection", "session", "execution", "commit", "session collision"} {
			if !approve && (stage == "session" || stage == "session collision") {
				continue
			}
			t.Run(fmt.Sprint(approve, "/", stage), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				r, ref, now := executionFixture(t, s, 0)
				original := claimExecution(t, s, ref, now)
				completion := completionFixture(r)
				table, event := "connection_requests", "UPDATE"
				switch stage {
				case "session":
					table, event = "sessions", "INSERT"
				case "execution", "commit":
					table = "connection_executions"
				case "session collision":
					_, session := createLiveSession(t, s, 1)
					completion.SessionID = session.ID
				}
				if stage != "session collision" {
					trigger := "CREATE TRIGGER fail_decision BEFORE " + event + " ON " + table + " BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
					if stage == "commit" {
						trigger = `CREATE TABLE decision_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_decision AFTER UPDATE ON connection_executions BEGIN INSERT INTO decision_guard VALUES ('missing'); END`
					}
					if _, err := s.db.Exec(trigger); err != nil {
						t.Fatal(err)
					}
				}
				var got app.ConnectionRequest
				var err error
				if approve {
					got, err = s.CompleteConnectionExecution(ctx, ref, completion, now)
				} else {
					got, err = s.RejectConnectionExecution(ctx, ref, now)
				}
				if err == nil || got.ID != "" {
					t.Fatal("failed transaction returned a committed outcome", got, err)
				}
				if stage == "session collision" && !errors.Is(err, app.ErrSessionExists) {
					t.Fatal(err)
				}
				assertExecution(t, s, original)
				assertConnectionUnchanged(t, s, r)
				assertSessionCount(t, s, r.ID, 0)
			})
		}
	}
	// Deadline normalization and interruption must also roll back at commit.
	t.Run("expiry commit", func(t *testing.T) {
		s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
		r, ref, now := executionFixture(t, s, 0)
		claimExecution(t, s, ref, now)
		startConnectionCall(t, s, ref, fmt.Sprintf("%032x", 5000), "", now)
		original, err := s.GetConnectionExecution(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`CREATE TABLE decision_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_decision AFTER UPDATE ON connection_requests BEGIN INSERT INTO decision_guard VALUES ('missing'); END`); err != nil {
			t.Fatal(err)
		}
		if got, err := s.CompleteConnectionExecution(ctx, ref, completionFixture(r), r.ExpiresAt); err == nil || got.ID != "" {
			t.Fatal(got, err)
		}
		assertExecution(t, s, original)
		assertConnectionUnchanged(t, s, r)
		assertSessionCount(t, s, r.ID, 0)
	})
}

func TestConnectionGuardedDecisionCompetition(t *testing.T) {
	ctx := context.Background()
	for _, competitor := range []string{"complete", "reject", "cancel", "call", "return", "revoke", "recover"} {
		t.Run(competitor, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			stores := []*Store{openTestStore(t, dir), openTestStore(t, dir)}
			r, ref, now := executionFixture(t, stores[0], 0)
			claimExecution(t, stores[0], ref, now)
			call := app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", 5000)}
			if competitor == "return" {
				startConnectionCall(t, stores[0], ref, call.CallID, "", now)
			}
			if competitor == "recover" {
				if _, err := stores[0].InterruptConnectionExecution(ctx, ref, now); err != nil {
					t.Fatal(err)
				}
			}
			type outcome struct {
				err    error
				invoke bool
			}
			results := make(chan outcome, 2)
			start := make(chan struct{})
			var group sync.WaitGroup
			group.Go(func() {
				<-start
				_, err := stores[0].CompleteConnectionExecution(ctx, ref, completionFixture(r), now)
				results <- outcome{err: err}
			})
			group.Go(func() {
				<-start
				var err error
				invoke := false
				switch competitor {
				case "complete":
					_, err = stores[1].CompleteConnectionExecution(ctx, ref, completionFixture(r), now)
				case "reject":
					_, err = stores[1].RejectConnectionExecution(ctx, ref, now)
				case "cancel":
					_, err = stores[1].FinishConnectionRequest(ctx, r.ID, app.ConnectionCanceled, now)
				case "call":
					var permit app.ConnectionCallPermit
					permit, err = stores[1].BeginConnectionCall(ctx, call, "", now)
					invoke = permit.Invoke
				case "return":
					_, err = stores[1].ReturnConnectionCall(ctx, call, now)
				case "revoke":
					err = stores[1].RevokeBrowserGrant(ctx, ref.BrowserGrantID, now)
				case "recover":
					next := ref
					next.ID = fmt.Sprintf("%032x", 9000)
					_, err = stores[1].RecoverConnectionExecution(ctx, ref.ID, next, now)
				}
				results <- outcome{err, invoke}
			})
			close(start)
			group.Wait()
			close(results)
			invocations := 0
			for result := range results {
				if result.err != nil && !errors.Is(result.err, app.ErrConnectionConflict) && !errors.Is(result.err, app.ErrConnectionExecutionConflict) && !errors.Is(result.err, app.ErrInvalidBrowserCredential) {
					t.Fatal(result.err)
				}
				if competitor == "complete" && result.err != nil {
					t.Fatal(result.err)
				}
				if result.invoke {
					invocations++
				}
			}
			got, err := stores[0].GetConnectionRequest(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			execution, err := stores[0].GetConnectionExecution(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			if got.State == app.ConnectionSucceeded {
				count = 1
				if execution.Phase != app.ExecutionFinished || invocations != 0 {
					t.Fatal("completion coexisted with a permitted call", got, execution, invocations)
				}
			}
			assertSessionCount(t, stores[0], r.ID, count)
			if competitor == "complete" && count != 1 {
				t.Fatal("concurrent completion lost success", got)
			}
			if competitor == "reject" && got.State != app.ConnectionSucceeded && got.State != app.ConnectionRejected {
				t.Fatal("neither approval nor rejection committed", got)
			}
			if competitor == "cancel" && got.State != app.ConnectionSucceeded && got.State != app.ConnectionCanceled {
				t.Fatal("neither approval nor cancellation committed", got)
			}
			if competitor == "return" && count == 0 && (got.State != app.ConnectionPending || execution.Phase != app.ExecutionReady) {
				t.Fatal("call return did not preserve an unapproved connection", got, execution)
			}
			if competitor == "recover" && (count != 0 || execution.ID == ref.ID) {
				t.Fatal("old execution completed after recovery", got, execution)
			}
			if competitor == "call" && count == 0 && (invocations != 1 || execution.Phase != app.ExecutionCalling) {
				t.Fatal("neither call nor completion won", got, execution)
			}
		})
	}
}
