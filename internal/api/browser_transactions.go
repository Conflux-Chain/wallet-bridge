package api

import (
	"net/http"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type BrowserTransactionResponse struct {
	TransactionResponse
	Wallet WalletResponse `json:"wallet"`
}

// @Summary Read a transaction for browser review
// @Description Returns the frozen transaction, its latest recorded state and the wallet selected for its Session. The original approval deadline applies. Reading does not claim the wallet or renew the Session or execution. Requires the management cookie. Accepts no query parameters or request body.
// @Produce json
// @Security BrowserCookie
// @Param Origin header string false "Must match the local service if supplied"
// @Param id path string true "32-character lowercase hexadecimal transaction request ID"
// @Success 200 {object} BrowserTransactionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /browser/transactions/{id} [get]
func (h *handler) getBrowserTransaction(w http.ResponseWriter, r *http.Request, _ app.BrowserGrant) {
	if !connectionReadInput(w, r) {
		return
	}
	requests := h.config.Transactions
	req, err := requests.GetTransactionRequest(r.Context(), r.PathValue("id"), time.Now())
	if err != nil {
		transactionFailure(w, err)
		return
	}
	// A Session's wallet binding is immutable, including after closure.
	session, err := requests.GetSession(r.Context(), req.SessionID)
	if err != nil {
		transactionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, BrowserTransactionResponse{TransactionResponse: transactionResponse(req), Wallet: walletResponse(session.Wallet)})
}
