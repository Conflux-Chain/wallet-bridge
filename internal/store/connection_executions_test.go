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

func executionFixture(t *testing.T, s *Store, index int) (app.ConnectionRequest, app.ConnectionExecutionRef, time.Time) {
	t.Helper()
	r := createPendingConnection(t, s, index)
	g, digest := fixture(index)
	if err := s.CreateBrowserGrant(context.Background(), g, digest); err != nil {
		t.Fatal(err)
	}
	return r, app.ConnectionExecutionRef{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", index+1000), BrowserGrantID: g.ID}, r.CreatedAt.Add(time.Second)
}

func claimExecution(t *testing.T, s *Store, ref app.ConnectionExecutionRef, now time.Time) app.ConnectionExecution {
	t.Helper()
	r, err := s.ClaimConnectionExecution(context.Background(), ref, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Wallet handoff production methods arrive in PR 2-3b2. SQL prepares that phase
// here so this PR can verify it is never released by deadline/liveness cleanup.
func callingExecution(t *testing.T, s *Store, ref app.ConnectionExecutionRef) {
	t.Helper()
	if _, err := s.db.Exec("UPDATE connection_executions SET phase='calling', call_id=? WHERE connection_id=?", fmt.Sprintf("%032x", 5000), ref.ConnectionID); err != nil {
		t.Fatal(err)
	}
}

func assertExecution(t *testing.T, s *Store, want app.ConnectionExecution) {
	t.Helper()
	got, err := s.GetConnectionExecution(context.Background(), want.ConnectionID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("execution changed unexpectedly", got, want, err)
	}
}

func TestConnectionExecutionClaimPersistence(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	request, ref, now := executionFixture(t, s, 0)
	first := claimExecution(t, s, ref, now)
	if first.ConnectionExecutionRef != ref || first.Phase != app.ExecutionReady || first.CallID != "" || !first.LastSeenAt.Equal(now) {
		t.Fatal("incorrect claim", first)
	}
	s.Close()
	s = openTestStore(t, dir)
	assertExecution(t, s, first) // Opening another connection is not restart recovery.
	got, err := s.ClaimConnectionExecution(ctx, ref, now.Add(time.Minute))
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatal("lost-response retry changed liveness or ownership", got, err)
	}
	other := ref
	other.ID = fmt.Sprintf("%032x", 9999)
	got, err = s.ClaimConnectionExecution(ctx, other, now)
	if !errors.Is(err, app.ErrConnectionExecutionConflict) || !reflect.DeepEqual(got, first) {
		t.Fatal("another page replaced owner", got, err)
	}
	assertConnectionUnchanged(t, s, request)
	assertSessionCount(t, s, request.ID, 0)
}

func TestConnectionExecutionHeartbeatBoundary(t *testing.T) {
	for _, calling := range []bool{false, true} {
		for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Millisecond} {
			t.Run(fmt.Sprint(calling, "/", offset), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				request, ref, now := executionFixture(t, s, 0)
				claimExecution(t, s, ref, now)
				if calling {
					callingExecution(t, s, ref)
				}
				at := now.Add(app.ConnectionExecutionHeartbeatTimeout + offset)
				got, err := s.TouchConnectionExecution(context.Background(), ref, at)
				if offset < 0 {
					if err != nil || !got.LastSeenAt.Equal(at.Truncate(time.Millisecond)) ||
						(got.Phase != app.ExecutionReady && got.Phase != app.ExecutionCalling) {
						t.Fatal("live heartbeat not accepted", got, err)
					}
					// Clock rollback cannot reduce the last accepted heartbeat.
					older, err := s.TouchConnectionExecution(context.Background(), ref, now)
					if err != nil || !reflect.DeepEqual(older, got) {
						t.Fatal("rollback changed liveness", older, err)
					}
				} else {
					phase := app.ExecutionInterruptedReady
					if calling {
						phase = app.ExecutionInterruptedCalling
					}
					if !errors.Is(err, app.ErrConnectionExecutionConflict) || got.Phase != phase || !got.LastSeenAt.Equal(now) {
						t.Fatal("stale heartbeat revived execution", got, err)
					}
					if _, err := s.TouchConnectionExecution(context.Background(), ref, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
						t.Fatal("clock rollback revived interruption", err)
					}
				}
				assertConnectionUnchanged(t, s, request)
			})
		}
	}
}

func TestConnectionExecutionExplicitRecovery(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request, ref, now := executionFixture(t, s, 0)
	first := claimExecution(t, s, ref, now)
	_, next, _ := executionFixture(t, s, 1)
	next.ConnectionID = ref.ConnectionID
	if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("recovery replaced live page", err)
	}
	if _, err := s.InterruptConnectionExecution(ctx, ref, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimConnectionExecution(ctx, next, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("claim bypassed explicit recovery", err)
	}
	if _, err := s.RecoverConnectionExecution(ctx, ref.ID, next, request.CreatedAt.Add(-time.Millisecond)); !errors.Is(err, app.ErrInvalidConnectionExecution) {
		t.Fatal("clock rollback released a future request", err)
	}
	recovered, err := s.RecoverConnectionExecution(ctx, ref.ID, next, now.Add(time.Second))
	if err != nil || recovered.ConnectionExecutionRef != next || recovered.Phase != app.ExecutionReady || recovered.CallID != "" {
		t.Fatal("explicit recovery failed", recovered, err)
	}
	got, err := s.RecoverConnectionExecution(ctx, ref.ID, next, now.Add(time.Minute))
	if err != nil || !reflect.DeepEqual(got, recovered) {
		t.Fatal("recovery response retry renewed or replaced execution", got, err)
	}
	for _, call := range []func() (app.ConnectionExecution, error){
		func() (app.ConnectionExecution, error) { return s.TouchConnectionExecution(ctx, ref, now) },
		func() (app.ConnectionExecution, error) { return s.InterruptConnectionExecution(ctx, ref, now) },
	} {
		got, err := call()
		if !errors.Is(err, app.ErrConnectionExecutionConflict) || !reflect.DeepEqual(got, recovered) {
			t.Fatal("old page affected recovered owner", got, err)
		}
	}
	assertExecution(t, s, recovered)
	assertConnectionUnchanged(t, s, request)
	assertSessionCount(t, s, request.ID, 0)
	if first.ID == recovered.ID {
		t.Fatal("recovery reused old execution ID")
	}
}

func TestConnectionExecutionMaintenanceRetainsWalletWait(t *testing.T) {
	ctx := context.Background()
	for _, reason := range []string{"heartbeat", "revocation", "grant expiry", "canceled", "request expiry", "restart"} {
		for _, calling := range []bool{false, true} {
			t.Run(fmt.Sprint(reason, "/", calling), func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				r, ref, now := executionFixture(t, s, 0)
				claimExecution(t, s, ref, now)
				if calling {
					callingExecution(t, s, ref)
				}
				at := now
				switch reason {
				case "heartbeat":
					at = now.Add(app.ConnectionExecutionHeartbeatTimeout)
				case "revocation":
					if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, now); err != nil {
						t.Fatal(err)
					}
				case "grant expiry":
					// Keep the grant's fixed lifetime valid while moving its end to now.
					if _, err := s.db.Exec("UPDATE browser_grants SET issued_at=?, expires_at=? WHERE id=?", now.Add(-app.BrowserGrantLifetime).UnixMilli(), now.UnixMilli(), ref.BrowserGrantID); err != nil {
						t.Fatal(err)
					}
				case "canceled":
					if _, err := s.FinishConnectionRequest(ctx, r.ID, app.ConnectionCanceled, now); err != nil {
						t.Fatal(err)
					}
				case "request expiry":
					at = r.ExpiresAt
				}
				maintain := s.MaintainConnectionExecutions
				if reason == "restart" {
					maintain = s.InterruptAllConnectionExecutions
				}
				count, err := maintain(ctx, at)
				wantCount := int64(1)
				if reason == "canceled" {
					wantCount = 0 // Cancellation now coordinates the execution atomically.
				}
				if err != nil || count != wantCount {
					t.Fatal("incorrect maintenance count", count, err)
				}
				got, err := s.GetConnectionExecution(ctx, r.ID)
				phase := app.ExecutionInterruptedReady
				if reason == "canceled" || reason == "request expiry" {
					phase = app.ExecutionFinished
				}
				if calling {
					phase = app.ExecutionInterruptedCalling
				}
				if err != nil || got.Phase != phase || !got.LastSeenAt.Equal(now) {
					t.Fatal("maintenance released wallet wait or renewed execution", got, err)
				}
				if count, err := maintain(ctx, at); err != nil || count != 0 {
					t.Fatal("maintenance replay changed execution", count, err)
				}
				other, next, _ := executionFixture(t, s, 1)
				other.CreatedAt, other.ExpiresAt = at, at.Add(app.ConnectionRequestLifetime)
				if _, err := s.db.Exec("UPDATE connection_requests SET created_at=?, expires_at=? WHERE id=?", other.CreatedAt.UnixMilli(), other.ExpiresAt.UnixMilli(), other.ID); err != nil {
					t.Fatal(err)
				}
				_, err = s.ClaimConnectionExecution(ctx, next, at)
				if (calling && !errors.Is(err, app.ErrConnectionExecutionBusy)) || (!calling && err != nil) {
					t.Fatal("incorrect global slot occupancy", err)
				}
			})
		}
	}
}

func TestConnectionExecutionConcurrentOwnership(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"same claim", "different requests", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			stores := []*Store{openTestStore(t, dir), openTestStore(t, dir)}
			_, ref, now := executionFixture(t, stores[0], 0)
			_, other, _ := executionFixture(t, stores[0], 1)
			if kind == "recovery" {
				claimExecution(t, stores[0], ref, now)
				if _, err := stores[0].InterruptConnectionExecution(ctx, ref, now); err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			var group sync.WaitGroup
			errorsFound := make(chan error, 8)
			for i := range 8 {
				group.Go(func() {
					<-start
					s, candidate := stores[i%2], ref
					if kind == "different requests" && i%2 == 1 {
						candidate = other
					}
					if kind != "same claim" {
						candidate.ID = fmt.Sprintf("%032x", 10000+i)
					}
					var err error
					if kind == "recovery" {
						_, err = s.RecoverConnectionExecution(ctx, ref.ID, candidate, now)
					} else {
						_, err = s.ClaimConnectionExecution(ctx, candidate, now)
					}
					errorsFound <- err
				})
			}
			close(start)
			group.Wait()
			close(errorsFound)
			successes := 0
			for err := range errorsFound {
				if err == nil {
					successes++
				} else if !errors.Is(err, app.ErrConnectionExecutionConflict) && !errors.Is(err, app.ErrConnectionExecutionBusy) {
					t.Fatal("race returned storage failure", err)
				}
			}
			want := 1
			if kind == "same claim" {
				want = 8
			}
			var count int
			if successes != want {
				t.Fatal("incorrect successful owners", successes, want)
			}
			if err := stores[0].db.QueryRow("SELECT count(*) FROM connection_executions WHERE " + executionOccupied).Scan(&count); err != nil || count != 1 {
				t.Fatal("concurrent ownership did not leave one slot", count, err)
			}
		})
	}
}

func TestConnectionExecutionRollback(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"claim", "heartbeat", "interrupt", "recover", "maintenance", "restart"} {
		for _, stage := range []string{"write", "commit"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				request, ref, now := executionFixture(t, s, 0)
				original := claimExecution(t, s, ref, now)
				_, other, _ := executionFixture(t, s, 1)
				if operation == "recover" {
					var err error
					original, err = s.InterruptConnectionExecution(ctx, ref, now)
					if err != nil {
						t.Fatal(err)
					}
				}
				event := "UPDATE"
				if operation == "claim" {
					event = "INSERT"
				}
				trigger := "CREATE TRIGGER fail_execution BEFORE " + event + " ON connection_executions BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
				if stage == "commit" {
					trigger = `CREATE TABLE execution_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_execution AFTER ` + event + ` ON connection_executions BEGIN INSERT INTO execution_guard VALUES ('missing'); END`
				}
				if _, err := s.db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
				var got app.ConnectionExecution
				var err error
				switch operation {
				case "claim":
					// Cleanup of the stale original must roll back with the failed claim.
					got, err = s.ClaimConnectionExecution(ctx, other, now.Add(app.ConnectionExecutionHeartbeatTimeout))
				case "heartbeat":
					got, err = s.TouchConnectionExecution(ctx, ref, now.Add(time.Second))
				case "interrupt":
					got, err = s.InterruptConnectionExecution(ctx, ref, now)
				case "recover":
					other.ConnectionID = ref.ConnectionID
					got, err = s.RecoverConnectionExecution(ctx, ref.ID, other, now)
				case "maintenance", "restart":
					call := s.MaintainConnectionExecutions
					if operation == "restart" {
						call = s.InterruptAllConnectionExecutions
					}
					var count int64
					count, err = call(ctx, now.Add(app.ConnectionExecutionHeartbeatTimeout))
					if count != 0 {
						t.Fatal("failed maintenance returned a committed count", count)
					}
				}
				if err == nil || got.ID != "" {
					t.Fatal("failed transaction returned a committed execution", got, err)
				}
				assertExecution(t, s, original)
				assertConnectionUnchanged(t, s, request)
				assertSessionCount(t, s, request.ID, 0)
			})
		}
	}
}

func TestConnectionExecutionAuthorizationAndInputs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, ref, now := executionFixture(t, s, 0)
	original := claimExecution(t, s, ref, now)
	_, other, _ := executionFixture(t, s, 1)
	for _, call := range []func(context.Context, app.ConnectionExecutionRef, time.Time) (app.ConnectionExecution, error){
		s.ClaimConnectionExecution, s.TouchConnectionExecution, s.InterruptConnectionExecution,
		func(ctx context.Context, ref app.ConnectionExecutionRef, at time.Time) (app.ConnectionExecution, error) {
			return s.RecoverConnectionExecution(ctx, original.ID, ref, at)
		},
	} {
		candidate := ref
		candidate.ID = fmt.Sprintf("%032x", 9000)
		candidate.BrowserGrantID = fmt.Sprintf("%032x", 999)
		if _, err := call(ctx, candidate, now); !errors.Is(err, app.ErrInvalidBrowserCredential) {
			t.Fatal("unauthenticated grant accepted", err)
		}
		candidate.BrowserGrantID = other.BrowserGrantID
		if _, err := call(ctx, candidate, now); !errors.Is(err, app.ErrConnectionExecutionConflict) {
			t.Fatal("other browser acquired live ownership", err)
		}
		if _, err := call(ctx, candidate, time.Time{}); !errors.Is(err, app.ErrInvalidConnectionExecution) {
			t.Fatal("invalid time accepted", err)
		}
		candidate.ID = "invalid"
		if _, err := call(ctx, candidate, now); !errors.Is(err, app.ErrInvalidConnectionExecution) {
			t.Fatal("invalid ID accepted", err)
		}
		candidate.ID = fmt.Sprintf("%032x", 9000)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := call(canceled, candidate, now); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled write accepted", err)
		}
		assertExecution(t, s, original)
	}
	if err := s.RevokeBrowserGrant(ctx, ref.BrowserGrantID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TouchConnectionExecution(ctx, ref, now); !errors.Is(err, app.ErrInvalidBrowserCredential) {
		t.Fatal("revoked grant renewed execution", err)
	}
	assertExecution(t, s, original) // Failed authentication does not mutate records.
}

func TestConnectionExecutionIDConflicts(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, ref, now := executionFixture(t, s, 0)
	claimExecution(t, s, ref, now)
	original, err := s.InterruptConnectionExecution(ctx, ref, now)
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := executionFixture(t, s, 1)
	collision := other
	collision.ID = ref.ID
	if _, err := s.ClaimConnectionExecution(ctx, collision, now.Add(time.Second)); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("execution ID collision accepted", err)
	}
	otherResult := claimExecution(t, s, other, now.Add(time.Second))
	callingExecution(t, s, other)
	otherResult, err = s.InterruptConnectionExecution(ctx, other, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	next := other
	next.ID = ref.ID
	if _, err := s.RecoverConnectionExecution(ctx, other.ID, next, now.Add(time.Second)); !errors.Is(err, app.ErrConnectionExecutionConflict) {
		t.Fatal("recovery overwrote another record's ID", err)
	}
	assertExecution(t, s, original)
	assertExecution(t, s, otherResult)
}

func TestConnectionExecutionTerminalRecovery(t *testing.T) {
	ctx := context.Background()
	for _, state := range []app.ConnectionState{app.ConnectionCanceled, app.ConnectionRejected, app.ConnectionExpired, app.ConnectionSucceeded} {
		t.Run(string(state), func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			r, ref, now := executionFixture(t, s, 0)
			claimExecution(t, s, ref, now)
			callingExecution(t, s, ref)
			at := now
			var err error
			if state == app.ConnectionSucceeded {
				r, err = s.CompleteConnectionRequest(ctx, r.ID, completionFixture(r), now)
			} else {
				if state == app.ConnectionExpired {
					at = r.ExpiresAt
				}
				r, err = s.FinishConnectionRequest(ctx, r.ID, state, at)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, next, _ := executionFixture(t, s, 1)
			next.ConnectionID = ref.ConnectionID
			released, err := s.RecoverConnectionExecution(ctx, ref.ID, next, at)
			if err != nil || released.Phase != app.ExecutionFinished || released.ID != ref.ID {
				t.Fatal("terminal recovery created an execution instead of releasing wait", released, err)
			}
			got, err := s.RecoverConnectionExecution(ctx, ref.ID, next, at)
			if err != nil || !reflect.DeepEqual(got, released) {
				t.Fatal("terminal recovery was not idempotent", got, err)
			}
			assertConnectionUnchanged(t, s, r)
			count := 0
			if state == app.ConnectionSucceeded {
				count = 1
			}
			assertSessionCount(t, s, r.ID, count)
		})
	}
}
