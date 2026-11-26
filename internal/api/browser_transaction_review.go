package api

import (
	"net/http"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type TransactionReviewStatusResponse struct {
	Phase         string `json:"phase" enums:"unclaimed,ready,calling,interrupted_ready,interrupted_calling,finished"`
	RecoverFromID string `json:"recoverFromId,omitempty"`
}

type TransactionReviewCommand struct {
	Action     app.ReviewAction `json:"action" swaggertype:"string" validate:"required" enums:"claim,heartbeat,interrupt,recover,begin_preparation,return_preparation"`
	ID         string           `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	PreviousID string           `json:"previousId,omitempty" minlength:"32" maxlength:"32"`
	CallID     string           `json:"callId,omitempty" minlength:"32" maxlength:"32"`
}

type TransactionReviewCommandResponse struct {
	Phase  string `json:"phase" enums:"ready,calling,interrupted_ready,interrupted_calling,finished"`
	Invoke bool   `json:"invoke"`
}

// @Summary Observe transaction review progress
// @Description Reports the recorded phase without claiming or renewing the execution. An interrupted preparation includes the ID needed for manual recovery. A page opening the transaction for the first time sees unclaimed.
// @Produce json
// @Security BrowserCookie
// @Param id path string true "Transaction request ID"
// @Success 200 {object} TransactionReviewStatusResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /browser/transactions/{id}/execution [get]
func (h *handler) observeTransactionReview(w http.ResponseWriter, r *http.Request, _ app.BrowserGrant) {
	if !connectionReadInput(w, r) {
		return
	}
	status, err := h.config.Transactions.ObserveTransactionReview(r.Context(), r.PathValue("id"))
	if err != nil {
		transactionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TransactionReviewStatusResponse{Phase: status.Phase, RecoverFromID: status.RecoverFromID})
}

// @Summary Coordinate a review or a wallet preparation call
// @Description Use a page-generated execution id. Claim, heartbeat and interrupt require only action and id. Recover takes a new id and the observed recoverFromId as previousId. Begin preparation takes a fresh callId and the preceding call ID as previousId, omitted for the first call. Return preparation uses the original callId. Reuse IDs on retry. Invoke a wallet preparation only on invoke=true. After a send handoff only heartbeat and interrupt are available here.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "Transaction request ID"
// @Param request body TransactionReviewCommand true "Review action and identifiers"
// @Success 200 {object} TransactionReviewCommandResponse
// @Failure 400,401,403,404,405,409,413,415,500,503 {object} ErrorResponse
// @Router /browser/transactions/{id}/execution [post]
func (h *handler) changeTransactionReview(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input TransactionReviewCommand
	if !decodeBody(w, r, &input, false) {
		return
	}
	ref := app.TransactionReviewRef{TransactionID: r.PathValue("id"), ID: input.ID, BrowserGrantID: browser.ID}
	cmd := app.ReviewCommand{Action: input.Action, PreviousID: input.PreviousID, CallID: input.CallID}
	if cmd.Action == app.ClaimReview || cmd.Action == app.RecoverReview {
		if !h.admit(w) {
			return
		}
		defer h.admission.RUnlock()
	}
	result, err := h.config.Transactions.ApplyTransactionReview(r.Context(), ref, cmd)
	if err != nil {
		transactionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, TransactionReviewCommandResponse{Phase: string(result.Review.Phase), Invoke: result.Invoke})
}
