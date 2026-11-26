//go:build windows || darwin

package api

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

func TestUpgradeRollbackRecoversWalletData(t *testing.T) {
	f := newConnectionHTTPFixture(t, "")
	if err := credentials.Ensure(f.dir); err != nil {
		t.Fatal(err)
	}
	key, err := credentials.Read(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	session, req := seedTransactionForBrowser(t, f, time.Now())
	cookie, _ := f.exchange(t, f.ticket(t))
	headers := browserHeaders(cookie)
	path := "/api/v1/browser/transactions/" + req.ID
	execution, call := strings.Repeat("a", 32), strings.Repeat("c", 32)
	f.reviewCommand(t, cookie, path+"/execution", TransactionReviewCommand{Action: app.ClaimReview, ID: execution}, 200)
	send := TransactionSendRequest{ConnectionConsentRequest: ConnectionConsentRequest{ID: execution,
		Wallet: walletResponse(session.Wallet), Account: session.Account, ChainID: session.ChainID}, CallID: call}
	f.request(t, "POST", path+"/send", jsonBody(t, send), headers, 200)
	hash := "0x" + strings.Repeat("12", 32)
	report := TransactionWalletReport{ConnectionReturnCallRequest: ConnectionReturnCallRequest{ID: execution, CallID: call}, TxHash: &hash}
	sent := responseJSON[TransactionResponse](t, f.request(t, "POST", path+"/result", jsonBody(t, report), headers, 200))
	session, err = f.database.GetSession(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.database.Close(); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("b", 32)
	if err := update.CreateBackup(t.Context(), f.dir, id); err != nil {
		t.Fatal(err)
	}
	// Reopen the isolated copy with the regular HTTP handlers and original cookie.
	err = update.RollbackFromBackup(t.Context(), f.dir, id, func(work string) error {
		if saved, err := credentials.Read(work); err != nil || saved != key {
			t.Fatal("restored service credential differs", err)
		}
		recovered := newConnectionHTTPFixture(t, work)
		defer recovered.database.Close()
		recovered.request(t, "GET", path, "", headers, 200)
		got := responseJSON[TransactionResponse](t, recovered.request(t, "GET", transactionsPath+"/"+req.ID, "", controlHeaders(), 200))
		if got != sent || got.TxHash != hash {
			t.Fatal("restored transaction or deadline differs", got)
		}
		current, err := recovered.database.GetSession(t.Context(), session.ID)
		if err != nil || !reflect.DeepEqual(current, session) {
			t.Fatal("restored Session differs", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "backups", id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful rollback retained its backup", err)
	}
}
