package api

import (
	"net/http"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type TransactionSendRequest struct {
	ConnectionConsentRequest
	CallID         string `json:"callId" validate:"required" minlength:"32" maxlength:"32"`
	PreviousCallID string `json:"previousCallId,omitempty" minlength:"32" maxlength:"32"`
}

type TransactionSendPermit struct {
	Invoke      bool                `json:"invoke"`
	Transaction *TransactionPayload `json:"transaction,omitempty"`
}

type TransactionWalletReport struct {
	ConnectionReturnCallRequest
	TxHash    *string `json:"txHash,omitempty"`
	ErrorCode *int    `json:"errorCode,omitempty"`
}

// @Summary Reject a pending transaction from its review page
// @Description Also accepts a queued request. Returns the effective state if cancellation, expiry or wallet handoff already took effect. Accepts an empty body or {}.
// @Security BrowserCookie
// @Produce json
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "Transaction request ID"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,413,415,500 {object} ErrorResponse
// @Router /browser/transactions/{id}/reject [post]
func (h *handler) rejectBrowserTransaction(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	req, err := h.config.Transactions.RejectTransactionRequest(r.Context(), r.PathValue("id"), browser.ID)
	writeTransaction(w, req, err)
}

// @Summary Obtain a one-time transaction send permit
// @Description After user confirmation, submit the observed wallet binding and a fresh send call ID. Only invoke=true with the returned frozen payload permits the page to call the wallet. Retry transport failures with the original IDs.
// @Security BrowserCookie
// @Accept json
// @Produce json
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "Transaction request ID"
// @Param request body TransactionSendRequest true "Current execution, call and observed binding"
// @Success 200 {object} TransactionSendPermit
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/transactions/{id}/send [post]
func (h *handler) grantBrowserSend(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input TransactionSendRequest
	if !decodeBodyLimit(w, r, &input, false, MaxConnectionBody) {
		return
	}
	binding, ok := input.binding()
	if !ok {
		transactionFailure(w, app.ErrInvalidSession)
		return
	}
	ref := app.TransactionReviewRef{TransactionID: r.PathValue("id"), ID: input.ID, BrowserGrantID: browser.ID}
	payload, err := h.config.Transactions.GrantTransactionSend(r.Context(), ref, app.TransactionSendInput{
		CallID: input.CallID, PreviousCallID: input.PreviousCallID, Binding: binding,
	})
	if err != nil {
		transactionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TransactionSendPermit{Invoke: payload != nil, Transaction: (*TransactionPayload)(payload)})
}

// @Summary Report the result of the original wallet send
// @Description Supply exactly one of txHash or errorCode. Use code 0 when the provider gives no code. A valid replacement browser authorization may submit the original identifiers after the initial grant ends. A late hash can resolve an uncertain result.
// @Security BrowserCookie
// @Accept json
// @Produce json
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "Transaction request ID"
// @Param request body TransactionWalletReport true "Original execution and send call IDs with the wallet result"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/transactions/{id}/result [post]
func (h *handler) reportBrowserSend(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input TransactionWalletReport
	if !decodeBody(w, r, &input, false) {
		return
	}
	if (input.TxHash == nil) == (input.ErrorCode == nil) {
		failure(w, http.StatusBadRequest, "bad_request", "Supply exactly one of txHash or errorCode")
		return
	}
	ref := app.TransactionSendRef{
		TransactionReviewRef: app.TransactionReviewRef{TransactionID: r.PathValue("id"), ID: input.ID, BrowserGrantID: browser.ID},
		CallID:               input.CallID,
	}
	if input.TxHash != nil {
		req, err := h.config.Transactions.RecordTransactionHash(r.Context(), ref, *input.TxHash)
		writeTransaction(w, req, err)
		return
	}
	req, err := h.config.Transactions.RecordTransactionError(r.Context(), ref, *input.ErrorCode)
	writeTransaction(w, req, err)
}

// @Summary Acknowledge an interrupted wallet send
// @Description Any authorized management page may confirm that the original popup has been handled. Releases its slot and preserves the original transaction for a late wallet report. Accepts an empty body or {}.
// @Security BrowserCookie
// @Produce json
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "Transaction request ID"
// @Success 200 {object} TransactionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/transactions/{id}/acknowledge [post]
func (h *handler) acknowledgeBrowserSend(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	req, err := h.config.Transactions.AcknowledgeTransactionInterruption(r.Context(), r.PathValue("id"), browser.ID)
	writeTransaction(w, req, err)
}
