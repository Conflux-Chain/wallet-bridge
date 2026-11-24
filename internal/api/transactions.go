package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type TransactionRequests interface {
	CreateTransactionRequest(context.Context, app.TransactionRequestInput, time.Time) (app.TransactionRequest, error)
	GetTransactionRequest(context.Context, string, time.Time) (app.TransactionRequest, error)
	CancelTransactionRequest(context.Context, string, time.Time) (app.TransactionRequest, error)
	GetSession(context.Context, string) (app.Session, error)
	ObserveTransactionReview(context.Context, string) (app.TransactionReviewStatus, error)
	ApplyTransactionReview(context.Context, app.TransactionReviewRef, app.ReviewCommand) (app.ReviewResult, error)
}

// @Summary Create or retry a transaction request
// @Description Reuse the request ID for retries. Matching normalized content returns the original record and deadline. A changed request with that ID returns 409. Amounts and fees are strings in smallest units, in decimal or 0x hexadecimal. Omitted value and data default to zero and empty bytes. Omit fees for wallet estimation. The JSON body limit is 1 MiB.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Param request body TransactionCreateRequest{transaction=TransactionPayload} true "Caller metadata and transaction for a fixed Session"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /transactions [post]
func (h *handler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var input TransactionCreateRequest
	if !decodeBodyLimit(w, r, &input, false, MaxTransactionBody) {
		return
	}
	if input.Transaction == nil {
		failure(w, http.StatusBadRequest, "bad_request", "A transaction object is required")
		return
	}
	fields := make(map[string]string, len(input.Transaction))
	for name, raw := range input.Transaction {
		if name == "nonce" || name == "type" {
			failure(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Unsupported transaction field %q", name))
			return
		}
		var value string
		if raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
			failure(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Transaction field %q must be a JSON string", name))
			return
		}
		fields[name] = value
	}
	result, err := h.config.Transactions.CreateTransactionRequest(r.Context(), app.TransactionRequestInput{
		ID: input.ID, SessionID: input.SessionID, ClientName: input.ClientName, Intent: input.Intent, Fields: fields,
	}, time.Now())
	writeTransaction(w, result, err)
}

// @Summary Read the transaction and its latest result
// @Description Reads the original request without renewing its deadline or Session. A late wallet reply can update an uncertain result. A transaction hash identifies a submitted transaction. The client can use the chain ID and hash to query a receipt.
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal request ID"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /transactions/{id} [get]
func (h *handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	if !connectionReadInput(w, r) {
		return
	}
	result, err := h.config.Transactions.GetTransactionRequest(r.Context(), r.PathValue("id"), time.Now())
	writeTransaction(w, result, err)
}

// @Summary Cancel a transaction awaiting approval
// @Description Accepts an empty body or an empty JSON object. Returns the effective state, including expiry or an earlier wallet handoff. Check state before treating the transaction as canceled.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal request ID"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,413,415,500 {object} ErrorResponse
// @Router /transactions/{id}/cancel [post]
func (h *handler) cancelTransaction(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	result, err := h.config.Transactions.CancelTransactionRequest(r.Context(), r.PathValue("id"), time.Now())
	writeTransaction(w, result, err)
}

func writeTransaction(w http.ResponseWriter, req app.TransactionRequest, err error) {
	if err != nil {
		transactionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, transactionResponse(req))
}

func transactionFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidTransaction):
		failure(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, app.ErrTransactionRequestNotFound):
		failure(w, http.StatusNotFound, "transaction_not_found", "The transaction request does not exist")
	case errors.Is(err, app.ErrSessionNotFound):
		failure(w, http.StatusNotFound, "session_not_found", "The Session does not exist")
	case errors.Is(err, app.ErrSessionClosed):
		failure(w, http.StatusConflict, "session_closed", "A new transaction requires an active Session")
	case errors.Is(err, app.ErrTransactionRequestConflict):
		failure(w, http.StatusConflict, "transaction_conflict", "The request ID already identifies different content")
	case errors.Is(err, app.ErrTransactionReviewConflict):
		failure(w, http.StatusConflict, "execution_conflict", "Read the current transaction and review phase")
	case errors.Is(err, app.ErrInvalidBrowserCredential):
		authorizationFailure(w, err)
	case errors.Is(err, app.ErrWalletBusy):
		failure(w, http.StatusConflict, "wallet_busy", "Another request occupies the wallet")
	default:
		failure(w, http.StatusInternalServerError, "storage_error", "Cannot complete the transaction request")
	}
}
