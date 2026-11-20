//go:build windows || darwin

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type connectionHTTPFixture struct {
	browserFixture
	core *app.ConnectionService
}

func newConnectionHTTPFixture(t *testing.T, dir string) connectionHTTPFixture {
	t.Helper()
	f := newBrowserFixture(t, dir)
	authorization, err := app.NewBrowserAuthorization(f.database)
	if err != nil {
		t.Fatal(err)
	}
	core, err := app.NewConnectionService(f.database)
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = New(Config{Version: "test", ServiceToken: serviceFixture, Stop: func() { t.Error("unexpected service stop") }, BrowserAuthorization: authorization, Connections: core})
	if err != nil {
		t.Fatal(err)
	}
	return connectionHTTPFixture{f, core}
}

func connectionHTTPInput(index int) ConnectionCreateRequest {
	return ConnectionCreateRequest{ID: fmt.Sprintf("%032x", index+1), ClientName: "Example CLI", Intent: "Connect an account", ChainID: "71", ChainName: "eSpace Testnet"}
}

func jsonBody(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func (f connectionHTTPFixture) create(t *testing.T, input ConnectionCreateRequest) ConnectionResponse {
	t.Helper()
	w := f.request(t, "POST", "/api/v1/connections", jsonBody(t, input), controlHeaders(), 200)
	return responseJSON[ConnectionResponse](t, w)
}

// Prepare historical consent directly in storage; HTTP cannot create a Session.
func (f connectionHTTPFixture) seedSession(t *testing.T, index int, created time.Time) (app.ConnectionRequest, app.Session) {
	t.Helper()
	created = created.UTC().Truncate(time.Millisecond)
	request := app.ConnectionRequest{ID: connectionHTTPInput(index).ID, ClientName: "Historical CLI", ChainID: "0x47", State: app.ConnectionPending, CreatedAt: created, ExpiresAt: created.Add(app.ConnectionRequestLifetime)}
	if err := f.database.CreateConnectionRequest(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	completion := app.ConnectionCompletion{SessionID: fmt.Sprintf("%032x", 1000+index), Wallet: app.WalletDescriptor{Kind: app.WalletEIP6963, Name: "Example Wallet", RDNS: "example.wallet"}, Account: "0x0000000000000000000000000000000000001234", ChainID: request.ChainID}
	request, err := f.database.CompleteConnectionRequest(context.Background(), request.ID, completion, created.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.database.GetSession(context.Background(), completion.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return request, session
}

func TestConnectionHTTPCreationRetries(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	input := connectionHTTPInput(0)
	input.Intent = strings.Repeat("description ", 120) // More than the control endpoint's 1 KiB limit.
	input.ChainID = "18446744073709551617"
	created := f.create(t, input)
	if created.State != "pending" || created.ChainID != "0x10000000000000001" || created.SessionID != "" || created.FinishedAt != "" || created.Intent != input.Intent {
		t.Fatal(created)
	}
	start, err := time.Parse(time.RFC3339Nano, created.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	end, err := time.Parse(time.RFC3339Nano, created.ExpiresAt)
	if err != nil || end.Sub(start) != app.ConnectionRequestLifetime {
		t.Fatal("invalid server deadline", end, err)
	}
	input.ChainID = "0x0010000000000000001"
	if replay := f.create(t, input); !reflect.DeepEqual(replay, created) {
		t.Fatal("equivalent retry reset request", replay, created)
	}
	for _, field := range []string{"client", "intent", "chain", "name"} {
		changed := input
		switch field {
		case "client":
			changed.ClientName += " changed"
		case "intent":
			changed.Intent += " changed"
		case "chain":
			changed.ChainID = "72"
		case "name":
			changed.ChainName += " changed"
		}
		w := f.request(t, "POST", "/api/v1/connections", jsonBody(t, changed), controlHeaders(), 409)
		if responseJSON[ErrorResponse](t, w).Code != "connection_conflict" {
			t.Fatal("unstable conflict code")
		}
	}
	f.database.Close()
	f = newConnectionHTTPFixture(t, f.dir)
	if replay := f.create(t, input); !reflect.DeepEqual(replay, created) {
		t.Fatal("reopen changed the original request", replay)
	}
	f.request(t, "POST", "/api/v1/connections/"+input.ID+"/cancel", "", controlHeaders(), 200)
	if retry := f.create(t, input); retry.State != "canceled" || retry.ExpiresAt != created.ExpiresAt {
		t.Fatal("retry revived a canceled request", retry)
	}
	info := responseJSON[InfoResponse](t, f.request(t, "GET", "/api/v1/info", "", nil, 200))
	if info.APIVersion.Minor != 3 || !strings.Contains(jsonBody(t, info.Capabilities), "connections.lifecycle") {
		t.Fatal("missing lifecycle capability", info)
	}
}

func TestConnectionHTTPPermissions(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	request, session := f.seedSession(t, 0, time.Now().Add(-2*time.Minute))
	pending := f.create(t, connectionHTTPInput(1))
	cookie, _ := f.exchange(t, f.ticket(t))
	for _, operation := range []struct{ method, path, body string }{
		{"POST", "/api/v1/connections", jsonBody(t, connectionHTTPInput(2))},
		{"GET", "/api/v1/connections/" + request.ID, ""},
		{"POST", "/api/v1/connections/" + pending.ID + "/cancel", "{}"},
		{"GET", "/api/v1/sessions/" + session.ID, ""},
		{"POST", "/api/v1/sessions/" + session.ID + "/close", "{}"},
	} {
		for _, headers := range []map[string]string{nil, browserHeaders(cookie), {"Authorization": "Bearer wrong"}, {"Authorization": "Bearer wrong", "Cookie": cookie.Name + "=" + cookie.Value, "Origin": Origin}} {
			f.request(t, operation.method, operation.path, operation.body, headers, 401)
		}
		for _, headers := range []map[string]string{{"Authorization": "Bearer " + serviceFixture, "Origin": "https://example.com"}, {"Authorization": "Bearer " + serviceFixture, "Host": "localhost:18765"}} {
			f.request(t, operation.method, operation.path, operation.body, headers, 403)
		}
		req := httptest.NewRequest(operation.method, Origin+operation.path, strings.NewReader(operation.body))
		req.Header["Authorization"] = []string{"Bearer " + serviceFixture, "Bearer " + serviceFixture}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal("duplicate credentials accepted", w.Code)
		}
	}
	current, err := f.database.GetSession(context.Background(), session.ID)
	if err != nil || !reflect.DeepEqual(current, session) {
		t.Fatal("unauthorized requests changed Session", current, err)
	}
	w := f.request(t, "GET", "/api/v1/connections/"+pending.ID, "", controlHeaders(), 200)
	if responseJSON[ConnectionResponse](t, w).State != "pending" {
		t.Fatal("unauthorized cancellation committed")
	}
	for _, path := range []string{"/api/v1/connections/" + pending.ID + "/complete", "/api/v1/connections/" + pending.ID + "/reject", "/api/v1/sessions"} {
		f.request(t, "POST", path, "{}", controlHeaders(), 404)
	}
}

func TestConnectionHTTPValidation(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	valid := jsonBody(t, connectionHTTPInput(0))
	for _, body := range []string{"", "null", "[]", "{}", valid + "{}", strings.TrimSuffix(valid, "}") + `,"state":"succeeded"}`, strings.TrimSuffix(valid, "}") + `,"expiresAt":"2099-01-01T00:00:00Z"}`} {
		f.request(t, "POST", "/api/v1/connections", body, controlHeaders(), 400)
	}
	for _, chain := range []string{"", "-1", "+71", "1.5", "1e3", " 71", "0x", "0xg", "0X47"} {
		input := connectionHTTPInput(0)
		input.ChainID = chain
		f.request(t, "POST", "/api/v1/connections", jsonBody(t, input), controlHeaders(), 400)
	}
	f.request(t, "POST", "/api/v1/connections", strings.Replace(valid, `"chainId":"71"`, `"chainId":71`, 1), controlHeaders(), 400)
	for _, field := range []string{"id", "client"} {
		input := connectionHTTPInput(0)
		if field == "id" {
			input.ID = strings.Repeat("A", 32)
		} else {
			input.ClientName = "  "
		}
		f.request(t, "POST", "/api/v1/connections", jsonBody(t, input), controlHeaders(), 400)
	}
	f.request(t, "POST", "/api/v1/connections", valid+strings.Repeat(" ", MaxConnectionBody-len(valid)+1), controlHeaders(), 413)
	f.request(t, "POST", "/api/v1/connections", valid, map[string]string{"Authorization": "Bearer " + serviceFixture, "Content-Type": "text/plain"}, 415)
	f.request(t, "POST", "/api/v1/connections?x=1", valid, controlHeaders(), 400)
	f.request(t, "POST", "/api/v1/connections", valid+strings.Repeat(" ", MaxConnectionBody-len(valid)), controlHeaders(), 200)
	for _, base := range []string{"connections", "sessions"} {
		for _, id := range []string{"bad", strings.Repeat("A", 32)} {
			f.request(t, "GET", "/api/v1/"+base+"/"+id, "", controlHeaders(), 400)
		}
		path := "/api/v1/" + base + "/" + strings.Repeat("f", 32)
		w := f.request(t, "GET", path, "", controlHeaders(), 404)
		if !strings.HasSuffix(responseJSON[ErrorResponse](t, w).Code, "_not_found") {
			t.Fatal("missing resource code")
		}
		f.request(t, "GET", path+"?after=x", "", controlHeaders(), 400)
		f.request(t, "GET", path, "{}", controlHeaders(), 400)
		f.request(t, "DELETE", path, "", controlHeaders(), 405)
		action := "/cancel"
		if base == "sessions" {
			action = "/close"
		}
		f.request(t, "POST", path+action, "{}", controlHeaders(), 404)
		f.request(t, "POST", path+action, `{"force":true}`, controlHeaders(), 400)
		f.request(t, "POST", path+action, strings.Repeat(" ", 1025), controlHeaders(), 413)
		f.request(t, "POST", path+action+"?x=1", "{}", controlHeaders(), 400)
		w = f.request(t, "GET", path+action, "", controlHeaders(), 405)
		if w.Header().Get("Allow") != "POST" {
			t.Fatal("missing allowed method")
		}
	}
}

func TestConnectionHTTPSessionHistory(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	request, before := f.seedSession(t, 0, time.Now().Add(-2*time.Minute))
	w := f.request(t, "GET", "/api/v1/connections/"+request.ID, "", controlHeaders(), 200)
	connection := responseJSON[ConnectionResponse](t, w)
	if connection.State != "succeeded" || connection.SessionID != before.ID {
		t.Fatal(connection)
	}
	unchanged, err := f.database.GetSession(context.Background(), before.ID)
	if err != nil || !reflect.DeepEqual(unchanged, before) {
		t.Fatal("request query renewed Session", unchanged, err)
	}
	w = f.request(t, "GET", "/api/v1/sessions/"+before.ID, "", controlHeaders(), 200)
	active := responseJSON[SessionResponse](t, w)
	if active.State != "active" || active.LastAccessAt <= timestamp(before.LastAccessAt) || active.Account != before.Account || active.Wallet.RDNS != before.Wallet.RDNS || active.ChainID != request.ChainID {
		t.Fatal(active)
	}
	w = f.request(t, "POST", "/api/v1/sessions/"+before.ID+"/close", "{}", controlHeaders(), 200)
	closed := responseJSON[SessionResponse](t, w)
	if closed.State != "closed" || closed.ClosedAt == "" {
		t.Fatal(closed)
	}
	f.database.Close()
	f = newConnectionHTTPFixture(t, f.dir)
	for _, method := range []string{"GET", "POST"} {
		path := "/api/v1/sessions/" + before.ID
		if method == "POST" {
			path += "/close"
		}
		w = f.request(t, method, path, "", controlHeaders(), 200)
		if replay := responseJSON[SessionResponse](t, w); !reflect.DeepEqual(replay, closed) {
			t.Fatal("closed record revived or changed", replay)
		}
	}
	f.request(t, "POST", "/api/v1/connections/"+request.ID+"/cancel", "{}", controlHeaders(), 409)
	w = f.request(t, "GET", "/api/v1/connections/"+request.ID, "", controlHeaders(), 200)
	if !reflect.DeepEqual(responseJSON[ConnectionResponse](t, w), connection) {
		t.Fatal("closure changed original connection result")
	}
	_, expired := f.seedSession(t, 1, time.Now().Add(-10*time.Minute))
	w = f.request(t, "GET", "/api/v1/sessions/"+expired.ID, "", controlHeaders(), 200)
	history := responseJSON[SessionResponse](t, w)
	if history.State != "closed" || history.ClosedAt != timestamp(expired.LastAccessAt.Add(app.SessionIdleLifetime)) || history.LastAccessAt != timestamp(expired.LastAccessAt) {
		t.Fatal("idle deadline reset", history)
	}
}

func TestConnectionHTTPCancelAndExpiry(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	pending := f.create(t, connectionHTTPInput(0))
	_, grant := f.exchange(t, f.ticket(t))
	input := app.ConnectionExecutionInput{ConnectionID: pending.ID, ID: fmt.Sprintf("%032x", 500)}
	browser := app.BrowserGrant{ID: grant.ID}
	if _, err := f.core.ClaimExecution(context.Background(), browser, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.BeginCall(context.Background(), browser, input, fmt.Sprintf("%032x", 600), ""); err != nil {
		t.Fatal(err)
	}
	w := f.request(t, "POST", "/api/v1/connections/"+pending.ID+"/cancel", "{}", controlHeaders(), 200)
	canceled := responseJSON[ConnectionResponse](t, w)
	if canceled.State != "canceled" || canceled.FinishedAt == "" {
		t.Fatal(canceled)
	}
	w = f.request(t, "POST", "/api/v1/connections/"+pending.ID+"/cancel", "", controlHeaders(), 200)
	if !reflect.DeepEqual(responseJSON[ConnectionResponse](t, w), canceled) {
		t.Fatal("cancel retry reset result")
	}
	execution, err := f.database.GetConnectionExecution(context.Background(), pending.ID)
	if err != nil || execution.Phase != app.ExecutionInterruptedCalling {
		t.Fatal("cancel released unknown wallet wait", execution, err)
	}
	for index, action := range []string{"GET", "POST"} {
		at := time.Now().UTC().Truncate(time.Millisecond).Add(-10 * time.Minute)
		request := app.ConnectionRequest{ID: connectionHTTPInput(index + 1).ID, ClientName: "Old CLI", ChainID: "0x47", State: app.ConnectionPending, CreatedAt: at, ExpiresAt: at.Add(app.ConnectionRequestLifetime)}
		if err := f.database.CreateConnectionRequest(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/connections/" + request.ID
		if action == "POST" {
			f.request(t, action, path+"/cancel", "", controlHeaders(), 409)
		}
		w = f.request(t, "GET", path, "", controlHeaders(), 200)
		result := responseJSON[ConnectionResponse](t, w)
		if result.State != "expired" || result.FinishedAt != timestamp(request.ExpiresAt) || result.SessionID != "" {
			t.Fatal("deadline lost or Session created", result)
		}
	}
}

func TestConnectionHTTPStorageFailure(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	request, session := f.seedSession(t, 0, time.Now().Add(-2*time.Minute))
	pending := f.create(t, connectionHTTPInput(1))
	f.database.Close()
	for _, operation := range []struct{ method, path, body string }{
		{"POST", "/api/v1/connections", jsonBody(t, connectionHTTPInput(2))},
		{"GET", "/api/v1/connections/" + request.ID, ""},
		{"POST", "/api/v1/connections/" + pending.ID + "/cancel", "{}"},
		{"GET", "/api/v1/sessions/" + session.ID, ""},
		{"POST", "/api/v1/sessions/" + session.ID + "/close", "{}"},
	} {
		w := f.request(t, operation.method, operation.path, operation.body, controlHeaders(), 500)
		if result := responseJSON[ErrorResponse](t, w); result.Code != "storage_error" || strings.Contains(w.Body.String(), f.dir) || strings.Contains(w.Body.String(), "sql:") {
			t.Fatal("internal error disclosed", result)
		}
	}
	f = newConnectionHTTPFixture(t, f.dir)
	after, err := f.database.GetSession(context.Background(), session.ID)
	if err != nil || !reflect.DeepEqual(after, session) {
		t.Fatal("failed access/close changed history", after, err)
	}
	if result := f.create(t, connectionHTTPInput(1)); !reflect.DeepEqual(result, pending) {
		t.Fatal("failed cancel committed", result)
	}
	f.create(t, connectionHTTPInput(2))
}

func TestConnectionHTTPConcurrentRetry(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	server := httptest.NewServer(f.handler)
	defer server.Close()
	for _, conflicting := range []bool{false, true} {
		t.Run(fmt.Sprint(conflicting), func(t *testing.T) {
			input := connectionHTTPInput(0)
			if conflicting {
				input.ID = connectionHTTPInput(1).ID
			}
			type outcome struct {
				status int
				body   []byte
				err    error
			}
			results := make(chan outcome, 8)
			for index := range 8 {
				copy := input
				if conflicting {
					copy.Intent = fmt.Sprint(index)
				}
				body := jsonBody(t, copy)
				go func() {
					req, _ := http.NewRequest("POST", server.URL+"/api/v1/connections", strings.NewReader(body))
					req.Host = Address
					req.Header.Set("Authorization", "Bearer "+serviceFixture)
					req.Header.Set("Content-Type", "application/json")
					response, err := server.Client().Do(req)
					if err != nil {
						results <- outcome{err: err}
						return
					}
					defer response.Body.Close()
					data, err := io.ReadAll(response.Body)
					results <- outcome{response.StatusCode, data, err}
				}()
			}
			accepted := 0
			var winner ConnectionResponse
			for range 8 {
				result := <-results
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.status == 200 {
					var current ConnectionResponse
					if err := json.Unmarshal(result.body, &current); err != nil {
						t.Fatal(err)
					}
					if accepted > 0 && !reflect.DeepEqual(winner, current) {
						t.Fatal("transport retries returned different records")
					}
					winner = current
					accepted++
				} else if !conflicting || result.status != 409 {
					t.Fatal("unexpected concurrent response", result.status, string(result.body))
				}
			}
			want := 8
			if conflicting {
				want = 1
			}
			if accepted != want {
				t.Fatal("wrong successful create count", accepted, want)
			}
			w := f.request(t, "GET", "/api/v1/connections/"+input.ID, "", controlHeaders(), 200)
			if !reflect.DeepEqual(responseJSON[ConnectionResponse](t, w), winner) {
				t.Fatal("stored winner differs")
			}
		})
	}
}
