//go:build windows || darwin

package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func completionFixture(request app.ConnectionRequest) (app.ConnectionCompletion, [32]byte) {
	session, digest := sessionFixture(request)
	return app.ConnectionCompletion{
		SessionID: session.ID, Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID,
	}, digest
}

func createPendingConnection(t *testing.T, s *Store, index int) app.ConnectionRequest {
	t.Helper()
	request, digest := connectionFixture(index)
	if err := s.CreateConnectionRequest(context.Background(), request, digest); err != nil {
		t.Fatal(err)
	}
	return request
}

func assertConnectionUnchanged(t *testing.T, s *Store, want app.ConnectionRequest) {
	t.Helper()
	got, err := s.GetConnectionRequest(context.Background(), want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("connection changed unexpectedly", got, err)
	}
}

func assertSessionCount(t *testing.T, s *Store, requestID string, want int) {
	t.Helper()
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sessions WHERE connection_id = ?", requestID).Scan(&count); err != nil || count != want {
		t.Fatal("unexpected session count", count, want, err)
	}
}

func TestConnectionCompletionPersistsAndReplaysAfterClosure(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	request := createPendingConnection(t, s, 0)
	completion, digest := completionFixture(request)
	now := request.CreatedAt.Add(time.Second + 234567*time.Nanosecond)
	finished := time.UnixMilli(now.UnixMilli()).UTC()
	got, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, now)
	want := request
	want.State, want.SessionID, want.FinishedAt = app.ConnectionSucceeded, completion.SessionID, &finished
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("completion did not return committed connection", got, err)
	}
	wantSession := app.Session{
		ID: completion.SessionID, ConnectionID: request.ID, Wallet: completion.Wallet,
		Account: completion.Account, ChainID: request.ChainID, CreatedAt: finished, LastAccessAt: finished,
	}
	session, err := s.FindSession(ctx, completion.SessionID, digest)
	if err != nil || !reflect.DeepEqual(session, wantSession) {
		t.Fatal("completion lost binding, credential or decision time", session, err)
	}
	// A retry must not reset a Session's subsequent access or closure.
	accessed, closed := finished.Add(time.Minute), finished.Add(2*time.Minute)
	if _, err := s.db.Exec("UPDATE sessions SET last_access_at = ?, closed_at = ? WHERE id = ?", accessed.UnixMilli(), closed.UnixMilli(), completion.SessionID); err != nil {
		t.Fatal(err)
	}
	wantSession.LastAccessAt, wantSession.ClosedAt = accessed, &closed
	s.Close()
	s = openTestStore(t, dir)
	got, err = s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.ExpiresAt.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("replay after restart/expiry lost original success", got, err)
	}
	session, err = s.GetSession(ctx, completion.SessionID)
	if err != nil || !reflect.DeepEqual(session, wantSession) {
		t.Fatal("replay revived or refreshed Session", session, err)
	}
	assertSessionCount(t, s, request.ID, 1)
}

func TestConnectionCompletionConflictingReplaysPreserveResult(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request := createPendingConnection(t, s, 0)
	completion, digest := completionFixture(request)
	want, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*app.ConnectionCompletion, *[32]byte){
		"Session ID":  func(c *app.ConnectionCompletion, _ *[32]byte) { c.SessionID = fmt.Sprintf("%032x", 999) },
		"credential":  func(_ *app.ConnectionCompletion, d *[32]byte) { *d = sha256.Sum256([]byte("different credential")) },
		"wallet kind": func(c *app.ConnectionCompletion, _ *[32]byte) { c.Wallet.Kind = app.WalletLegacy },
		"wallet name": func(c *app.ConnectionCompletion, _ *[32]byte) { c.Wallet.Name = "Another Wallet" },
		"wallet rdns": func(c *app.ConnectionCompletion, _ *[32]byte) { c.Wallet.RDNS = "org.example.other" },
		"account": func(c *app.ConnectionCompletion, _ *[32]byte) {
			c.Account = "0x0000000000000000000000000000000000005678"
		},
		"chain": func(c *app.ConnectionCompletion, _ *[32]byte) { c.ChainID = "0x47" },
	} {
		t.Run(name, func(t *testing.T) {
			changed, changedDigest := completion, digest
			mutate(&changed, &changedDigest)
			got, err := s.CompleteConnectionRequest(ctx, request.ID, changed, changedDigest, request.ExpiresAt)
			if !errors.Is(err, app.ErrConnectionConflict) || !reflect.DeepEqual(got, want) {
				t.Fatal("conflicting replay did not return original result", got, err)
			}
		})
	}
	for _, state := range []app.ConnectionState{app.ConnectionCanceled, app.ConnectionRejected, app.ConnectionExpired} {
		got, err := s.FinishConnectionRequest(ctx, request.ID, state, request.ExpiresAt)
		if !errors.Is(err, app.ErrConnectionConflict) || !reflect.DeepEqual(got, want) {
			t.Fatal("late termination replaced success", got, err)
		}
	}
	assertConnectionUnchanged(t, s, want)
	assertSessionCount(t, s, request.ID, 1)
}

func TestConnectionFinishPreventsLateCompletion(t *testing.T) {
	ctx := context.Background()
	for _, state := range []app.ConnectionState{app.ConnectionCanceled, app.ConnectionRejected, app.ConnectionExpired} {
		t.Run(string(state), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			request := createPendingConnection(t, s, 0)
			now := request.CreatedAt.Add(time.Second)
			if state == app.ConnectionExpired {
				now = request.ExpiresAt
			}
			want := request
			want.State, want.FinishedAt = state, &now
			got, err := s.FinishConnectionRequest(ctx, request.ID, state, now)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("termination lost original deadline or decision time", got, err)
			}
			s.Close()
			s = openTestStore(t, dir)
			got, err = s.FinishConnectionRequest(ctx, request.ID, state, request.ExpiresAt.Add(time.Hour))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("termination replay changed saved outcome", got, err)
			}
			completion, digest := completionFixture(request)
			got, err = s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.ExpiresAt.Add(time.Hour))
			if !errors.Is(err, app.ErrConnectionConflict) || !reflect.DeepEqual(got, want) {
				t.Fatal("late wallet success replaced terminal result", got, err)
			}
			other := app.ConnectionCanceled
			if state == other {
				other = app.ConnectionRejected
			}
			got, err = s.FinishConnectionRequest(ctx, request.ID, other, request.ExpiresAt.Add(time.Hour))
			if !errors.Is(err, app.ErrConnectionConflict) || !reflect.DeepEqual(got, want) {
				t.Fatal("conflicting termination replaced original result", got, err)
			}
			assertConnectionUnchanged(t, s, want)
			assertSessionCount(t, s, request.ID, 0)
		})
	}
}

func TestConnectionCompletionExpiryBoundary(t *testing.T) {
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			request := createPendingConnection(t, s, 0)
			completion, digest := completionFixture(request)
			got, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.ExpiresAt.Add(offset))
			if offset < 0 {
				if err != nil || got.State != app.ConnectionSucceeded {
					t.Fatal("unexpired completion failed", got, err)
				}
				assertSessionCount(t, s, request.ID, 1)
			} else {
				if !errors.Is(err, app.ErrConnectionConflict) || got.State != app.ConnectionExpired || got.FinishedAt == nil || !got.FinishedAt.Equal(request.ExpiresAt) {
					t.Fatal("expired completion created Session or reset deadline", got, err)
				}
				assertConnectionUnchanged(t, s, got)
				assertSessionCount(t, s, request.ID, 0)
			}
		})
	}
	for _, state := range []app.ConnectionState{app.ConnectionCanceled, app.ConnectionRejected, app.ConnectionExpired} {
		t.Run("late "+string(state), func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			request := createPendingConnection(t, s, 0)
			got, err := s.FinishConnectionRequest(ctx, request.ID, state, request.ExpiresAt.Add(time.Minute))
			if (state == app.ConnectionExpired && err != nil) || (state != app.ConnectionExpired && !errors.Is(err, app.ErrConnectionConflict)) ||
				got.State != app.ConnectionExpired || got.FinishedAt == nil || !got.FinishedAt.Equal(request.ExpiresAt) {
				t.Fatal("late termination did not preserve expiry", got, err)
			}
		})
	}
}

func TestConnectionCompletionRollsBackEveryWrite(t *testing.T) {
	ctx := context.Background()
	for _, failure := range []string{"Session ID collision", "digest collision", "insert failure", "commit failure"} {
		t.Run(failure, func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			first := createPendingConnection(t, s, 0)
			completion, digest := completionFixture(first)
			original, err := s.CompleteConnectionRequest(ctx, first.ID, completion, digest, first.CreatedAt.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			request := createPendingConnection(t, s, 1)
			if failure != "Session ID collision" {
				completion.SessionID = fmt.Sprintf("%032x", 999)
			}
			if failure != "digest collision" {
				digest = sha256.Sum256([]byte("second Session credential"))
			}
			var trigger string
			switch failure {
			case "insert failure":
				trigger = "CREATE TRIGGER fail_session AFTER INSERT ON sessions BEGIN SELECT RAISE(ABORT, 'injected insert failure'); END"
			case "commit failure":
				// Deferred foreign keys fail at Commit, after both intended writes.
				trigger = `CREATE TABLE commit_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_session AFTER INSERT ON sessions BEGIN INSERT INTO commit_guard VALUES ('missing'); END`
			}
			if trigger != "" {
				if _, err := s.db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.CreatedAt.Add(time.Second))
			if err == nil || got.ID != "" {
				t.Fatal("failed transaction returned a result", got, err)
			}
			if trigger == "" && !errors.Is(err, app.ErrSessionExists) {
				t.Fatal("Session collision did not return typed error", err)
			}
			assertConnectionUnchanged(t, s, request)
			assertConnectionUnchanged(t, s, original)
			assertSessionCount(t, s, request.ID, 0)
			assertSessionCount(t, s, first.ID, 1)
		})
	}
}

func TestConnectionTransitionsRejectInvalidInputs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request := createPendingConnection(t, s, 0)
	completion, digest := completionFixture(request)
	now := request.CreatedAt.Add(time.Second)
	for name, mutate := range map[string]func(*app.ConnectionCompletion){
		"ID":          func(c *app.ConnectionCompletion) { c.SessionID = "invalid" },
		"wallet kind": func(c *app.ConnectionCompletion) { c.Wallet.Kind = "unknown" },
		"name":        func(c *app.ConnectionCompletion) { c.Wallet.Name = " \t" },
		"rdns":        func(c *app.ConnectionCompletion) { c.Wallet.RDNS = " \t" },
		"account":     func(c *app.ConnectionCompletion) { c.Account = "0x123" },
		"upper hex":   func(c *app.ConnectionCompletion) { c.Account = "0xABC0000000000000000000000000000000000000" },
		"non hex":     func(c *app.ConnectionCompletion) { c.Account = "0xzzz0000000000000000000000000000000000000" },
		"chain":       func(c *app.ConnectionCompletion) { c.ChainID = "0x047" },
		"mismatch":    func(c *app.ConnectionCompletion) { c.ChainID = "0x47" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := completion
			mutate(&changed)
			if _, err := s.CompleteConnectionRequest(ctx, request.ID, changed, digest, now); !errors.Is(err, app.ErrInvalidSession) {
				t.Fatal("invalid binding accepted", err)
			}
		})
	}
	for _, call := range []func() (app.ConnectionRequest, error){
		func() (app.ConnectionRequest, error) {
			return s.CompleteConnectionRequest(ctx, "invalid", completion, digest, now)
		},
		func() (app.ConnectionRequest, error) {
			return s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.CreatedAt.Add(-time.Millisecond))
		},
		func() (app.ConnectionRequest, error) {
			return s.CompleteConnectionRequest(ctx, request.ID, completion, digest, time.Time{})
		},
		func() (app.ConnectionRequest, error) {
			return s.FinishConnectionRequest(ctx, request.ID, app.ConnectionExpired, now)
		},
		func() (app.ConnectionRequest, error) {
			return s.FinishConnectionRequest(ctx, request.ID, app.ConnectionPending, now)
		},
		func() (app.ConnectionRequest, error) {
			return s.FinishConnectionRequest(ctx, request.ID, app.ConnectionSucceeded, now)
		},
		func() (app.ConnectionRequest, error) {
			return s.FinishConnectionRequest(ctx, request.ID, "unknown", now)
		},
	} {
		if _, err := call(); !errors.Is(err, app.ErrInvalidConnectionRequest) {
			t.Fatal("invalid transition accepted", err)
		}
	}
	missing := fmt.Sprintf("%032x", 999)
	if _, err := s.CompleteConnectionRequest(ctx, missing, completion, digest, now); !errors.Is(err, app.ErrConnectionRequestNotFound) {
		t.Fatal("missing completion did not return not-found", err)
	}
	if _, err := s.FinishConnectionRequest(ctx, missing, app.ConnectionCanceled, now); !errors.Is(err, app.ErrConnectionRequestNotFound) {
		t.Fatal("missing termination did not return not-found", err)
	}
	assertConnectionUnchanged(t, s, request)
	assertSessionCount(t, s, request.ID, 0)
	completion.Wallet = app.WalletDescriptor{Kind: app.WalletLegacy, Name: "Legacy Wallet"}
	if _, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, now); err != nil {
		t.Fatal("legacy wallet without rdns was rejected", err)
	}
}

func TestConnectionTransitionsSerializeAcrossConnections(t *testing.T) {
	ctx := context.Background()
	for _, race := range []string{"identical completions", "conflicting completions", "cancellation", "expiry"} {
		t.Run(race, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			stores := []*Store{openTestStore(t, dir), openTestStore(t, dir)}
			for iteration := range 4 {
				request := createPendingConnection(t, stores[0], iteration)
				completion, digest := completionFixture(request)
				completion.SessionID = fmt.Sprintf("%032x", 1000+iteration)
				digest = sha256.Sum256([]byte(completion.SessionID))
				type result struct {
					request app.ConnectionRequest
					err     error
				}
				results := make(chan result, 8)
				start := make(chan struct{})
				var group sync.WaitGroup
				for index := range 8 {
					group.Go(func() {
						<-start
						s := stores[index%len(stores)]
						var got app.ConnectionRequest
						var err error
						if index%2 == 1 && (race == "cancellation" || race == "expiry") {
							state, now := app.ConnectionCanceled, request.CreatedAt.Add(time.Second)
							if race == "expiry" {
								state, now = app.ConnectionExpired, request.ExpiresAt
							}
							got, err = s.FinishConnectionRequest(ctx, request.ID, state, now)
						} else {
							candidate, candidateDigest := completion, digest
							if race == "conflicting completions" {
								candidate.SessionID = fmt.Sprintf("%032x", 10000+iteration*8+index)
								candidateDigest = sha256.Sum256([]byte(candidate.SessionID))
							}
							got, err = s.CompleteConnectionRequest(ctx, request.ID, candidate, candidateDigest, request.CreatedAt.Add(time.Second))
						}
						results <- result{got, err}
					})
				}
				close(start)
				group.Wait()
				close(results)
				final, err := stores[0].GetConnectionRequest(ctx, request.ID)
				if err != nil || final.State == app.ConnectionPending {
					t.Fatal("race left connection pending", final, err)
				}
				successes := 0
				for result := range results {
					if result.err == nil {
						successes++
					} else if !errors.Is(result.err, app.ErrConnectionConflict) {
						t.Fatal("race returned storage failure", result.err)
					}
					if !reflect.DeepEqual(result.request, final) {
						t.Fatal("race returned different durable outcomes", result.request, final)
					}
				}
				if successes == 0 || (race == "identical completions" && successes != 8) || (race == "conflicting completions" && successes != 1) {
					t.Fatal("incorrect winning/replayed writes", successes)
				}
				count := 0
				if final.State == app.ConnectionSucceeded {
					count = 1
				}
				assertSessionCount(t, stores[0], request.ID, count)
			}
		})
	}
}

func TestConnectionTransitionsCanceledContextDoesNotWrite(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request := createPendingConnection(t, s, 0)
	completion, digest := completionFixture(request)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.CompleteConnectionRequest(ctx, request.ID, completion, digest, request.CreatedAt.Add(time.Second)); !errors.Is(err, context.Canceled) {
		t.Fatal("completion ignored canceled context", err)
	}
	if _, err := s.FinishConnectionRequest(ctx, request.ID, app.ConnectionCanceled, request.CreatedAt.Add(time.Second)); !errors.Is(err, context.Canceled) {
		t.Fatal("termination ignored canceled context", err)
	}
	assertConnectionUnchanged(t, s, request)
	assertSessionCount(t, s, request.ID, 0)
}
