package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type transactionInput[T any] struct {
	requestLookup
	sessionLookup
	Intent      string `json:"intent,omitempty" jsonschema:"Optional user-stated purpose. Omit when unknown. Do not infer the effects of unrecognized calldata."`
	Transaction T      `json:"transaction" jsonschema:"Raw EVM fields. Quantities use decimal or 0x hexadecimal strings in smallest units. Omit fees for wallet estimation, or supply gasPrice or both EIP-1559 fee fields. Nonce, explicit type and contract deployment are unsupported."`
}

type transactionResult struct {
	api.TransactionResponse
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

func (t walletTools) createTransaction(ctx context.Context, req *sdk.CallToolRequest, in transactionInput[map[string]json.RawMessage]) (*sdk.CallToolResult, transactionResult, error) {
	if !t.uiEmbedded {
		return nil, transactionResult{}, errors.New("ui_unavailable: use a build with the embedded transaction review page")
	}
	name, err := callerName(req)
	if err != nil {
		return nil, transactionResult{}, err
	}
	c, err := client.New(t.dataDir)
	if err != nil {
		return nil, transactionResult{}, err
	}
	if err := c.EnsureTransactionApproval(ctx); err != nil {
		return nil, transactionResult{}, err
	}
	return transactionReceipt(c.CreateTransaction(ctx, api.TransactionCreateRequest{
		ID: in.RequestID, SessionID: in.SessionID, ClientName: name, Intent: in.Intent, Transaction: in.Transaction,
	}))
}

func (t walletTools) getTransaction(ctx context.Context, _ *sdk.CallToolRequest, in requestLookup) (*sdk.CallToolResult, transactionResult, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, transactionResult{}, err
	}
	return transactionReceipt(c.GetTransaction(ctx, in.RequestID))
}

func (t walletTools) cancelTransaction(ctx context.Context, _ *sdk.CallToolRequest, in requestLookup) (*sdk.CallToolResult, transactionResult, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, transactionResult{}, err
	}
	return transactionReceipt(c.CancelTransaction(ctx, in.RequestID))
}

func transactionReceipt(r api.TransactionResponse, err error) (*sdk.CallToolResult, transactionResult, error) {
	if err != nil {
		return nil, transactionResult{}, err
	}
	url := api.Origin + "/?transaction=" + r.ID
	text := fmt.Sprintf("Transaction %s (%s), chain %s. [Open the transaction request](%s)", r.ID, r.State, r.Transaction.ChainID, url)
	switch r.State {
	case "pending":
		text += "\nApproval expires at " + r.ExpiresAt
	case "awaiting_wallet":
		text += "\nThe wallet is processing the send. Wait for its response and check this request again."
	case "uncertain":
		text += "\nThe transaction may have been broadcast. End automatic waiting. Use get_transaction later with this request ID to check for a late hash. Do not resend."
	case "sent":
		text += "\nTransaction hash: " + r.TxHash + ". Check the receipt separately for on-chain execution."
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, transactionResult{r, "transaction", url}, nil
}
