//go:build windows || darwin

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (f connectionHTTPFixture) reviewCommand(t *testing.T, cookie *http.Cookie, path string, cmd TransactionReviewCommand, want int) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, "POST", path, jsonBody(t, cmd), browserHeaders(cookie), want)
}

func TestBrowserReviewHTTPPreparation(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, req := seedTransactionForBrowser(t, f, time.Now().Add(-time.Minute))
	first, _ := f.exchange(t, f.ticket(t))
	second, _ := f.exchange(t, f.ticket(t))
	path := "/api/v1/browser/transactions/" + req.ID + "/execution"
	owner, next, call := strings.Repeat("a", 32), strings.Repeat("c", 32), strings.Repeat("d", 32)
	observe := func(phase, recoverID string) {
		t.Helper()
		reply := f.request(t, "GET", path, "", browserHeaders(second), 200)
		status := responseJSON[TransactionReviewStatusResponse](t, reply)
		if status.Phase != phase || status.RecoverFromID != recoverID || strings.Contains(reply.Body.String(), call) ||
			(recoverID == "" && strings.Contains(reply.Body.String(), owner)) {
			t.Fatal("unexpected observer response", reply.Body.String())
		}
	}
	post := func(cookie *http.Cookie, action app.ReviewAction, id, previous, callID string, want int) *httptest.ResponseRecorder {
		t.Helper()
		return f.reviewCommand(t, cookie, path, TransactionReviewCommand{Action: action, ID: id, PreviousID: previous, CallID: callID}, want)
	}
	observe("unclaimed", "")
	post(first, app.ClaimReview, owner, "", "", 200)
	observe("ready", "")
	post(first, app.ClaimReview, next, "", "", 409) // Another tab, same browser grant.
	post(second, app.HeartbeatReview, owner, "", "", 409)
	post(first, app.HeartbeatReview, owner, "", "", 200)
	begin := func() TransactionReviewCommandResponse {
		t.Helper()
		return responseJSON[TransactionReviewCommandResponse](t, post(first, app.BeginPreparationCall, owner, "", call, 200))
	}
	if permit := begin(); !permit.Invoke || permit.Phase != "calling" {
		t.Fatal("first preparation", permit)
	}
	if replay := begin(); replay.Invoke {
		t.Fatal("retry received a second permission", replay)
	}
	observe("calling", "")
	post(first, app.ReturnPreparationCall, owner, "", call, 200)
	observe("ready", "")
	connection := f.create(t, connectionHTTPInput(3))
	blocked := f.execution(t, second, connection.ID, "claim", next, "", 409)
	if responseJSON[ErrorResponse](t, blocked).Code != "wallet_busy" {
		t.Fatal("another request acquired the wallet during review")
	}
	newCall := strings.Repeat("e", 32)
	post(first, app.BeginPreparationCall, owner, "", newCall, 409)
	post(first, app.BeginPreparationCall, owner, call, newCall, 200)
	post(first, app.InterruptReview, owner, "", "", 200)
	observe("interrupted_calling", owner)
	post(second, app.RecoverReview, next, owner, "", 200)
	post(second, app.RecoverReview, next, owner, "", 200)
	post(first, app.ReturnPreparationCall, owner, "", newCall, 409)
	observe("ready", "")
	if s, err := f.database.GetSession(t.Context(), session.ID); err != nil || !s.LastAccessAt.Equal(req.CreatedAt) {
		t.Fatal("coordination renewed the Session", err)
	}
}

func TestBrowserReviewHTTPAfterHandoff(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, req := seedTransactionForBrowser(t, f, time.Now())
	cookie, grant := f.exchange(t, f.ticket(t))
	path := "/api/v1/browser/transactions/" + req.ID + "/execution"
	ref := app.TransactionReviewRef{TransactionID: req.ID, ID: strings.Repeat("a", 32), BrowserGrantID: grant.ID}
	f.reviewCommand(t, cookie, path, TransactionReviewCommand{Action: app.ClaimReview, ID: ref.ID}, 200)
	send := app.TransactionSendInput{CallID: strings.Repeat("c", 32), Binding: app.ConnectionBinding{Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID}}
	if payload, err := f.database.GrantTransactionSend(t.Context(), ref, send); err != nil || payload == nil {
		t.Fatal("seed handoff", err)
	}
	f.reviewCommand(t, cookie, path, TransactionReviewCommand{Action: app.InterruptReview, ID: ref.ID}, 200)
	other, _ := f.exchange(t, f.ticket(t))
	page := f.request(t, "GET", path, "", browserHeaders(other), 200)
	if responseJSON[TransactionReviewStatusResponse](t, page).Phase != "interrupted_calling" || strings.Contains(page.Body.String(), ref.ID) || strings.Contains(page.Body.String(), send.CallID) {
		t.Fatal("unexpected handed-off observation", page.Body.String())
	}
	recovery := TransactionReviewCommand{Action: app.RecoverReview, ID: strings.Repeat("d", 32), PreviousID: ref.ID}
	f.reviewCommand(t, other, path, recovery, 409)
}

func TestBrowserReviewHTTPAccess(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, req := seedTransactionForBrowser(t, f, time.Now())
	cookie, _ := f.exchange(t, f.ticket(t))
	path := "/api/v1/browser/transactions/" + req.ID + "/execution"
	id := strings.Repeat("a", 32)
	body := jsonBody(t, TransactionReviewCommand{Action: app.ClaimReview, ID: id})
	headers := browserHeaders(cookie)
	f.request(t, "GET", path, "", controlHeaders(), 401)
	f.request(t, "GET", path+"?test=1", "", headers, 400)
	f.request(t, "GET", "/api/v1/browser/transactions/"+id+"/execution", "", headers, 404)
	f.request(t, "DELETE", path, "", headers, 405)
	f.request(t, "POST", path, body, map[string]string{"Origin": Origin, "Authorization": "Bearer " + serviceFixture}, 401)
	f.request(t, "POST", path, body, map[string]string{"Cookie": headers["Cookie"]}, 403)
	f.request(t, "POST", path, body, map[string]string{"Cookie": headers["Cookie"], "Origin": "https://example.com"}, 403)
	f.request(t, "POST", path, strings.Repeat(" ", MaxControlBody+1), headers, 413)
	for _, cmd := range []TransactionReviewCommand{
		{Action: "send", ID: id}, {Action: app.ClaimReview, ID: "bad"}, {Action: app.ClaimReview, ID: id, CallID: id},
		{Action: app.RecoverReview, ID: id}, {Action: app.BeginPreparationCall, ID: id}, {Action: app.ReturnPreparationCall, ID: id},
	} {
		f.reviewCommand(t, cookie, path, cmd, 400)
	}
	authorization, err := app.NewBrowserAuthorization(revokeAfterAuthenticationStore{f.database})
	if err != nil {
		t.Fatal(err)
	}
	f.handler = testHandlerWithCores(t, func() { t.Error("unexpected stop") }, authorization, f.core, f.database)
	f.request(t, "POST", path, body, headers, 401)
	if review, err := f.database.GetTransactionReview(t.Context(), req.ID); err != nil || review != nil {
		t.Fatal("invalid or revoked request claimed review", review, err)
	}
}
