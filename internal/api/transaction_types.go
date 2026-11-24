package api

import (
	"encoding/json"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type TransactionCreateRequest struct {
	ID         string `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	SessionID  string `json:"sessionId" validate:"required" minlength:"32" maxlength:"32"`
	ClientName string `json:"clientName" validate:"required"`
	// Optional caller-supplied description, displayed separately from the transaction fields.
	Intent      string                     `json:"intent,omitempty"`
	Transaction map[string]json.RawMessage `json:"transaction" validate:"required" swaggertype:"object"`
}

type TransactionPayload struct {
	From                 string `json:"from" validate:"required"`
	To                   string `json:"to" validate:"required"`
	ChainID              string `json:"chainId" validate:"required" example:"0x47"`
	Value                string `json:"value,omitempty" example:"0x0"`
	Data                 string `json:"data,omitempty" example:"0x"`
	Gas                  string `json:"gas,omitempty"`
	GasPrice             string `json:"gasPrice,omitempty"`
	MaxFeePerGas         string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas,omitempty"`
}

type TransactionResponse struct {
	ID          string             `json:"id"`
	SessionID   string             `json:"sessionId"`
	ClientName  string             `json:"clientName"`
	Intent      string             `json:"intent"`
	Transaction TransactionPayload `json:"transaction"`
	State       string             `json:"state" enums:"pending,awaiting_wallet,sent,rejected,uncertain,canceled,expired"`
	CreatedAt   string             `json:"createdAt" format:"date-time"`
	ExpiresAt   string             `json:"expiresAt" format:"date-time"`
	HandedOffAt string             `json:"handedOffAt,omitempty" format:"date-time"`
	FinishedAt  string             `json:"finishedAt,omitempty" format:"date-time"`
	TxHash      string             `json:"txHash,omitempty"`
}

func transactionResponse(req app.TransactionRequest) TransactionResponse {
	result := TransactionResponse{
		ID: req.ID, SessionID: req.SessionID, ClientName: req.ClientName, Intent: req.Intent,
		Transaction: TransactionPayload(req.Transaction), State: string(req.State), TxHash: req.TxHash,
		CreatedAt: timestamp(req.CreatedAt), ExpiresAt: timestamp(req.ExpiresAt),
	}
	if req.HandedOffAt != nil {
		result.HandedOffAt = timestamp(*req.HandedOffAt)
	}
	if req.FinishedAt != nil {
		result.FinishedAt = timestamp(*req.FinishedAt)
	}
	return result
}
