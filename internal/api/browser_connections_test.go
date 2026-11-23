//go:build windows || darwin

package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func browserConnectionPath(id string) string { return "/api/v1/browser/connections/" + id }

func (f connectionHTTPFixture) execution(t *testing.T, cookie *http.Cookie, connectionID, action, id, previousID string, status int) *httptest.ResponseRecorder {
	t.Helper()
	path := browserConnectionPath(connectionID) + "/execution"
	if action == "" {
		return f.request(t, "GET", path, "", browserHeaders(cookie), status)
	}
	var input any = ConnectionExecutionRequest{ID: id}
	if action == "recover" {
		input = ConnectionExecutionRecoverRequest{ID: id, PreviousID: previousID}
	}
	return f.request(t, "POST", path+"/"+action, jsonBody(t, input), browserHeaders(cookie), status)
}

func seedBrowserRequest(t *testing.T, f connectionHTTPFixture, index int, at time.Time) app.ConnectionRequest {
	t.Helper()
	at = at.UTC().Truncate(time.Millisecond)
	r := app.ConnectionRequest{ID: connectionHTTPInput(index).ID, ClientName: "Historical CLI", ChainID: "0x47", State: app.ConnectionPending, CreatedAt: at, ExpiresAt: at.Add(app.ConnectionRequestLifetime)}
	if err := f.database.CreateConnectionRequest(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	return r
}

// Historical grants make deadline/heartbeat cases deterministic without sleeps.
func seedBrowserCookie(t *testing.T, f connectionHTTPFixture, index byte, issued time.Time) (*http.Cookie, app.BrowserGrant) {
	t.Helper()
	var secret [32]byte
	secret[0] = index
	grant := app.BrowserGrant{ID: fmt.Sprintf("%032x", 9000+int(index)), IssuedAt: issued.UTC().Truncate(time.Millisecond)}
	grant.ExpiresAt = grant.IssuedAt.Add(app.BrowserGrantLifetime)
	if err := f.database.CreateBrowserGrant(context.Background(), grant, sha256.Sum256(secret[:])); err != nil {
		t.Fatal(err)
	}
	return managementCookie(hex.EncodeToString(secret[:])), grant
}

func TestBrowserConnectionHTTPObservationAndOwnership(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	first, firstGrant := f.exchange(t, f.ticket(t))
	second, _ := f.exchange(t, f.ticket(t))
	w := f.request(t, "GET", browserConnectionPath(r.ID), "", browserHeaders(first), 200)
	if !reflect.DeepEqual(responseJSON[ConnectionResponse](t, w), r) {
		t.Fatal("browser observation changed the request")
	}
	w = f.execution(t, first, r.ID, "", "", "", 404)
	if responseJSON[ErrorResponse](t, w).Code != "execution_not_found" {
		t.Fatal("missing execution was not explicit")
	}
	id := fmt.Sprintf("%032x", 500)
	w = f.execution(t, first, r.ID, "claim", id, "", 200)
	claimed := responseJSON[ConnectionExecutionResponse](t, w)
	if claimed.ConnectionID != r.ID || claimed.ID != id || claimed.Phase != "ready" || claimed.CallID != "" {
		t.Fatal(claimed)
	}
	for _, observer := range []*http.Cookie{first, second} {
		w = f.execution(t, observer, r.ID, "", "", "", 200)
		if !reflect.DeepEqual(responseJSON[ConnectionExecutionResponse](t, w), claimed) {
			t.Fatal("observation renewed or replaced execution")
		}
		for _, secret := range []string{first.Value, second.Value, firstGrant.ID, "browserGrantId", "token_hash"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("execution response disclosed browser identity or secret")
			}
		}
	}
	w = f.execution(t, first, r.ID, "claim", id, "", 200)
	if !reflect.DeepEqual(responseJSON[ConnectionExecutionResponse](t, w), claimed) {
		t.Fatal("claim replay renewed heartbeat")
	}
	for _, action := range []string{"claim", "heartbeat", "interrupt"} {
		w = f.execution(t, second, r.ID, action, id, "", 409)
		if responseJSON[ErrorResponse](t, w).Code != "execution_conflict" {
			t.Fatal("another browser obtained execution")
		}
	}
	f.execution(t, first, r.ID, "heartbeat", fmt.Sprintf("%032x", 501), "", 409)
	stored, err := f.database.GetConnectionExecution(context.Background(), r.ID)
	if err != nil || stored.BrowserGrantID != firstGrant.ID || stored.ID != id {
		t.Fatal("HTTP body supplied ownership", stored, err)
	}
	info := responseJSON[InfoResponse](t, f.request(t, "GET", "/api/v1/info", "", nil, 200))
	if info.APIVersion.Minor != 4 || !strings.Contains(jsonBody(t, info.Capabilities), "browser.connections.coordination") {
		t.Fatal("missing browser coordination capability")
	}
}

func TestBrowserConnectionHTTPPermissions(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	cookie, _ := f.exchange(t, f.ticket(t))
	id := fmt.Sprintf("%032x", 500)
	f.execution(t, cookie, r.ID, "claim", id, "", 200)
	before, err := f.database.GetConnectionExecution(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	expired, _ := seedBrowserCookie(t, f, 31, time.Now().Add(-31*24*time.Hour))
	revoked, grant := f.exchange(t, f.ticket(t))
	if err := f.database.RevokeBrowserGrant(context.Background(), grant.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/execution", "/execution/claim", "/execution/heartbeat", "/execution/interrupt", "/execution/recover"} {
		method, body := "GET", ""
		if strings.Count(suffix, "/") == 2 {
			method, body = "POST", jsonBody(t, ConnectionExecutionRequest{ID: id})
		}
		path := browserConnectionPath(r.ID) + suffix
		for _, tc := range []struct {
			headers map[string]string
			status  int
		}{
			{nil, 403}, {map[string]string{"Origin": Origin}, 401},
			{map[string]string{"Origin": Origin, "Authorization": "Bearer " + serviceFixture}, 401},
			{map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}, 403},
			{map[string]string{"Origin": "null", "Cookie": cookie.Name + "=" + cookie.Value}, 403},
			{map[string]string{"Origin": Origin, "Cookie": cookie.Name + "=" + cookie.Value, "Host": "localhost:18765"}, 403},
			{browserHeaders(expired), 401}, {browserHeaders(revoked), 401},
			{map[string]string{"Origin": Origin, "Cookie": cookie.Name + "=" + cookie.Value + "; " + cookie.Name + "=" + cookie.Value}, 401},
		} {
			f.request(t, method, path, body, tc.headers, tc.status)
		}
		req := httptest.NewRequest(method, Origin+path, strings.NewReader(body))
		req.Header.Set("Cookie", cookie.Name+"="+cookie.Value)
		req.Header["Origin"] = []string{Origin, Origin}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("duplicate Origin accepted")
		}
	}
	after, err := f.database.GetConnectionExecution(context.Background(), r.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("unauthorized request modified execution", after, err)
	}
	headers := browserHeaders(cookie)
	headers["Authorization"] = "Bearer wrong"
	f.request(t, "GET", browserConnectionPath(r.ID), "", headers, 200)
	for _, suffix := range []string{"/complete", "/reject", "/execution/begin", "/execution/return"} {
		f.request(t, "POST", browserConnectionPath(r.ID)+suffix, "{}", browserHeaders(cookie), 404)
	}
}

func TestBrowserConnectionHTTPValidation(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	cookie, _ := f.exchange(t, f.ticket(t))
	id := fmt.Sprintf("%032x", 500)
	for _, action := range []string{"claim", "heartbeat", "interrupt", "recover"} {
		path := browserConnectionPath(r.ID) + "/execution/" + action
		valid := jsonBody(t, ConnectionExecutionRequest{ID: id})
		if action == "recover" {
			valid = jsonBody(t, ConnectionExecutionRecoverRequest{ID: id, PreviousID: fmt.Sprintf("%032x", 499)})
		}
		for _, body := range []string{"", "{}", "null", "[]", valid + "{}", `{"id":"bad"}`, `{"id":"` + strings.Repeat("A", 32) + `"}`, strings.TrimSuffix(valid, "}") + `,"browserGrantId":"` + fmt.Sprintf("%032x", 900) + `"}`, strings.TrimSuffix(valid, "}") + `,"connectionId":"` + r.ID + `"}`} {
			f.request(t, "POST", path, body, browserHeaders(cookie), 400)
		}
		f.request(t, "POST", path, strings.Repeat(" ", 1025), browserHeaders(cookie), 413)
		f.request(t, "POST", path+"?id=x", valid, browserHeaders(cookie), 400)
		headers := browserHeaders(cookie)
		headers["Content-Type"] = "text/plain"
		f.request(t, "POST", path, valid, headers, 415)
		w := f.request(t, "GET", path, "", browserHeaders(cookie), 405)
		if w.Header().Get("Allow") != "POST" {
			t.Fatal("missing POST Allow")
		}
	}
	for _, suffix := range []string{"", "/execution"} {
		path := browserConnectionPath(r.ID) + suffix
		f.request(t, "GET", path+"?x=1", "", browserHeaders(cookie), 400)
		f.request(t, "GET", path, "{}", browserHeaders(cookie), 400)
		f.request(t, "POST", path, "{}", browserHeaders(cookie), 405)
		f.request(t, "GET", browserConnectionPath("bad")+suffix, "", browserHeaders(cookie), 400)
	}
	missing := strings.Repeat("f", 32)
	f.request(t, "GET", browserConnectionPath(missing), "", browserHeaders(cookie), 404)
	f.execution(t, cookie, missing, "claim", id, "", 404)
	f.execution(t, cookie, r.ID, "heartbeat", id, "", 404)
	f.execution(t, cookie, r.ID, "recover", id, id, 400)
	body := jsonBody(t, ConnectionExecutionRequest{ID: id})
	f.request(t, "POST", browserConnectionPath(r.ID)+"/execution/claim", body+strings.Repeat(" ", MaxControlBody-len(body)), browserHeaders(cookie), 200)
}

func TestBrowserConnectionHTTPRecovery(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	first, _ := f.exchange(t, f.ticket(t))
	second, _ := f.exchange(t, f.ticket(t))
	oldID, newID := fmt.Sprintf("%032x", 500), fmt.Sprintf("%032x", 501)
	f.execution(t, first, r.ID, "claim", oldID, "", 200)
	f.execution(t, second, r.ID, "recover", newID, oldID, 409)
	w := f.execution(t, first, r.ID, "interrupt", oldID, "", 200)
	interrupted := responseJSON[ConnectionExecutionResponse](t, w)
	w = f.execution(t, first, r.ID, "interrupt", oldID, "", 200)
	if interrupted.Phase != "interrupted_ready" || !reflect.DeepEqual(responseJSON[ConnectionExecutionResponse](t, w), interrupted) {
		t.Fatal("interruption replay changed state")
	}
	f.execution(t, second, r.ID, "recover", newID, fmt.Sprintf("%032x", 499), 409)
	w = f.execution(t, second, r.ID, "recover", newID, oldID, 200)
	recovered := responseJSON[ConnectionExecutionResponse](t, w)
	if recovered.Phase != "ready" || recovered.ID != newID {
		t.Fatal(recovered)
	}
	w = f.execution(t, second, r.ID, "recover", newID, oldID, 200)
	if !reflect.DeepEqual(responseJSON[ConnectionExecutionResponse](t, w), recovered) {
		t.Fatal("recovery replay renewed heartbeat")
	}
	for _, action := range []string{"claim", "heartbeat", "interrupt"} {
		f.execution(t, first, r.ID, action, oldID, "", 409)
	}
	w = f.request(t, "GET", browserConnectionPath(r.ID), "", browserHeaders(second), 200)
	if current := responseJSON[ConnectionResponse](t, w); !reflect.DeepEqual(current, r) {
		t.Fatal("recovery changed request or created Session", current)
	}
}

func TestBrowserConnectionHTTPLifetimes(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	now := time.Now().UTC().Truncate(time.Millisecond)
	cookie, grant := seedBrowserCookie(t, f, 1, now.Add(-24*time.Hour))
	r := seedBrowserRequest(t, f, 0, now.Add(-2*time.Minute))
	ref := app.ConnectionExecutionRef{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 500), BrowserGrantID: grant.ID}
	before, err := f.database.ClaimConnectionExecution(context.Background(), ref, now.Add(-app.ConnectionExecutionHeartbeatTimeout))
	if err != nil {
		t.Fatal(err)
	}
	w := f.execution(t, cookie, r.ID, "", "", "", 200)
	if snapshot := responseJSON[ConnectionExecutionResponse](t, w); snapshot.Phase != "ready" || snapshot.LastSeenAt != timestamp(before.LastSeenAt) {
		t.Fatal("read normalized or renewed execution", snapshot)
	}
	f.execution(t, cookie, r.ID, "heartbeat", ref.ID, "", 409)
	w = f.execution(t, cookie, r.ID, "", "", "", 200)
	if responseJSON[ConnectionExecutionResponse](t, w).Phase != "interrupted_ready" {
		t.Fatal("stale heartbeat revived execution")
	}
	newID := fmt.Sprintf("%032x", 501)
	f.execution(t, cookie, r.ID, "recover", newID, ref.ID, 200)
	f.execution(t, cookie, r.ID, "interrupt", newID, "", 200)
	// A recent old heartbeat can advance through HTTP without changing deadlines.
	live := seedBrowserRequest(t, f, 1, now.Add(-time.Minute))
	ref.ConnectionID, ref.ID = live.ID, fmt.Sprintf("%032x", 502)
	before, err = f.database.ClaimConnectionExecution(context.Background(), ref, now.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	w = f.execution(t, cookie, live.ID, "heartbeat", ref.ID, "", 200)
	seen, err := time.Parse(time.RFC3339Nano, responseJSON[ConnectionExecutionResponse](t, w).LastSeenAt)
	if err != nil || !seen.After(before.LastSeenAt) {
		t.Fatal("live heartbeat did not advance", seen, err)
	}
	unchanged, err := f.database.GetConnectionRequest(context.Background(), live.ID)
	if err != nil || !reflect.DeepEqual(unchanged, live) {
		t.Fatal("heartbeat changed original request", unchanged, err)
	}
	completed, session := f.seedSession(t, 2, now.Add(-2*time.Minute))
	f.request(t, "GET", browserConnectionPath(completed.ID), "", browserHeaders(cookie), 200)
	f.execution(t, cookie, completed.ID, "", "", "", 404)
	stored, err := f.database.GetSession(context.Background(), session.ID)
	if err != nil || !reflect.DeepEqual(stored, session) {
		t.Fatal("browser observation renewed Session", stored, err)
	}
	expired := seedBrowserRequest(t, f, 3, now.Add(-10*time.Minute))
	w = f.request(t, "GET", browserConnectionPath(expired.ID), "", browserHeaders(cookie), 200)
	if snapshot := responseJSON[ConnectionResponse](t, w); snapshot.State != "expired" || snapshot.FinishedAt != timestamp(expired.ExpiresAt) {
		t.Fatal(snapshot)
	}
	w = f.execution(t, cookie, expired.ID, "claim", fmt.Sprintf("%032x", 503), "", 409)
	if responseJSON[ErrorResponse](t, w).Code != "connection_conflict" {
		t.Fatal("expired request accepted")
	}
}

func TestBrowserConnectionHTTPWalletWaiting(t *testing.T) {
	for _, reason := range []string{"canceled", "expired", "restart"} {
		t.Run(reason, func(t *testing.T) {
			f := newConnectionHTTPFixture(t, "")
			now := time.Now().UTC().Truncate(time.Millisecond)
			cookie, grant := seedBrowserCookie(t, f, 1, now.Add(-24*time.Hour))
			at := now.Add(-time.Minute)
			if reason == "expired" {
				at = now.Add(-10 * time.Minute)
			}
			r := seedBrowserRequest(t, f, 0, at)
			ref := app.ConnectionExecutionRef{ConnectionID: r.ID, ID: fmt.Sprintf("%032x", 500), BrowserGrantID: grant.ID}
			if _, err := f.database.ClaimConnectionExecution(context.Background(), ref, at.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			call := app.ConnectionCallRef{ConnectionExecutionRef: ref, CallID: fmt.Sprintf("%032x", 600)}
			if permit, err := f.database.BeginConnectionCall(context.Background(), call, "", at.Add(time.Second)); err != nil || !permit.Invoke {
				t.Fatal(permit, err)
			}
			switch reason {
			case "canceled":
				f.request(t, "POST", "/api/v1/connections/"+r.ID+"/cancel", "", controlHeaders(), 200)
			case "expired":
				f.request(t, "GET", browserConnectionPath(r.ID), "", browserHeaders(cookie), 200)
			case "restart":
				f.database.Close()
				f = newConnectionHTTPFixture(t, f.dir)
				if _, err := f.core.Restore(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			w := f.execution(t, cookie, r.ID, "", "", "", 200)
			waiting := responseJSON[ConnectionExecutionResponse](t, w)
			if waiting.Phase != "interrupted_calling" || waiting.CallID != call.CallID {
				t.Fatal("waiting lost", waiting)
			}
			other := f.create(t, connectionHTTPInput(1))
			newID := fmt.Sprintf("%032x", 501)
			w = f.execution(t, cookie, other.ID, "claim", fmt.Sprintf("%032x", 502), "", 409)
			if responseJSON[ErrorResponse](t, w).Code != "wallet_busy" {
				t.Fatal("unknown call did not occupy slot")
			}
			w = f.execution(t, cookie, r.ID, "recover", newID, ref.ID, 200)
			recovered := responseJSON[ConnectionExecutionResponse](t, w)
			if reason == "restart" {
				if recovered.Phase != "ready" || recovered.ID != newID || recovered.CallID != "" {
					t.Fatal(recovered)
				}
				f.execution(t, cookie, r.ID, "heartbeat", ref.ID, "", 409)
				f.execution(t, cookie, r.ID, "interrupt", newID, "", 200)
			} else if recovered.Phase != "finished" || recovered.ID != ref.ID {
				t.Fatal("terminal recovery created a new execution", recovered)
			}
			f.execution(t, cookie, other.ID, "claim", fmt.Sprintf("%032x", 502), "", 200)
			current, err := f.database.GetConnectionRequest(context.Background(), r.ID)
			if err != nil || current.SessionID != "" || !current.ExpiresAt.Equal(r.ExpiresAt) {
				t.Fatal("recovery created Session or reset deadline", current, err)
			}
		})
	}
}

func browserCoordinationSQL(t *testing.T, dir string) *sql.DB {
	t.Helper()
	path := filepath.ToSlash(filepath.Join(dir, store.DatabaseName))
	if filepath.VolumeName(dir) != "" {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"rw"}, "_pragma": {"foreign_keys(ON)", "journal_mode(PERSIST)", "busy_timeout(1000)"}}.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestBrowserConnectionHTTPStorageFailure(t *testing.T) {
	for _, action := range []string{"claim", "heartbeat", "interrupt", "recover"} {
		for _, stage := range []string{"write", "commit"} {
			t.Run(action+"/"+stage, func(t *testing.T) {
				f := newConnectionHTTPFixture(t, "")
				r := f.create(t, connectionHTTPInput(0))
				cookie, _ := f.exchange(t, f.ticket(t))
				id, previous := fmt.Sprintf("%032x", 500), ""
				event := "INSERT"
				if action != "claim" {
					f.execution(t, cookie, r.ID, "claim", id, "", 200)
					event = "UPDATE"
				}
				if action == "recover" {
					f.execution(t, cookie, r.ID, "interrupt", id, "", 200)
					previous, id = id, fmt.Sprintf("%032x", 501)
				}
				before, readErr := f.database.GetConnectionExecution(context.Background(), r.ID)
				db := browserCoordinationSQL(t, f.dir)
				trigger := "CREATE TRIGGER fail_browser_coordination BEFORE " + event + " ON connection_executions BEGIN SELECT RAISE(ABORT, 'private failure details'); END"
				if stage == "commit" {
					trigger = "CREATE TABLE browser_coordination_guard (id TEXT REFERENCES connection_requests(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER fail_browser_coordination AFTER " + event + " ON connection_executions BEGIN INSERT INTO browser_coordination_guard VALUES ('missing'); END"
				}
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
				w := f.execution(t, cookie, r.ID, action, id, previous, 500)
				if responseJSON[ErrorResponse](t, w).Code != "storage_error" || strings.Contains(w.Body.String(), f.dir) || strings.Contains(w.Body.String(), "private failure details") {
					t.Fatal("storage error leaked or misreported")
				}
				after, err := f.database.GetConnectionExecution(context.Background(), r.ID)
				if action == "claim" {
					if readErr != app.ErrConnectionExecutionNotFound || err != app.ErrConnectionExecutionNotFound {
						t.Fatal("failed claim committed", after, err)
					}
				} else if readErr != nil || err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("failed mutation committed", after, err)
				}
				if _, err := db.Exec("DROP TRIGGER fail_browser_coordination"); err != nil {
					t.Fatal(err)
				}
				f.execution(t, cookie, r.ID, action, id, previous, 200)
			})
		}
	}
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	cookie, _ := f.exchange(t, f.ticket(t))
	f.database.Close()
	w := f.request(t, "GET", browserConnectionPath(r.ID), "", browserHeaders(cookie), 500)
	if responseJSON[ErrorResponse](t, w).Code != "authorization_error" || strings.Contains(w.Body.String(), f.dir) {
		t.Fatal("authentication storage error leaked or misreported")
	}
}

type revokeAfterAuthenticationStore struct{ app.BrowserGrantStore }

func (s revokeAfterAuthenticationStore) FindActiveBrowserGrant(ctx context.Context, digest [32]byte, now time.Time) (app.BrowserGrant, error) {
	grant, err := s.BrowserGrantStore.FindActiveBrowserGrant(ctx, digest, now)
	if err == nil {
		err = s.BrowserGrantStore.RevokeBrowserGrant(ctx, grant.ID, now)
	}
	return grant, err
}

func TestBrowserConnectionHTTPRevocationAfterAuthentication(t *testing.T) {
	for _, action := range []string{"claim", "heartbeat", "interrupt", "recover"} {
		t.Run(action, func(t *testing.T) {
			f := newConnectionHTTPFixture(t, "")
			r := f.create(t, connectionHTTPInput(0))
			cookie, _ := f.exchange(t, f.ticket(t))
			id, previous := fmt.Sprintf("%032x", 500), ""
			if action != "claim" {
				f.execution(t, cookie, r.ID, "claim", id, "", 200)
			}
			if action == "recover" {
				f.execution(t, cookie, r.ID, "interrupt", id, "", 200)
				previous, id = id, fmt.Sprintf("%032x", 501)
			}
			before, readErr := f.database.GetConnectionExecution(context.Background(), r.ID)
			authorization, err := app.NewBrowserAuthorization(revokeAfterAuthenticationStore{f.database})
			if err != nil {
				t.Fatal(err)
			}
			f.handler, err = New(Config{ServiceToken: serviceFixture, Stop: func() { t.Error("unexpected stop") }, BrowserAuthorization: authorization, Connections: f.core})
			if err != nil {
				t.Fatal(err)
			}
			w := f.execution(t, cookie, r.ID, action, id, previous, 401)
			if responseJSON[ErrorResponse](t, w).Code != "unauthenticated" {
				t.Fatal("revocation ignored")
			}
			after, err := f.database.GetConnectionExecution(context.Background(), r.ID)
			if action == "claim" {
				if readErr != app.ErrConnectionExecutionNotFound || err != app.ErrConnectionExecutionNotFound {
					t.Fatal("revoked claim committed")
				}
			} else if readErr != nil || err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("revoked mutation committed", after, err)
			}
		})
	}
}

func TestBrowserConnectionHTTPConcurrentClaims(t *testing.T) {
	for _, sameRequest := range []bool{true, false} {
		t.Run(fmt.Sprint(sameRequest), func(t *testing.T) {
			f := newConnectionHTTPFixture(t, "")
			first := f.create(t, connectionHTTPInput(0))
			second := first
			if !sameRequest {
				second = f.create(t, connectionHTTPInput(1))
			}
			cookieA, grantA := f.exchange(t, f.ticket(t))
			cookieB, grantB := f.exchange(t, f.ticket(t))
			server := httptest.NewServer(f.handler)
			defer server.Close()
			client := server.Client()
			client.Timeout = 5 * time.Second
			type result struct {
				status int
				data   []byte
				err    error
			}
			results := make(chan result, 2)
			body := jsonBody(t, ConnectionExecutionRequest{ID: fmt.Sprintf("%032x", 500)})
			for _, entry := range []struct {
				connection string
				cookie     *http.Cookie
			}{{first.ID, cookieA}, {second.ID, cookieB}} {
				go func() {
					req, _ := http.NewRequest("POST", server.URL+browserConnectionPath(entry.connection)+"/execution/claim", strings.NewReader(body))
					req.Host = Address
					req.Header.Set("Origin", Origin)
					req.Header.Set("Content-Type", "application/json")
					req.AddCookie(entry.cookie)
					response, err := client.Do(req)
					if err != nil {
						results <- result{err: err}
						return
					}
					defer response.Body.Close()
					data, err := io.ReadAll(response.Body)
					results <- result{response.StatusCode, data, err}
				}()
			}
			accepted := 0
			for range 2 {
				outcome := <-results
				if outcome.err != nil {
					t.Fatal(outcome.err)
				}
				if outcome.status == 200 {
					accepted++
					var execution ConnectionExecutionResponse
					if err := json.Unmarshal(outcome.data, &execution); err != nil {
						t.Fatal(err)
					}
					stored, err := f.database.GetConnectionExecution(context.Background(), execution.ConnectionID)
					if err != nil || (stored.BrowserGrantID != grantA.ID && stored.BrowserGrantID != grantB.ID) {
						t.Fatal("owner not from authenticated cookie", stored, err)
					}
				} else if outcome.status != 409 {
					t.Fatal("unexpected claim response", outcome.status, string(outcome.data))
				}
			}
			if accepted != 1 {
				t.Fatal("global slot had multiple winners", accepted)
			}
		})
	}
}
