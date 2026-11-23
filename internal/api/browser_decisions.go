package api

import (
	"net/http"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type ConnectionBeginCallRequest struct {
	ID             string `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	CallID         string `json:"callId" validate:"required" minlength:"32" maxlength:"32"`
	PreviousCallID string `json:"previousCallId,omitempty" minlength:"32" maxlength:"32"`
}

type ConnectionReturnCallRequest struct {
	ID     string `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	CallID string `json:"callId" validate:"required" minlength:"32" maxlength:"32"`
}

type ConnectionConsentRequest struct {
	ID      string         `json:"id" validate:"required" minlength:"32" maxlength:"32"`
	Wallet  WalletResponse `json:"wallet" validate:"required"`
	Account string         `json:"account" validate:"required"`
	ChainID string         `json:"chainId" validate:"required" example:"0x47"`
}

type ConnectionCallPermitResponse struct {
	Execution ConnectionExecutionResponse `json:"execution"`
	Invoke    bool                        `json:"invoke"`
}

// @Summary Begin a wallet call
// @Description Newly committed handoff grants invoke once. Retry with the same callId and previousCallId after a lost response. Only invoke=true authorizes this page to open the wallet. JSON body limit: 1024 bytes. No query parameters.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionBeginCallRequest true "Begin a wallet call"
// @Success 200 {object} ConnectionCallPermitResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/begin [post]
func (h *handler) beginBrowserCall(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input ConnectionBeginCallRequest
	if !decodeBody(w, r, &input, false) {
		return
	}
	ref := app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID}
	result, err := h.config.Connections.BeginCall(r.Context(), browser, ref, input.CallID, input.PreviousCallID)
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ConnectionCallPermitResponse{Execution: executionResponse(result.Execution), Invoke: result.Invoke})
}

// @Summary Record that the wallet call ended
// @Description Return the same callId after the wallet promise settles. A return never grants consent or establishes a Session. JSON body limit: 1024 bytes. No query parameters.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionReturnCallRequest true "Record that the wallet call ended"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/return [post]
func (h *handler) returnBrowserCall(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input ConnectionReturnCallRequest
	if !decodeBody(w, r, &input, false) {
		return
	}
	ref := app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID}
	result, err := h.config.Connections.ReturnCall(r.Context(), browser, ref, input.CallID)
	writeExecution(w, result, err)
}

// @Summary Confirm a connection with a wallet binding
// @Description Submit after explicit consent with the actual wallet, account and network. A matching retry returns the committed connection and Session ID. JSON body limit: 8192 bytes. No query parameters.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionConsentRequest true "Confirm a connection with a wallet binding"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/complete [post]
func (h *handler) completeBrowserConnection(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input ConnectionConsentRequest
	if !decodeBodyLimit(w, r, &input, false, MaxConnectionBody) {
		return
	}
	chain, ok := httpChainID(input.ChainID)
	if !ok {
		connectionFailure(w, app.ErrInvalidSession)
		return
	}
	ref := app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID}
	binding := app.ConnectionBinding{
		Wallet:  app.WalletDescriptor{Kind: app.WalletKind(input.Wallet.Kind), Name: input.Wallet.Name, RDNS: input.Wallet.RDNS},
		Account: input.Account, ChainID: chain,
	}
	result, err := h.config.Connections.CompleteExecution(r.Context(), browser, ref, binding)
	writeBrowserDecision(w, result, err)
}

// @Summary Reject the connection
// @Description Reject using the current ready execution. A matching replay returns the committed rejection. JSON body limit: 1024 bytes. No query parameters.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionExecutionRequest true "Reject the connection"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/reject [post]
func (h *handler) rejectBrowserConnection(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input ConnectionExecutionRequest
	if !decodeBody(w, r, &input, false) {
		return
	}
	ref := app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID}
	result, err := h.config.Connections.RejectExecution(r.Context(), browser, ref)
	writeBrowserDecision(w, result, err)
}

func writeBrowserDecision(w http.ResponseWriter, request app.ConnectionRequest, err error) {
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionResponse(request))
}
