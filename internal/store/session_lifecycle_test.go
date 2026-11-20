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

func createLiveSession(t *testing.T, s *Store, index int) (app.ConnectionRequest, app.Session, [32]byte) {
	t.Helper()
	request := createPendingConnection(t, s, index)
	completion, _ := completionFixture(request)
	completion.SessionID = fmt.Sprintf("%032x", 1000+index)
	digest := sha256.Sum256([]byte(completion.SessionID))
	request, err := s.CompleteConnectionRequest(context.Background(), request.ID, completion, digest, request.CreatedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.GetSession(context.Background(), completion.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return request, session, digest
}

func assertSessionUnchanged(t *testing.T, s *Store, want app.Session) {
	t.Helper()
	got, err := s.GetSession(context.Background(), want.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("Session changed unexpectedly", got, want, err)
	}
}

func TestSessionLifecycleAccessAndReadOnlyQueries(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request, session, digest := createLiveSession(t, s, 0)
	if _, err := s.FindSession(ctx, session.ID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConnectionRequest(ctx, request.ID); err != nil {
		t.Fatal(err)
	}
	assertSessionUnchanged(t, s, session)
	now := session.LastAccessAt.Add(4*time.Minute + 123456*time.Nanosecond)
	want := session
	want.LastAccessAt = time.UnixMilli(now.UnixMilli()).UTC()
	got, err := s.AccessSession(ctx, session.ID, digest, now)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("live client access did not renew only the access time", got, err)
	}
	// Earlier observations, including a clock rollback, must not shorten a lease.
	for _, earlier := range []time.Time{session.LastAccessAt.Add(time.Minute), session.CreatedAt.Add(-time.Hour)} {
		got, err = s.AccessSession(ctx, session.ID, digest, earlier)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("clock rollback reduced last access", got, err)
		}
	}
	assertSessionUnchanged(t, s, want)
	assertConnectionUnchanged(t, s, request)
}

func TestSessionLifecycleIdleBoundaryAndRestart(t *testing.T) {
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			s := openTestStore(t, dir)
			request, session, digest := createLiveSession(t, s, 0)
			expires := session.LastAccessAt.Add(app.SessionIdleLifetime)
			got, err := s.AccessSession(ctx, session.ID, digest, expires.Add(offset))
			want := session
			if offset < 0 {
				want.LastAccessAt = expires.Add(-time.Millisecond)
			} else {
				want.ClosedAt = &expires
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("idle boundary renewed an expired Session or changed binding", got, err)
			}
			s.Close()
			s = openTestStore(t, dir)
			got, err = s.AccessSession(ctx, session.ID, digest, session.CreatedAt)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("restart or earlier clock revived/rewound Session", got, err)
			}
			assertSessionUnchanged(t, s, want)
			assertConnectionUnchanged(t, s, request)
		})
	}
}

func TestSessionLifecycleCloseKeepsHistory(t *testing.T) {
	ctx := context.Background()
	for _, offset := range []time.Duration{-time.Minute, time.Minute, 6 * time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
			request, session, digest := createLiveSession(t, s, 0)
			now, closed := session.LastAccessAt.Add(offset), session.LastAccessAt.Add(offset)
			if offset < 0 {
				closed = session.LastAccessAt
			} else if offset >= app.SessionIdleLifetime {
				closed = session.LastAccessAt.Add(app.SessionIdleLifetime)
			}
			want := session
			want.ClosedAt = &closed
			for _, closeAt := range []time.Time{now, now.Add(time.Hour)} {
				got, err := s.CloseSession(ctx, session.ID, digest, closeAt)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("close replay lost binding or changed original closure", got, err)
				}
			}
			got, err := s.AccessSession(ctx, session.ID, digest, now.Add(2*time.Hour))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("client query revived closed Session", got, err)
			}
			got, err = s.FindSession(ctx, session.ID, digest)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("closed Session history became inaccessible", got, err)
			}
			assertConnectionUnchanged(t, s, request)
		})
	}
}

func TestSessionLifecycleCredentialIsolation(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	request, session, digest := createLiveSession(t, s, 0)
	_, other, otherDigest := createLiveSession(t, s, 1)
	_, requestDigest := connectionFixture(0)
	now := session.LastAccessAt.Add(time.Hour)
	for _, resource := range []struct {
		id     string
		digest [32]byte
	}{{session.ID, otherDigest}, {other.ID, digest}, {session.ID, requestDigest}, {"missing", digest}} {
		for _, operation := range []func(context.Context, string, [32]byte, time.Time) (app.Session, error){s.AccessSession, s.CloseSession} {
			got, err := operation(ctx, resource.id, resource.digest, now)
			if !errors.Is(err, app.ErrSessionNotFound) || got.ID != "" {
				t.Fatal("wrong credential exposed/modified a Session", got, err)
			}
		}
	}
	assertSessionUnchanged(t, s, session)
	assertSessionUnchanged(t, s, other)
	assertConnectionUnchanged(t, s, request)
}

func TestConnectionSessionExpiryMaintenance(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	s := openTestStore(t, dir)
	due := createPendingConnection(t, s, 0)
	now := due.CreatedAt.Add(10 * time.Minute)
	fresh, freshDigest := connectionFixture(1)
	fresh.CreatedAt, fresh.ExpiresAt = now.Add(-time.Minute), now.Add(4*time.Minute)
	if err := s.CreateConnectionRequest(ctx, fresh, freshDigest); err != nil {
		t.Fatal(err)
	}
	succeeded, idle, _ := createLiveSession(t, s, 2)
	_, live, liveDigest := createLiveSession(t, s, 3)
	for _, accessed := range []time.Time{now.Add(-6 * time.Minute), now.Add(-2 * time.Minute)} {
		var err error
		live, err = s.AccessSession(ctx, live.ID, liveDigest, accessed)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, closed, closedDigest := createLiveSession(t, s, 4)
	closed, err := s.CloseSession(ctx, closed.ID, closedDigest, closed.LastAccessAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	rejected := createPendingConnection(t, s, 5)
	rejected, err = s.FinishConnectionRequest(ctx, rejected.ID, app.ConnectionRejected, rejected.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := s.ExpireConnectionsAndSessions(ctx, now)
	if err != nil || counts != (app.ConnectionExpiryCounts{ConnectionRequests: 1, Sessions: 1}) {
		t.Fatal("maintenance expired wrong records", counts, err)
	}
	due.State, due.FinishedAt = app.ConnectionExpired, &due.ExpiresAt
	expired := idle.LastAccessAt.Add(app.SessionIdleLifetime)
	idle.ClosedAt = &expired
	for _, request := range []app.ConnectionRequest{due, fresh, succeeded, rejected} {
		assertConnectionUnchanged(t, s, request)
	}
	for _, session := range []app.Session{idle, live, closed} {
		assertSessionUnchanged(t, s, session)
	}
	counts, err = s.ExpireConnectionsAndSessions(ctx, now)
	if err != nil || counts != (app.ConnectionExpiryCounts{}) {
		t.Fatal("maintenance retry changed historical records", counts, err)
	}
	s.Close()
	s = openTestStore(t, dir)
	assertSessionUnchanged(t, s, idle)
	assertConnectionUnchanged(t, s, due)
}

func TestSessionLifecycleRollback(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"access", "close", "maintenance"} {
		for _, stage := range []string{"update", "commit"} {
			t.Run(operation+"/"+stage, func(t *testing.T) {
				s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
				_, session, digest := createLiveSession(t, s, 0)
				pending := createPendingConnection(t, s, 1)
				trigger := "CREATE TRIGGER fail_lifecycle BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, 'injected failure'); END"
				if stage == "commit" {
					trigger = `CREATE TABLE lifecycle_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED);
CREATE TRIGGER fail_lifecycle AFTER UPDATE ON sessions BEGIN INSERT INTO lifecycle_guard VALUES ('missing'); END`
				}
				if _, err := s.db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
				now := session.LastAccessAt.Add(time.Minute)
				var err error
				if operation == "maintenance" {
					counts, failure := s.ExpireConnectionsAndSessions(ctx, now.Add(time.Hour))
					err = failure
					if counts != (app.ConnectionExpiryCounts{}) {
						t.Fatal("failed maintenance returned committed counts", counts)
					}
				} else {
					call := s.AccessSession
					if operation == "close" {
						call = s.CloseSession
					}
					got, failure := call(ctx, session.ID, digest, now)
					err = failure
					if got.ID != "" {
						t.Fatal("failed lifecycle write returned a Session", got)
					}
				}
				if err == nil {
					t.Fatal("injected failure committed")
				}
				assertSessionUnchanged(t, s, session)
				assertConnectionUnchanged(t, s, pending)
			})
		}
	}
}

func TestSessionLifecycleConcurrentWrites(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"accesses", "close", "expiry"} {
		t.Run(scenario, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "data")
			stores := []*Store{openTestStore(t, dir), openTestStore(t, dir)}
			for iteration := range 4 {
				request, session, digest := createLiveSession(t, stores[0], iteration)
				start, results := make(chan struct{}), make(chan error, 8)
				var group sync.WaitGroup
				for index := range 8 {
					group.Go(func() {
						<-start
						s := stores[index%2]
						now := session.LastAccessAt.Add(time.Duration(index+1) * time.Second)
						var err error
						if scenario == "expiry" {
							now = session.LastAccessAt.Add(app.SessionIdleLifetime)
							if index%2 == 1 {
								_, err = s.ExpireConnectionsAndSessions(ctx, now)
							} else {
								_, err = s.AccessSession(ctx, session.ID, digest, now)
							}
						} else if scenario == "close" && index%2 == 1 {
							_, err = s.CloseSession(ctx, session.ID, digest, now)
						} else {
							_, err = s.AccessSession(ctx, session.ID, digest, now)
						}
						results <- err
					})
				}
				close(start)
				group.Wait()
				close(results)
				for err := range results {
					if err != nil {
						t.Fatal("concurrent lifecycle write failed", err)
					}
				}
				got, err := stores[0].GetSession(ctx, session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "accesses" {
					want := session
					want.LastAccessAt = session.LastAccessAt.Add(8 * time.Second)
					if !reflect.DeepEqual(got, want) {
						t.Fatal("out-of-order accesses lost latest renewal", got)
					}
				} else {
					if got.ClosedAt == nil || got.ClosedAt.Before(got.LastAccessAt) {
						t.Fatal("concurrent access revived closed Session", got)
					}
					if scenario == "expiry" && !got.ClosedAt.Equal(session.LastAccessAt.Add(app.SessionIdleLifetime)) {
						t.Fatal("concurrent expiry reset original deadline", got)
					}
					replay, err := stores[0].AccessSession(ctx, session.ID, digest, session.LastAccessAt)
					if err != nil || !reflect.DeepEqual(replay, got) {
						t.Fatal("closed Session revived after race", replay, err)
					}
				}
				assertConnectionUnchanged(t, stores[0], request)
			}
		})
	}
}

func TestSessionLifecycleInvalidTimeAndCanceledContext(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "data"))
	_, session, digest := createLiveSession(t, s, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, operation := range []func(context.Context, string, [32]byte, time.Time) (app.Session, error){s.AccessSession, s.CloseSession} {
		if _, err := operation(ctx, session.ID, digest, session.CreatedAt.Add(time.Minute)); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled lifecycle command was accepted", err)
		}
		if _, err := operation(context.Background(), session.ID, digest, time.Time{}); !errors.Is(err, app.ErrInvalidSession) {
			t.Fatal("invalid lifecycle time accepted", err)
		}
	}
	if _, err := s.ExpireConnectionsAndSessions(ctx, session.CreatedAt.Add(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled maintenance was accepted", err)
	}
	if _, err := s.ExpireConnectionsAndSessions(context.Background(), time.Time{}); !errors.Is(err, app.ErrInvalidConnectionRequest) {
		t.Fatal("invalid maintenance time accepted", err)
	}
	assertSessionUnchanged(t, s, session)
}
