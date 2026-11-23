//go:build manual_wallet

package client

import (
	"context"
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
	id := NewConnectionID()
	input := api.ConnectionCreateRequest{ID: id, ClientName: "Wallet Bridge 验收", Intent: "核对选定的钱包、账户与网络", ChainID: "0x47", ChainName: "Conflux eSpace Testnet"}
	if _, err := client.CreateConnection(ctx, input); err != nil {
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
			result, err := client.GetConnection(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if result.State == "pending" {
				continue
			}
			if result.State != "succeeded" {
				t.Fatalf("Connection ended: %s", result.State)
			}
			session, err := client.GetSession(ctx, result.SessionID)
			if err != nil {
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
