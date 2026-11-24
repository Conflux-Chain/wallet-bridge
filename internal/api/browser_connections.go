package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (h *handler) routeBrowserConnections(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/api/v1/browser/connections/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	verb := http.MethodGet
	var action func(http.ResponseWriter, *http.Request, app.BrowserGrant)
	switch {
	case len(parts) == 1:
		action = h.getBrowserConnection
	case len(parts) == 2 && parts[1] == "execution":
		action = h.getBrowserExecution
	case len(parts) == 3 && parts[1] == "execution":
		verb = http.MethodPost
		switch parts[2] {
		case "begin":
			action = h.beginBrowserCall
		case "return":
			action = h.returnBrowserCall
		case "complete":
			action = h.completeBrowserConnection
		case "reject":
			action = h.rejectBrowserConnection
		case "claim":
			action = h.claimBrowserExecution
		case "heartbeat":
			action = h.touchBrowserExecution
		case "interrupt":
			action = h.interruptBrowserExecution
		case "recover":
			action = h.recoverBrowserExecution
		}
	}
	if action == nil {
		return false
	}
	h.serveBrowserResource(w, r, verb, parts[0], action)
	return true
}

func (h *handler) serveBrowserResource(w http.ResponseWriter, r *http.Request, verb, id string, action func(http.ResponseWriter, *http.Request, app.BrowserGrant)) {
	if !method(w, r, verb) {
		return
	}
	browser, ok := h.authenticateBrowser(w, r)
	if !ok {
		return
	}
	if !validResourceID(id) {
		failure(w, http.StatusBadRequest, "bad_request", "Resource ID must be 32 lowercase hexadecimal characters")
		return
	}
	r.SetPathValue("id", id)
	action(w, r, browser)
}

func (h *handler) authenticateBrowser(w http.ResponseWriter, r *http.Request) (app.BrowserGrant, bool) {
	// ServeHTTP already validates Host and any supplied Origin. Browsers may
	// omit Origin on a same-origin GET. Mutations require it explicitly.
	if r.Method != http.MethodGet && !sameOrigin(w, r) {
		return app.BrowserGrant{}, false
	}
	token, ok := browserCookie(r)
	if !ok {
		authorizationFailure(w, app.ErrInvalidBrowserCredential)
		return app.BrowserGrant{}, false
	}
	grant, err := h.config.BrowserAuthorization.Authenticate(r.Context(), token)
	if err != nil {
		authorizationFailure(w, err)
		return app.BrowserGrant{}, false
	}
	return grant, true
}

// getBrowserConnection observes a request without accessing its Session.
// @Summary Read a connection for browser review
// @Description Requires a valid management cookie. Same-origin GET may omit Origin. Any supplied Origin must match the local service. Bearer alone cannot authorize this endpoint. Accepts no body or query parameters. Applies the original request deadline but never renews a Session, takes execution ownership, or invokes a wallet.
// @Produce json
// @Security BrowserCookie
// @Param Origin header string false "Must match the local service if supplied"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /browser/connections/{id} [get]
func (h *handler) getBrowserConnection(w http.ResponseWriter, r *http.Request, _ app.BrowserGrant) {
	h.getConnection(w, r)
}

// getBrowserExecution observes ownership and progress without renewing liveness.
// @Summary Read the current connection execution
// @Description Requires a valid management cookie. Same-origin GET may omit Origin. Any supplied Origin must match the local service. Accepts no body or query parameters. No execution returns 404 execution_not_found. Reads neither normalize stale phases nor acquire ownership or renew heartbeat/Session lifetime; subsequent mutations check liveness transactionally. No browser grant ID or secret is returned.
// @Produce json
// @Security BrowserCookie
// @Param Origin header string false "Must match the local service if supplied"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution [get]
func (h *handler) getBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	if !connectionReadInput(w, r) {
		return
	}
	result, err := h.config.Connections.Execution(r.Context(), browser, r.PathValue("id"))
	writeExecution(w, result, err)
}

// claimBrowserExecution binds a new page execution to the authenticated browser.
// @Summary Claim the connection execution
// @Description Requires management cookie and exact local Origin; the cookie supplies the browser subject, never the JSON body. Body contains only a fresh execution id, limited to 1024 bytes; query parameters are rejected. Retry with the same id; a matching retry does not renew heartbeat. Another owner/phase or occupied global wallet slot returns 409. Query the original request/execution instead of automatically generating a replacement id. Claiming never invokes a wallet or creates a Session.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionExecutionRequest true "Page execution ID"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/claim [post]
func (h *handler) claimBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	h.changeBrowserExecution(w, r, browser, h.config.Connections.ClaimExecution)
}

// touchBrowserExecution renews only a live execution owned by this browser/page.
// @Summary Send an execution heartbeat
// @Description Requires management cookie and exact local Origin. Body contains only the current execution id, limited to 1024 bytes; no query parameters. Does not extend the connection deadline or Session lifetime. Stale, interrupted, replaced or terminal executions cannot be revived by a heartbeat; query after a 409.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionExecutionRequest true "Current page execution ID"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/heartbeat [post]
func (h *handler) touchBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	h.changeBrowserExecution(w, r, browser, h.config.Connections.TouchExecution)
}

// interruptBrowserExecution invalidates an execution without canceling its request.
// @Summary Interrupt the current page execution
// @Description Requires management cookie and exact local Origin. Body contains only the current execution id, limited to 1024 bytes; no query parameters. Repeating interruption is idempotent. An unfinished wallet handoff remains interrupted_calling and occupies the slot; interruption cannot dismiss its popup or cancel the connection.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionExecutionRequest true "Current page execution ID"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/interrupt [post]
func (h *handler) interruptBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	h.changeBrowserExecution(w, r, browser, h.config.Connections.InterruptExecution)
}

// recoverBrowserExecution is an explicit user continuation, never an automatic retry.
// @Summary Explicitly continue an interrupted execution
// @Description Requires management cookie and exact local Origin. After the user ends the previous page/wallet operation, submit a fresh execution id and the expected previousId; retry with both unchanged. Body is limited to 1024 bytes; no query parameters. An active request gets a new execution and must recheck wallet/account/network. A terminal request only clears outstanding waiting and returns the original execution as finished; the new id is not installed and no Session is created. Old results cannot complete the new execution. Recovery does not invoke a wallet.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Param id path string true "32-character lowercase hexadecimal connection ID"
// @Param request body ConnectionExecutionRecoverRequest true "Fresh ID and expected previous execution ID"
// @Success 200 {object} ConnectionExecutionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/connections/{id}/execution/recover [post]
func (h *handler) recoverBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant) {
	var input ConnectionExecutionRecoverRequest
	if !decodeBody(w, r, &input, false) {
		return
	}
	result, err := h.config.Connections.RecoverExecution(r.Context(), browser, input.PreviousID, app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID})
	writeExecution(w, result, err)
}

func (h *handler) changeBrowserExecution(w http.ResponseWriter, r *http.Request, browser app.BrowserGrant,
	operation func(context.Context, app.BrowserGrant, app.ConnectionExecutionInput) (app.ConnectionExecution, error)) {
	var input ConnectionExecutionRequest
	if !decodeBody(w, r, &input, false) {
		return
	}
	result, err := operation(r.Context(), browser, app.ConnectionExecutionInput{ConnectionID: r.PathValue("id"), ID: input.ID})
	writeExecution(w, result, err)
}

func writeExecution(w http.ResponseWriter, result app.ConnectionExecution, err error) {
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, executionResponse(result))
}
