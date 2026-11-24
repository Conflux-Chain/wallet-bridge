//go:build windows || darwin

package api

import (
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func seedTransactionForBrowser(t *testing.T, f connectionHTTPFixture, at time.Time) (app.Session, app.TransactionRequest) {
	t.Helper()
	_, session := f.seedSession(t, 0, at.Add(-time.Minute))
	req, err := f.database.CreateTransactionRequest(t.Context(), app.TransactionRequestInput{
		ID: strings.Repeat("b", 32), SessionID: session.ID, ClientName: "Example CLI",
		Fields: map[string]string{"from": session.Account, "chainId": session.ChainID, "to": "0x" + strings.Repeat("ab", 20), "data": "0x0012", "value": "10"},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	return session, req
}

func TestBrowserTransactionHTTPObservation(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	at := time.Now().Add(-time.Minute)
	session, req := seedTransactionForBrowser(t, f, at)
	cookie, grant := seedBrowserCookie(t, f, 1, at)
	path := "/api/v1/browser/transactions/" + req.ID
	headers := browserHeaders(cookie)
	delete(headers, "Origin") // Same-origin navigation may omit Origin.
	read := func() BrowserTransactionResponse {
		t.Helper()
		return responseJSON[BrowserTransactionResponse](t, f.request(t, "GET", path, "", headers, 200))
	}
	page := read()
	client := responseJSON[TransactionResponse](t, f.request(t, "GET", transactionsPath+"/"+req.ID, "", controlHeaders(), 200))
	if page.TransactionResponse != client || page.Intent != "" || page.Transaction.Data != "0x0012" ||
		page.Wallet != (WalletResponse{Kind: "eip6963", Name: "Example Wallet", RDNS: "example.wallet"}) {
		t.Fatal("browser review differs from frozen transaction or selected wallet", page)
	}
	if review, err := f.database.GetTransactionReview(t.Context(), req.ID); err != nil || review != nil {
		t.Fatal("reading claimed the wallet", review, err)
	}
	ref := app.TransactionReviewRef{TransactionID: req.ID, ID: strings.Repeat("a", 32), BrowserGrantID: grant.ID}
	claim, err := f.database.ApplyTransactionReview(t.Context(), ref, app.ReviewCommand{Action: app.ClaimReview})
	if err != nil {
		t.Fatal(err)
	}
	if again := read(); again != page {
		t.Fatal("reopening changed transaction data", again)
	}
	if review, err := f.database.GetTransactionReview(t.Context(), req.ID); err != nil || review == nil || *review != claim.Review {
		t.Fatal("reading changed execution ownership or heartbeat", review, err)
	}
	if stored, err := f.database.GetSession(t.Context(), session.ID); err != nil || !stored.LastAccessAt.Equal(req.CreatedAt) {
		t.Fatal("reading renewed Session", stored, err)
	}
	f.request(t, "POST", "/api/v1/sessions/"+session.ID+"/close", "", controlHeaders(), 200)
	if history := read(); history.State != "canceled" || history.Wallet != page.Wallet || history.Transaction != page.Transaction {
		t.Fatal("closed Session lost transaction history", history)
	}
}

func TestBrowserTransactionHTTPDeadline(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, req := seedTransactionForBrowser(t, f, time.Now().Add(-app.TransactionRequestLifetime-time.Minute))
	cookie, _ := f.exchange(t, f.ticket(t))
	path := "/api/v1/browser/transactions/" + req.ID
	read := responseJSON[BrowserTransactionResponse](t, f.request(t, "GET", path, "", browserHeaders(cookie), 200))
	if read.State != "expired" || read.FinishedAt != timestamp(req.ExpiresAt) || read.ExpiresAt != read.FinishedAt {
		t.Fatal("reading after deadline lost expiry", read)
	}
}

func TestBrowserTransactionHTTPAccess(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, req := seedTransactionForBrowser(t, f, time.Now())
	path := "/api/v1/browser/transactions/" + req.ID
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	f.request(t, "GET", path, "", nil, 401)
	f.request(t, "GET", path, "", controlHeaders(), 401)
	f.request(t, "GET", path, "", map[string]string{"Cookie": headers["Cookie"], "Origin": "https://example.com"}, 403)
	f.request(t, "GET", path, "", map[string]string{"Cookie": headers["Cookie"], "Host": "example.com"}, 403)
	f.request(t, "GET", path+"?id=1", "", headers, 400)
	f.request(t, "GET", path, "{}", headers, 400)
	f.request(t, "POST", path, "", headers, 405)
	f.request(t, "GET", "/api/v1/browser/transactions/invalid", "", headers, 400)
	missing := f.request(t, "GET", "/api/v1/browser/transactions/"+strings.Repeat("e", 32), "", headers, 404)
	if got := responseJSON[ErrorResponse](t, missing); got.Code != "transaction_not_found" {
		t.Fatal(got)
	}
	f.request(t, "POST", "/api/v1/browser/logout", "", headers, 200)
	f.request(t, "GET", path, "", headers, 401)
}
