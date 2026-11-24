//go:build windows || darwin

package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const transactionsPath = "/api/v1/transactions"

func transactionHTTPInput(s app.Session) (map[string]any, map[string]any) {
	fields := map[string]any{"from": s.Account, "to": "0x" + strings.Repeat("ab", 20), "chainId": "71"}
	return map[string]any{"id": fmt.Sprintf("%032x", 7000), "sessionId": s.ID, "clientName": "Example CLI", "transaction": fields}, fields
}

func TestTransactionHTTPLifecycle(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, s := f.seedSession(t, 0, time.Now().Add(-time.Minute))
	input, fields := transactionHTTPInput(s)
	fields["value"] = "9007199254740993"
	create := func() TransactionResponse {
		return responseJSON[TransactionResponse](t, f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 200))
	}
	created := create()
	if created.State != "pending" || created.SessionID != s.ID || created.ClientName != "Example CLI" || created.Intent != "" ||
		created.Transaction.Value != "0x20000000000001" || created.Transaction.Data != "0x" || created.Transaction.ChainID != "0x47" {
		t.Fatal("unexpected frozen request", created)
	}
	path := transactionsPath + "/" + created.ID
	accessed, err := f.database.GetSession(t.Context(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	fields["chainId"], fields["value"], fields["data"] = "0x47", created.Transaction.Value, "0x"
	input["intent"] = ""
	if replay := create(); replay != created {
		t.Fatal("equivalent retry changed original request", replay)
	}
	reply := f.request(t, "GET", path, "", controlHeaders(), 200)
	read := responseJSON[TransactionResponse](t, reply)
	if read != created || responseJSON[map[string]any](t, reply)["intent"] != "" {
		t.Fatal("query differed from creation", read)
	}
	input["intent"] = "changed"
	w := f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 409)
	if responseJSON[ErrorResponse](t, w).Code != "transaction_conflict" {
		t.Fatal("wrong conflict code")
	}
	delete(input, "intent")
	canceled := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/cancel", "", controlHeaders(), 200))
	if canceled.State != "canceled" || canceled.FinishedAt == "" || canceled.ExpiresAt != created.ExpiresAt {
		t.Fatal("cancel", canceled)
	}
	if again := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/cancel", "{}", controlHeaders(), 200)); again != canceled {
		t.Fatal("cancel replay changed result", again)
	}
	after, err := f.database.GetSession(t.Context(), s.ID)
	if err != nil || !after.LastAccessAt.Equal(accessed.LastAccessAt) {
		t.Fatal("poll, retry or cancel renewed Session", after, err)
	}
	f.request(t, "POST", "/api/v1/sessions/"+s.ID+"/close", "", controlHeaders(), 200)
	f.database.Close()
	f = newConnectionHTTPFixture(t, f.dir)
	if replay := create(); replay != canceled {
		t.Fatal("closed Session lost original request", replay)
	}
	input["id"] = fmt.Sprintf("%032x", 7001)
	w = f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 409)
	if responseJSON[ErrorResponse](t, w).Code != "session_closed" {
		t.Fatal("missing Session closure error")
	}
}

func TestTransactionHTTPBoundary(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, s := f.seedSession(t, 0, time.Now().Add(-time.Minute))
	input, fields := transactionHTTPInput(s)
	path := transactionsPath + "/" + input["id"].(string)
	cookie, _ := f.exchange(t, f.ticket(t))
	for _, op := range []struct{ method, path, body string }{
		{"POST", transactionsPath, jsonBody(t, input)}, {"GET", path, ""}, {"POST", path + "/cancel", "{}"},
	} {
		for _, headers := range []map[string]string{nil, browserHeaders(cookie), {"Authorization": "Bearer wrong"}} {
			f.request(t, op.method, op.path, op.body, headers, 401)
		}
		f.request(t, op.method, op.path, op.body, map[string]string{"Authorization": "Bearer " + serviceFixture, "Origin": "https://example.com"}, 403)
	}
	f.request(t, "GET", path+"?data=1", "", controlHeaders(), 400)
	f.request(t, "GET", transactionsPath+"/not-an-id", "", controlHeaders(), 400)
	f.request(t, "GET", transactionsPath, "", controlHeaders(), 405)
	f.request(t, "POST", path+"/cancel", `{"force":true}`, controlHeaders(), 400)
	f.request(t, "POST", transactionsPath, jsonBody(t, input), map[string]string{"Authorization": "Bearer " + serviceFixture, "Content-Type": "text/plain"}, 415)
	delete(input, "clientName")
	f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 400)
	input["clientName"] = "Example CLI"
	for _, tc := range []struct {
		key     string
		value   any
		message string
	}{
		{"type", "0x2", "type"}, {"nonce", 0, "nonce"}, {"gas", 21000, "string"},
		{"value", nil, "string"}, {"gasPrice", "", "gasPrice"}, {"to", "", "deployment"},
		{"chainId", "1", "Session"}, {"maxFeePerGas", "0x1", "together"}, {"extra", "0", "unsupported"},
	} {
		original, present := fields[tc.key]
		fields[tc.key] = tc.value
		w := f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 400)
		if problem := responseJSON[ErrorResponse](t, w); !strings.Contains(problem.Message, tc.message) {
			t.Fatal("missing field diagnosis", tc.key, problem)
		}
		if present {
			fields[tc.key] = original
		} else {
			delete(fields, tc.key)
		}
	}
	if got := responseJSON[ErrorResponse](t, f.request(t, "GET", path, "", controlHeaders(), 404)); got.Code != "transaction_not_found" {
		t.Fatal(got)
	}
	input["sessionId"] = strings.Repeat("f", 32)
	if got := responseJSON[ErrorResponse](t, f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 404)); got.Code != "session_not_found" {
		t.Fatal(got)
	}
	input["sessionId"] = s.ID
	fields["data"] = "0x" + strings.Repeat("ab", 5000)
	body := jsonBody(t, input)
	body += strings.Repeat(" ", MaxTransactionBody-len(body))
	f.request(t, "POST", transactionsPath, body+" ", controlHeaders(), 413)
	w := f.request(t, "POST", transactionsPath, body, controlHeaders(), 200)
	if result := responseJSON[TransactionResponse](t, w); result.Transaction.Data != fields["data"] || result.Transaction.Value != "0x0" || strings.Contains(w.Body.String(), `"gasPrice"`) {
		t.Fatal("frozen data or omitted quantities changed")
	}
}

func TestTransactionHTTPWalletResult(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	_, s := f.seedSession(t, 0, time.Now().Add(-time.Minute))
	input, _ := transactionHTTPInput(s)
	created := responseJSON[TransactionResponse](t, f.request(t, "POST", transactionsPath, jsonBody(t, input), controlHeaders(), 200))
	_, grant := f.exchange(t, f.ticket(t))
	ref := app.TransactionReviewRef{TransactionID: created.ID, ID: strings.Repeat("e", 32), BrowserGrantID: grant.ID}
	if _, err := f.database.ApplyTransactionReview(t.Context(), ref, app.ReviewCommand{Action: app.ClaimReview}, time.Now()); err != nil {
		t.Fatal(err)
	}
	send := app.TransactionSendInput{CallID: strings.Repeat("a", 32), Binding: app.ConnectionBinding{Wallet: s.Wallet, Account: s.Account, ChainID: s.ChainID}}
	if payload, err := f.database.GrantTransactionSend(t.Context(), ref, send); err != nil || payload == nil {
		t.Fatal("handoff", err)
	}
	path := transactionsPath + "/" + created.ID
	handoff := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/cancel", "", controlHeaders(), 200))
	if handoff.State != "awaiting_wallet" || handoff.HandedOffAt == "" {
		t.Fatal("cancel during handoff", handoff)
	}
	f.request(t, "POST", "/api/v1/sessions/"+s.ID+"/close", "", controlHeaders(), 200)
	call := app.TransactionSendRef{TransactionReviewRef: ref, CallID: send.CallID}
	if _, err := f.database.RecordTransactionError(t.Context(), call, 4900, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := responseJSON[TransactionResponse](t, f.request(t, "GET", path, "", controlHeaders(), 200)); got.State != "uncertain" || got.TxHash != "" {
		t.Fatal("unknown broadcast", got)
	}
	hash := "0x" + strings.Repeat("ab", 32)
	if _, err := f.database.RecordTransactionHash(t.Context(), call, hash, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := responseJSON[TransactionResponse](t, f.request(t, "GET", path, "", controlHeaders(), 200)); got.State != "sent" || got.TxHash != hash || got.FinishedAt == "" {
		t.Fatal("late hash", got)
	}
}
