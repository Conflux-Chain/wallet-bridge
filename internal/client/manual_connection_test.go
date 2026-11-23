//go:build manual_wallet

package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
)

var walletDir = flag.String("wallet-data-dir", "", "absolute data directory of the running test service")

// Opt-in HTTP client for manually checking a browser wallet before the CLI is available.
func TestManualWalletConnection(t *testing.T) {
	if *walletDir == "" {
		t.Skip("supply -wallet-data-dir for manual acceptance")
	}
	client, err := New(*walletDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := hex.EncodeToString(nonce[:])
	input, err := json.Marshal(api.ConnectionCreateRequest{ID: id, ClientName: "Wallet Bridge 验收", Intent: "核对选定的钱包、账户与网络", ChainID: "0x47", ChainName: "Conflux eSpace Testnet"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.request(ctx, "POST", "/api/v1/connections", requestOptions{expected: 200, body: input}); err != nil {
		t.Fatal(err)
	}
	fmt.Println("Open in the authorized Chrome profile:", api.Origin+"/?connection="+id)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
			body, _, err := client.request(ctx, "GET", "/api/v1/connections/"+id, requestOptions{expected: 200})
			if err != nil {
				t.Fatal(err)
			}
			var result api.ConnectionResponse
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			if result.State == "pending" {
				continue
			}
			if result.State != "succeeded" {
				t.Fatalf("Connection ended: %s", result.State)
			}
			body, _, err = client.request(ctx, "GET", "/api/v1/sessions/"+result.SessionID, requestOptions{expected: 200})
			if err != nil {
				t.Fatal(err)
			}
			var session api.SessionResponse
			if err := json.Unmarshal(body, &session); err != nil {
				t.Fatal(err)
			}
			if session.ConnectionID != id || session.ChainID != "0x47" || session.State != "active" {
				t.Fatal("Unexpected session binding")
			}
			fmt.Printf("Connected: wallet=%q account=%s chainId=%s\n", session.Wallet.Name, session.Account, session.ChainID)
			return
		}
	}
}
