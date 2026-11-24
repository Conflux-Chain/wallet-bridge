package client

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"strings"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const transactionsPath = "/api/v1/transactions"

// CreateTransaction submits once. Retain the input, including its ID, for a retry.
func (c *Client) CreateTransaction(ctx context.Context, input api.TransactionCreateRequest) (api.TransactionResponse, error) {
	if !validHex(input.SessionID, 16) {
		return api.TransactionResponse{}, &Error{"bad_request", "Use the Session ID returned by the connection request", nil}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return api.TransactionResponse{}, &Error{"bad_request", "Cannot encode the transaction request", err}
	}
	result, err := c.transaction(ctx, input.ID, http.MethodPost, transactionsPath, body)
	if err == nil && (result.SessionID != input.SessionID || result.ClientName != input.ClientName || result.Intent != input.Intent) {
		return api.TransactionResponse{}, invalidTransactionResponse()
	}
	return result, err
}

func (c *Client) GetTransaction(ctx context.Context, id string) (api.TransactionResponse, error) {
	return c.transaction(ctx, id, http.MethodGet, transactionsPath+"/"+id, nil)
}

// CancelTransaction returns the effective outcome if expiry or handoff won the race.
func (c *Client) CancelTransaction(ctx context.Context, id string) (api.TransactionResponse, error) {
	result, err := c.transaction(ctx, id, http.MethodPost, transactionsPath+"/"+id+"/cancel", nil)
	if err == nil && result.State == "pending" {
		return api.TransactionResponse{}, invalidTransactionResponse()
	}
	return result, err
}

func (c *Client) transaction(ctx context.Context, id, method, path string, body []byte) (api.TransactionResponse, error) {
	data, err := c.lifecycleRequest(ctx, transactionCapability, id, method, path, body)
	if err != nil {
		return api.TransactionResponse{}, err
	}
	var result api.TransactionResponse
	if json.Unmarshal(data, &result) != nil || result.ID != id || !validHex(result.SessionID, 16) ||
		strings.TrimSpace(result.ClientName) == "" || !validTimestamp(result.CreatedAt) || !validTimestamp(result.ExpiresAt) ||
		!validTransactionPayload(result.Transaction) || !validTransactionOutcome(result) {
		return api.TransactionResponse{}, invalidTransactionResponse()
	}
	return result, nil
}

func validTransactionPayload(tx api.TransactionPayload) bool {
	fields := map[string]string{"from": tx.From, "to": tx.To, "chainId": tx.ChainID, "value": tx.Value, "data": tx.Data}
	for name, value := range map[string]string{"gas": tx.Gas, "gasPrice": tx.GasPrice,
		"maxFeePerGas": tx.MaxFeePerGas, "maxPriorityFeePerGas": tx.MaxPriorityFeePerGas} {
		if value != "" {
			fields[name] = value
		}
	}
	normalized, err := app.NormalizeTransaction(fields, app.Session{Account: tx.From, ChainID: tx.ChainID})
	return err == nil && tx == api.TransactionPayload(normalized)
}

func validTransactionOutcome(result api.TransactionResponse) bool {
	handedOff, finished := result.HandedOffAt != "", result.FinishedAt != ""
	if handedOff && !validTimestamp(result.HandedOffAt) || finished && !validTimestamp(result.FinishedAt) {
		return false
	}
	if result.State == "sent" {
		hash, ok := strings.CutPrefix(result.TxHash, "0x")
		return handedOff && finished && ok && validHex(hash, 32)
	}
	if result.TxHash != "" {
		return false
	}
	switch result.State {
	case "pending":
		return !handedOff && !finished
	case "canceled", "expired":
		return !handedOff && finished
	case "rejected": // The review page or the wallet can reject.
		return finished
	case "awaiting_wallet":
		return handedOff && !finished
	case "uncertain":
		return handedOff && finished
	default:
		return false
	}
}

func invalidTransactionResponse() error {
	return &Error{"request_failed", "The service returned an invalid transaction response; query the original request ID", nil}
}

func transactionParameterMessage(message string) string {
	switch message {
	case `Unsupported transaction field "nonce"`:
		return "Omit nonce; the wallet selects it"
	case `Unsupported transaction field "type"`:
		return "Omit type; the wallet selects it"
	case "invalid transaction: to is required (contract deployment is unsupported)":
		return "Supply a recipient address; contract deployment is unsupported"
	default:
		return "Invalid transaction parameters; check the Session, sender, chain, recipient and fee fields"
	}
}
