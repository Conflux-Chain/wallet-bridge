//go:build windows || darwin

package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const activityPath = "/api/v1/browser/activity"

func TestBrowserActivityPagination(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	at := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	session, tx := seedTransactionForBrowser(t, f, at)
	// Both kinds share a timestamp and an ID at a page boundary.
	tied := app.ConnectionRequest{ID: tx.ID, ClientName: "Codex", ChainID: "0x1", State: app.ConnectionPending,
		CreatedAt: at, ExpiresAt: at.Add(app.ConnectionRequestLifetime)}
	if err := f.database.CreateConnectionRequest(t.Context(), tied); err != nil {
		t.Fatal(err)
	}
	cookie, _ := f.exchange(t, f.ticket(t))
	read := func(query string) ActivityListResponse {
		t.Helper()
		return responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath+query, "", browserHeaders(cookie), 200))
	}
	want := []ActivityResponse{
		{Kind: "transaction", ID: tx.ID, ClientName: tx.ClientName, ChainID: session.ChainID, CreatedAt: timestamp(at), State: "pending", SessionID: session.ID},
		{Kind: "connection", ID: tied.ID, ClientName: tied.ClientName, ChainID: tied.ChainID, CreatedAt: timestamp(at), State: "pending"},
		{Kind: "connection", ID: session.ConnectionID, ClientName: "Historical CLI", ChainID: session.ChainID,
			CreatedAt: timestamp(at.Add(-time.Minute)), State: "succeeded", SessionID: session.ID, Removable: true},
	}
	all := read("")
	if !slices.Equal(all.Activities, want) || all.NextAfter != "" {
		t.Fatalf("activity list: %+v", all)
	}
	first := read("?limit=1")
	if !slices.Equal(first.Activities, want[:1]) || first.NextAfter == "" {
		t.Fatal("first page", first)
	}
	newer := connectionHTTPInput(42)
	f.create(t, newer)
	f.create(t, newer)
	second := read("?limit=2&after=" + first.NextAfter)
	if !slices.Equal(second.Activities, want[1:]) || second.NextAfter != "" {
		t.Fatal("older page after insert and retry", second)
	}
	if latest := read(""); len(latest.Activities) != 4 || latest.Activities[0].ID != newer.ID {
		t.Fatal("newest request or retry", latest)
	}
	if current, err := f.database.GetSession(t.Context(), session.ID); err != nil || !current.LastAccessAt.Equal(at) {
		t.Fatal("history read renewed Session", current, err)
	}
}

func TestBrowserActivityLateResult(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	session, tx := seedTransactionForBrowser(t, f, time.Now().Add(-time.Minute))
	newer := f.create(t, connectionHTTPInput(22))
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	path := "/api/v1/browser/transactions/" + tx.ID
	execution, call := strings.Repeat("c", 32), strings.Repeat("d", 32)
	f.reviewCommand(t, cookie, path+"/execution", TransactionReviewCommand{Action: app.ClaimReview, ID: execution}, 200)
	send := TransactionSendRequest{ConnectionConsentRequest: ConnectionConsentRequest{ID: execution, Wallet: walletResponse(session.Wallet),
		Account: session.Account, ChainID: session.ChainID}, CallID: call}
	f.request(t, "POST", path+"/send", jsonBody(t, send), headers, 200)
	check := func(state, hash string) {
		t.Helper()
		result := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath, "", headers, 200))
		rows := result.Activities
		if len(rows) != 3 || rows[0].ID != newer.ID || rows[1].ID != tx.ID || rows[1].State != state ||
			rows[1].TxHash != hash || rows[2].SessionID != session.ID {
			t.Fatalf("history after %s: %+v", state, rows)
		}
	}
	check("awaiting_wallet", "")
	remove := jsonBody(t, ActivityRemoveRequest{Kind: app.WalletTransaction, ID: tx.ID})
	f.request(t, "POST", activityPath+"/remove", remove, headers, 409)
	ref := ConnectionReturnCallRequest{ID: execution, CallID: call}
	code := 4900
	f.request(t, "POST", path+"/result", jsonBody(t, TransactionWalletReport{ConnectionReturnCallRequest: ref, ErrorCode: &code}), headers, 200)
	f.request(t, "POST", "/api/v1/sessions/"+session.ID+"/close", "", controlHeaders(), 200)
	check("uncertain", "")
	f.request(t, "POST", activityPath+"/remove", remove, headers, 409)
	hash := "0x" + strings.Repeat("ab", 32)
	body := jsonBody(t, TransactionWalletReport{ConnectionReturnCallRequest: ref, TxHash: &hash})
	f.request(t, "POST", path+"/result", body, headers, 200)
	f.request(t, "POST", path+"/result", body, headers, 200)
	check("sent", hash)
	f.request(t, "POST", activityPath+"/remove", remove, headers, 204)
	f.request(t, "POST", path+"/result", body, headers, 200)
	rows := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath, "", headers, 200)).Activities
	if len(rows) != 2 || rows[0].ID != newer.ID || rows[1].ID != session.ConnectionID {
		t.Fatal("report replay restored a removed activity", rows)
	}
	result := responseJSON[TransactionResponse](t, f.request(t, "GET", transactionsPath+"/"+tx.ID, "", controlHeaders(), 200))
	if result.TxHash != hash || result.State != "sent" {
		t.Fatal("removed activity lost wallet result", result)
	}
}

func TestBrowserActivityRemove(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, tx := seedTransactionForBrowser(t, f, time.Now().Add(-time.Minute))
	input := connectionHTTPInput(37)
	input.ID = tx.ID
	f.create(t, input)
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	path := activityPath + "/remove"
	body := jsonBody(t, ActivityRemoveRequest{Kind: app.WalletConnection, ID: input.ID})
	f.request(t, "POST", path, body, headers, 409)
	canceled := responseJSON[ConnectionResponse](t, f.request(t, "POST", "/api/v1/connections/"+input.ID+"/cancel", "{}", controlHeaders(), 200))
	f.request(t, "POST", path, body, controlHeaders(), 403)
	f.request(t, "POST", path, body, map[string]string{"Origin": Origin}, 401)
	f.request(t, "POST", path, body, map[string]string{"Cookie": headers["Cookie"]}, 403)
	f.request(t, "GET", path, "", headers, 405)
	f.request(t, "POST", path, `{"kind":"all"}`, headers, 400)
	f.request(t, "POST", path, jsonBody(t, ActivityRemoveRequest{Kind: app.WalletConnection, ID: strings.Repeat("e", 32)}), headers, 404)
	f.request(t, "POST", path, body, headers, 204)

	f.database.Close()
	f = newConnectionHTTPFixture(t, f.dir)
	f.request(t, "POST", path, body, headers, 204)
	if got := f.create(t, input); got != canceled {
		t.Fatal("creation retry after restart", got)
	}
	rows := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath, "", headers, 200)).Activities
	if len(rows) != 2 || rows[0].Kind != "transaction" || rows[0].ID != tx.ID || rows[0].Removable {
		t.Fatal("wrong activity hidden or reappeared", rows)
	}
}

func TestBrowserActivityAccessAndExpiry(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	empty := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath, "", headers, 200))
	if empty.Activities == nil || len(empty.Activities) != 0 || empty.NextAfter != "" {
		t.Fatal("empty page", empty)
	}
	at := time.Now().UTC().Truncate(time.Millisecond).Add(-2 * app.TransactionRequestLifetime)
	_, tx := seedTransactionForBrowser(t, f, at)
	expired := app.ConnectionRequest{ID: tx.ID, ClientName: "Codex", ChainID: "0x1", State: app.ConnectionPending,
		CreatedAt: at, ExpiresAt: at.Add(app.ConnectionRequestLifetime)}
	if err := f.database.CreateConnectionRequest(t.Context(), expired); err != nil {
		t.Fatal(err)
	}
	rows := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath, "", headers, 200)).Activities
	if len(rows) != 3 || rows[0].State != "expired" || rows[1].State != "expired" || rows[2].State != "succeeded" {
		t.Fatal("effective expiry before maintenance", rows)
	}
	for _, row := range rows {
		if !row.Removable {
			t.Fatal("finished activity is not removable", row)
		}
		f.request(t, "POST", activityPath+"/remove", jsonBody(t, ActivityRemoveRequest{Kind: app.WalletRequestKind(row.Kind), ID: row.ID}), headers, 204)
	}
	if got := responseJSON[ActivityListResponse](t, f.request(t, "GET", activityPath+"?limit=1", "", headers, 200)); len(got.Activities) != 0 || got.NextAfter != "" {
		t.Fatal("removed activity still paginated", got)
	}
	for _, query := range []string{"?limit=0", "?limit=51", "?limit=1&limit=2", "?unknown=1", "?after=", "?after=1.x.connection", "?after=bad%zz"} {
		f.request(t, "GET", activityPath+query, "", headers, 400)
	}
	f.request(t, "GET", activityPath, "{}", headers, 400)
	f.request(t, "POST", activityPath, "", headers, 405)
	f.request(t, "GET", activityPath, "", nil, 401)
	f.request(t, "GET", activityPath, "", controlHeaders(), 401)
	f.request(t, "GET", activityPath, "", map[string]string{"Cookie": headers["Cookie"], "Origin": "https://example.com"}, 403)
	f.request(t, "POST", "/api/v1/browser/logout", "{}", headers, 200)
	f.request(t, "GET", activityPath, "", headers, 401)
}
