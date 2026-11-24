//go:build windows || darwin

package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

func TestTransactionDecisionHTTPQueuedRejection(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, first := seedTransactionForBrowser(t, f, time.Now())
	owner, _ := f.exchange(t, f.ticket(t))
	observer, _ := f.exchange(t, f.ticket(t))
	root := "/api/v1/browser/transactions/"
	execution := strings.Repeat("a", 32)
	f.reviewCommand(t, owner, root+first.ID+"/execution", TransactionReviewCommand{Action: app.ClaimReview, ID: execution}, 200)
	f.reviewCommand(t, owner, root+first.ID+"/execution", TransactionReviewCommand{Action: app.BeginPreparationCall, ID: execution, CallID: strings.Repeat("f", 32)}, 200)
	input, _ := transactionHTTPInput(session)
	queued := responseJSON[TransactionResponse](t, f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 200))
	reject := func() TransactionResponse {
		t.Helper()
		return responseJSON[TransactionResponse](t, f.request(t, "POST", root+queued.ID+"/reject", "", browserHeaders(observer), 200))
	}
	// Roll back both the queue removal and decision if activity recording fails.
	db := browserCoordinationSQL(t, f.dir)
	if _, err := db.Exec("CREATE TRIGGER fail_decision BEFORE INSERT ON transaction_results BEGIN SELECT RAISE(ABORT, 'fail'); END"); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", root+queued.ID+"/reject", "{}", browserHeaders(observer), 500)
	if req := responseJSON[TransactionResponse](t, f.request(t, "GET", transactionsPath+"/"+queued.ID, "", controlHeaders(), 200)); req != queued {
		t.Fatal("failed rejection changed queued transaction", req)
	}
	if _, err := db.Exec("DROP TRIGGER fail_decision"); err != nil {
		t.Fatal(err)
	}
	rejected := reject()
	if rejected.State != "rejected" || rejected.HandedOffAt != "" || rejected.FinishedAt == "" || reject() != rejected {
		t.Fatal("queued rejection or replay", rejected)
	}
	if got := responseJSON[TransactionResponse](t, f.request(t, "POST", transactionsPath+"/"+queued.ID+"/cancel", "", controlHeaders(), 200)); got != rejected {
		t.Fatal("client lost the user's rejection", got)
	}
	if active, err := f.database.GetActiveWalletRequest(t.Context()); err != nil || active == nil || active.ID != first.ID || active.Phase != app.ExecutionCalling {
		t.Fatal("rejecting the queued transaction disturbed the open popup", active, err)
	}
}

func TestTransactionDecisionHTTPHandoff(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, req := seedTransactionForBrowser(t, f, time.Now())
	owner, _ := f.exchange(t, f.ticket(t))
	other, _ := seedBrowserCookie(t, f, 91, time.Now().Add(-time.Minute))
	path := "/api/v1/browser/transactions/" + req.ID
	execution, call := strings.Repeat("a", 32), strings.Repeat("c", 32)
	f.reviewCommand(t, owner, path+"/execution", TransactionReviewCommand{Action: app.ClaimReview, ID: execution}, 200)
	send := TransactionSendRequest{
		ConnectionConsentRequest: ConnectionConsentRequest{ID: execution, Wallet: walletResponse(session.Wallet), Account: session.Account, ChainID: "71"},
		CallID:                   call,
	}
	headers := browserHeaders(owner)
	bad := send
	bad.Account = "0x" + strings.Repeat("ff", 20)
	f.request(t, "POST", path+"/send", jsonBody(t, bad), headers, 400)
	f.request(t, "POST", path+"/send", jsonBody(t, send), browserHeaders(other), 409)
	permit := func() TransactionSendPermit {
		t.Helper()
		return responseJSON[TransactionSendPermit](t, f.request(t, "POST", path+"/send", jsonBody(t, send), headers, 200))
	}
	first, replay := permit(), permit()
	if !first.Invoke || first.Transaction == nil || *first.Transaction != TransactionPayload(req.Transaction) || replay.Invoke || replay.Transaction != nil {
		t.Fatal("send permit or replay", first, replay)
	}
	if got := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/reject", "", browserHeaders(other), 200)); got.State != "awaiting_wallet" {
		t.Fatal("page rejection overtook handoff", got)
	}
	f.request(t, "POST", path+"/acknowledge", "", browserHeaders(other), 409)
	f.reviewCommand(t, owner, path+"/execution", TransactionReviewCommand{Action: app.InterruptReview, ID: execution}, 200)
	ack := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/acknowledge", "", browserHeaders(other), 200))
	if ack.State != "uncertain" || ack.TxHash != "" {
		t.Fatal("manual release changed the outcome", ack)
	}
	f.request(t, "POST", "/api/v1/sessions/"+session.ID+"/close", "", controlHeaders(), 200)
	hash := "0x" + strings.Repeat("12", 32)
	report := TransactionWalletReport{ConnectionReturnCallRequest: ConnectionReturnCallRequest{ID: execution, CallID: call}, TxHash: &hash}
	body := jsonBody(t, report)
	f.request(t, "POST", path+"/result", body, browserHeaders(other), 409)
	f.request(t, "POST", "/api/v1/browser/logout", "", headers, 200)
	f.request(t, "POST", path+"/result", body, headers, 401)
	fresh, _ := f.exchange(t, f.ticket(t))
	headers = browserHeaders(fresh)
	sent := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/result", body, headers, 200))
	if sent.State != "sent" || sent.TxHash != hash {
		t.Fatal("lost late hash after reauthorization", sent)
	}
	if again := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/result", body, headers, 200)); again != sent {
		t.Fatal("report replay changed result", again)
	}
	different := "0x" + strings.Repeat("34", 32)
	report.TxHash = &different
	f.request(t, "POST", path+"/result", jsonBody(t, report), headers, 409)
	if got := responseJSON[TransactionResponse](t, f.request(t, "GET", transactionsPath+"/"+req.ID, "", controlHeaders(), 200)); got != sent {
		t.Fatal("original client cannot read final result", got)
	}
}

func TestTransactionDecisionHTTPAccessAndExpiry(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, req := seedTransactionForBrowser(t, f, time.Now().Add(-app.TransactionRequestLifetime-time.Second))
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	path := "/api/v1/browser/transactions/" + req.ID
	for _, action := range []string{"reject", "send", "result", "acknowledge"} {
		route := path + "/" + action
		f.request(t, "GET", route, "", headers, 405)
		f.request(t, "POST", route, "{}", map[string]string{"Origin": Origin, "Authorization": "Bearer " + serviceFixture}, 401)
		f.request(t, "POST", route, "{}", map[string]string{"Cookie": headers["Cookie"]}, 403)
		f.request(t, "POST", route, "{}", map[string]string{"Origin": "https://example.com", "Cookie": headers["Cookie"]}, 403)
		f.request(t, "POST", route, `{"browserGrantId":"extra"}`, headers, 400)
	}
	f.request(t, "POST", path+"/result", `{"errorCode":4001,"txHash":"0xab"}`, headers, 400)
	f.request(t, "POST", path+"/result", "{}", headers, 400)
	if expired := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/reject", "{}", headers, 200)); expired.State != "expired" || expired.FinishedAt != timestamp(req.ExpiresAt) {
		t.Fatal("page rejection replaced prior expiry", expired)
	}
}

func TestTransactionDecisionHTTPWalletError(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{{0, "uncertain"}, {4001, "rejected"}} {
		t.Run(tc.want, func(t *testing.T) {
			f := newConnectionHTTPFixture(t, "")
			session, req := seedTransactionForBrowser(t, f, time.Now())
			cookie, grant := f.exchange(t, f.ticket(t))
			ref := app.TransactionReviewRef{TransactionID: req.ID, ID: strings.Repeat("e", 32), BrowserGrantID: grant.ID}
			if _, err := f.database.ApplyTransactionReview(t.Context(), ref, app.ReviewCommand{Action: app.ClaimReview}); err != nil {
				t.Fatal(err)
			}
			call := strings.Repeat("a", 32)
			send := app.TransactionSendInput{CallID: call, Binding: app.ConnectionBinding{Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID}}
			if payload, err := f.database.GrantTransactionSend(t.Context(), ref, send); err != nil || payload == nil {
				t.Fatal("seed handoff", err)
			}
			report := TransactionWalletReport{ConnectionReturnCallRequest: ConnectionReturnCallRequest{ID: ref.ID, CallID: call}, ErrorCode: &tc.code}
			path := "/api/v1/browser/transactions/" + req.ID + "/result"
			got := responseJSON[TransactionResponse](t, f.request(t, "POST", path, jsonBody(t, report), browserHeaders(cookie), 200))
			if got.State != tc.want || got.HandedOffAt == "" || got.TxHash != "" {
				t.Fatal("unexpected wallet outcome", got)
			}
		})
	}
}

func TestTransactionDecisionConcurrent(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, req := seedTransactionForBrowser(t, f, time.Now())
	_, grant := f.exchange(t, f.ticket(t))
	ref := app.TransactionReviewRef{TransactionID: req.ID, ID: strings.Repeat("e", 32), BrowserGrantID: grant.ID}
	ctx := t.Context()
	if _, err := f.database.ApplyTransactionReview(ctx, ref, app.ReviewCommand{Action: app.ClaimReview}); err != nil {
		t.Fatal(err)
	}
	second, err := store.Open(ctx, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	type sendResult struct {
		payload *app.Transaction
		err     error
	}
	start := make(chan struct{})
	done := make(chan sendResult, 1)
	go func() {
		<-start
		input := app.TransactionSendInput{CallID: strings.Repeat("a", 32), Binding: app.ConnectionBinding{Wallet: session.Wallet, Account: session.Account, ChainID: session.ChainID}}
		payload, err := second.GrantTransactionSend(ctx, ref, input)
		done <- sendResult{payload, err}
	}()
	close(start)
	decision, err := f.database.RejectTransactionRequest(ctx, req.ID, grant.ID)
	send := <-done
	if err != nil {
		t.Fatal(err)
	}
	switch decision.State {
	case app.TransactionRejected:
		if send.payload != nil || !errors.Is(send.err, app.ErrTransactionReviewConflict) || decision.HandedOffAt != nil {
			t.Fatal("send overtook rejection", send, decision)
		}
	case app.TransactionAwaitingWallet:
		if send.payload == nil || send.err != nil || decision.HandedOffAt == nil {
			t.Fatal("rejection overtook send", send, decision)
		}
	default:
		t.Fatal("unexpected concurrent decision", decision)
	}
}
