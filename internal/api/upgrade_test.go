//go:build windows || darwin

package api

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const upgradeStopPath = "/api/v1/control/stop-for-upgrade"

func TestUpgradeStopBusyAndRetry(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	stops := 0
	f.handler.(*handler).config.Stop = func() { stops++ }
	session, tx := seedTransactionForBrowser(t, f, time.Now())
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	f.request(t, "POST", upgradeStopPath, "", headers, 401)
	f.request(t, "GET", upgradeStopPath, "", controlHeaders(), 405)
	f.request(t, "POST", upgradeStopPath, `{"force":true}`, controlHeaders(), 400)

	execution := strings.Repeat("a", 32)
	path := "/api/v1/browser/transactions/" + tx.ID
	claim := TransactionReviewCommand{Action: app.ClaimReview, ID: execution}
	f.reviewCommand(t, cookie, path+"/execution", claim, 200)
	call := strings.Repeat("c", 32)
	send := TransactionSendRequest{
		ConnectionConsentRequest: ConnectionConsentRequest{ID: execution, Wallet: walletResponse(session.Wallet), Account: session.Account, ChainID: session.ChainID},
		CallID:                   call,
	}
	f.request(t, "POST", path+"/send", jsonBody(t, send), headers, 200)
	for _, phase := range []string{"calling", "interrupted_calling"} {
		if phase == "interrupted_calling" {
			f.reviewCommand(t, cookie, path+"/execution", TransactionReviewCommand{Action: app.InterruptReview, ID: execution}, 200)
		}
		busy := responseJSON[UpgradeBusyResponse](t, f.request(t, "POST", upgradeStopPath, "", controlHeaders(), 409))
		if busy.Code != "wallet_busy" || busy.ActiveWalletRequest != (ActiveWalletRequestResponse{Kind: "transaction", ID: tx.ID, Phase: phase}) || stops != 0 {
			t.Fatal(busy, stops)
		}
	}
	queued := f.create(t, connectionHTTPInput(8))
	f.request(t, "POST", path+"/acknowledge", "{}", headers, 200)
	if stops != 0 {
		t.Fatal("wallet release automatically stopped the service")
	}
	f.request(t, "POST", upgradeStopPath, "", controlHeaders(), 202)
	f.request(t, "POST", upgradeStopPath, "", controlHeaders(), 503)
	f.request(t, "POST", "/api/v1/connections", jsonBody(t, connectionHTTPInput(9)), controlHeaders(), 503)
	input, _ := transactionHTTPInput(session)
	f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 503)
	f.execution(t, cookie, queued.ID, "claim", execution, "", 503)
	f.execution(t, cookie, queued.ID, "recover", strings.Repeat("e", 32), execution, 503)
	f.reviewCommand(t, cookie, path+"/execution", claim, 503)
	f.reviewCommand(t, cookie, path+"/execution", TransactionReviewCommand{Action: app.RecoverReview, ID: strings.Repeat("e", 32), PreviousID: execution}, 503)
	f.request(t, "GET", "/api/v1/connections/"+queued.ID, "", controlHeaders(), 200)
	hash := "0x" + strings.Repeat("12", 32)
	report := TransactionWalletReport{ConnectionReturnCallRequest: ConnectionReturnCallRequest{ID: execution, CallID: call}, TxHash: &hash}
	sent := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/result", jsonBody(t, report), headers, 200))
	if sent.TxHash != hash || stops != 1 {
		t.Fatal(sent, stops)
	}
}

func TestUpgradeStopRacesWalletClaim(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	stopped := false
	f.handler.(*handler).config.Stop = func() { stopped = true }
	r := f.create(t, connectionHTTPInput(1))
	cookie, _ := f.exchange(t, f.ticket(t))
	makePost := func(path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", Origin+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, req)
		return w
	}
	body := jsonBody(t, ConnectionExecutionRequest{ID: strings.Repeat("b", 32)})
	var stop, claim *httptest.ResponseRecorder
	var group sync.WaitGroup
	start := make(chan struct{})
	group.Go(func() {
		<-start
		stop = makePost(upgradeStopPath, "", controlHeaders())
	})
	group.Go(func() {
		<-start
		claim = makePost(browserConnectionPath(r.ID)+"/execution/claim", body, browserHeaders(cookie))
	})
	close(start)
	group.Wait()
	if stopped {
		if stop.Code != 202 || claim.Code != 503 {
			t.Fatal(stop.Code, claim.Code)
		}
	} else if stop.Code != 409 || claim.Code != 200 || responseJSON[UpgradeBusyResponse](t, stop).ActiveWalletRequest.ID != r.ID {
		t.Fatal(stop.Code, claim.Code)
	}
}
