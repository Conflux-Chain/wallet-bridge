//go:build windows || darwin

package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const pendingPath = "/api/v1/browser/pending-requests"

func TestBrowserPendingRequests(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	session, tx := seedTransactionForBrowser(t, f, at)
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	makeConnection := func(id string, created time.Time) PendingRequestResponse {
		t.Helper()
		req := app.ConnectionRequest{ID: id, ClientName: "Codex", ChainID: "0x47", State: app.ConnectionPending,
			CreatedAt: created, ExpiresAt: created.Add(app.ConnectionRequestLifetime)}
		if err := f.database.CreateConnectionRequest(ctx, req); err != nil {
			t.Fatal(err)
		}
		return PendingRequestResponse{Kind: "connection", ID: id, ClientName: req.ClientName, ChainID: req.ChainID,
			CreatedAt: timestamp(created), ExpiresAt: timestamp(req.ExpiresAt)}
	}
	// A connection and transaction may share both their ID and creation time.
	tied := makeConnection(tx.ID, at)
	first := makeConnection(strings.Repeat("f", 32), at.Add(-time.Second))
	makeConnection(strings.Repeat("e", 32), at.Add(-app.ConnectionRequestLifetime))
	checkList := func(want ...PendingRequestResponse) {
		t.Helper()
		response := responseJSON[PendingRequestListResponse](t, f.request(t, "GET", pendingPath, "", headers, 200))
		if response.Requests == nil || !slices.Equal(response.Requests, want) {
			t.Fatalf("pending list: got %+v, want %+v", response.Requests, want)
		}
	}
	transaction := PendingRequestResponse{Kind: "transaction", ID: tx.ID, ClientName: tx.ClientName, ChainID: session.ChainID,
		CreatedAt: timestamp(tx.CreatedAt), ExpiresAt: timestamp(tx.ExpiresAt)}
	checkList(first, tied, transaction)
	if active, err := f.database.GetActiveWalletRequest(ctx); err != nil || active != nil {
		t.Fatal("reading the list claimed the wallet", active, err)
	}
	if current, err := f.database.GetSession(ctx, session.ID); err != nil || !current.LastAccessAt.Equal(at) {
		t.Fatal("reading the list changed Session access", current.LastAccessAt, err)
	}
	if items, err := f.database.ListPendingWalletRequests(ctx, tx.ExpiresAt); err != nil || len(items) != 0 {
		t.Fatal("included a request at its deadline", items, err)
	}

	path := "/api/v1/browser/transactions/" + tx.ID
	execution := strings.Repeat("a", 32)
	f.reviewCommand(t, cookie, path+"/execution", TransactionReviewCommand{Action: app.ClaimReview, ID: execution}, 200)
	checkList(first, tied, transaction)
	send := TransactionSendRequest{ConnectionConsentRequest: ConnectionConsentRequest{ID: execution, Wallet: walletResponse(session.Wallet),
		Account: session.Account, ChainID: session.ChainID}, CallID: strings.Repeat("c", 32)}
	f.request(t, "POST", path+"/send", jsonBody(t, send), headers, 200)
	checkList(first, tied)
	for _, item := range []PendingRequestResponse{first, tied} {
		f.request(t, "POST", "/api/v1/connections/"+item.ID+"/cancel", "", controlHeaders(), 200)
	}
	checkList()
}

func TestBrowserPendingRequestsAccess(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	delete(headers, "Origin")
	f.request(t, "GET", pendingPath, "", headers, 200)
	f.request(t, "GET", pendingPath, "", nil, 401)
	f.request(t, "GET", pendingPath, "", controlHeaders(), 401)
	f.request(t, "GET", pendingPath, "", map[string]string{"Cookie": headers["Cookie"], "Origin": "https://example.com"}, 403)
	f.request(t, "GET", pendingPath+"?id=x", "", headers, 400)
	f.request(t, "GET", pendingPath, "{}", headers, 400)
	f.request(t, "POST", pendingPath, "", headers, 405)
	f.request(t, "POST", "/api/v1/browser/logout", "{}", browserHeaders(cookie), 200)
	f.request(t, "GET", pendingPath, "", headers, 401)
}
