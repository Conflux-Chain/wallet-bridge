//go:build windows || darwin

package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestBrowserDecisionHTTPHandoffAndConsent(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	first, _ := f.exchange(t, f.ticket(t))
	other, _ := f.exchange(t, f.ticket(t))
	id, callID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	f.execution(t, first, r.ID, "claim", id, "", 200)
	path := browserConnectionPath(r.ID) + "/execution/"
	begin := ConnectionBeginCallRequest{ID: id, CallID: callID}
	returned := ConnectionReturnCallRequest{ID: id, CallID: callID}
	consent := ConnectionConsentRequest{ID: id, Wallet: WalletResponse{Kind: "eip6963", Name: "Test", RDNS: "test.wallet"}, Account: "0x" + strings.Repeat("AB", 20), ChainID: "71"}
	headers := browserHeaders(first)
	for _, action := range []struct {
		path string
		body any
	}{
		{"begin", begin}, {"return", returned}, {"complete", consent}, {"reject", ConnectionExecutionRequest{ID: id}},
	} {
		body := jsonBody(t, action.body)
		f.request(t, "POST", path+action.path, body, controlHeaders(), 403)
		f.request(t, "POST", path+action.path, body, map[string]string{"Origin": Origin, "Authorization": "Bearer " + serviceFixture}, 401)
		f.request(t, "POST", path+action.path, body, browserHeaders(other), 409)
		f.request(t, "POST", path+action.path, body, map[string]string{"Cookie": first.String()}, 403)
		f.request(t, "POST", path+action.path, `{"browserGrantId":"unexpected"}`, headers, 400)
		f.request(t, "POST", path+action.path+"?x=1", body, headers, 400)
	}
	beginCall := func(input ConnectionBeginCallRequest) ConnectionCallPermitResponse {
		t.Helper()
		return responseJSON[ConnectionCallPermitResponse](t, f.request(t, "POST", path+"begin", jsonBody(t, input), headers, 200))
	}
	if !beginCall(begin).Invoke || beginCall(begin).Invoke {
		t.Fatal("retry did not preserve single wallet invocation")
	}
	f.request(t, "POST", path+"complete", jsonBody(t, consent), headers, 409)
	f.request(t, "POST", path+"return", jsonBody(t, returned), headers, 200)
	f.request(t, "POST", path+"return", jsonBody(t, returned), headers, 200)
	// A settled wallet prompt (including user rejection) does not reject
	// the bridge request or extend its original five-minute deadline.
	pending := responseJSON[ConnectionResponse](t, f.request(t, "GET", "/api/v1/connections/"+r.ID, "", controlHeaders(), 200))
	if pending != r {
		t.Fatal("wallet return changed pending client request", pending)
	}
	next := ConnectionBeginCallRequest{ID: id, CallID: strings.Repeat("c", 32)}
	f.request(t, "POST", path+"begin", jsonBody(t, next), headers, 409)
	next.PreviousCallID = callID
	if !beginCall(next).Invoke {
		t.Fatal("missing permit for next wallet call")
	}
	f.request(t, "POST", path+"begin", jsonBody(t, begin), headers, 409)
	f.request(t, "POST", path+"return", jsonBody(t, ConnectionReturnCallRequest{ID: id, CallID: next.CallID}), headers, 200)
	wrong := consent
	wrong.ChainID = "1"
	f.request(t, "POST", path+"complete", jsonBody(t, wrong), headers, 400)
	complete := func() ConnectionResponse {
		t.Helper()
		return responseJSON[ConnectionResponse](t, f.request(t, "POST", path+"complete", jsonBody(t, consent), headers, 200))
	}
	result := complete()
	if result.State != "succeeded" || result.SessionID == "" || result != complete() {
		t.Fatal("consent did not converge", result)
	}
	session := responseJSON[SessionResponse](t, f.request(t, "GET", "/api/v1/sessions/"+result.SessionID, "", controlHeaders(), 200))
	if session.Account != strings.ToLower(consent.Account) || session.Wallet != consent.Wallet || session.ChainID != "0x47" {
		t.Fatal(session)
	}
	f.request(t, "POST", path+"reject", jsonBody(t, ConnectionExecutionRequest{ID: id}), headers, 409)
}

func TestBrowserDecisionHTTPReplacedExecution(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	r := f.create(t, connectionHTTPInput(0))
	cookie, _ := f.exchange(t, f.ticket(t))
	id, replacement, callID := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	path := browserConnectionPath(r.ID) + "/execution/"
	post := func(action string, input any, status int) {
		t.Helper()
		f.request(t, http.MethodPost, path+action, jsonBody(t, input), browserHeaders(cookie), status)
	}
	f.execution(t, cookie, r.ID, "claim", id, "", 200)
	post("begin", ConnectionBeginCallRequest{ID: id, CallID: callID}, 200)
	f.execution(t, cookie, r.ID, "interrupt", id, "", 200)
	f.execution(t, cookie, r.ID, "recover", replacement, id, 200)
	post("return", ConnectionReturnCallRequest{ID: id, CallID: callID}, 409)
	post("complete", ConnectionConsentRequest{ID: id, Wallet: WalletResponse{Kind: "legacy", Name: "Test"}, Account: "0x" + strings.Repeat("ab", 20), ChainID: "71"}, 409)
	post("reject", ConnectionExecutionRequest{ID: id}, 409)
	post("reject", ConnectionExecutionRequest{ID: replacement}, 200)
	post("reject", ConnectionExecutionRequest{ID: replacement}, 200)
	final := responseJSON[ConnectionResponse](t, f.request(t, "GET", "/api/v1/connections/"+r.ID, "", controlHeaders(), 200))
	if final.State != "rejected" || final.SessionID != "" {
		t.Fatal(final)
	}
}
