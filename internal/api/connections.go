package api

import (
	"errors"
	"math/big"
	"net/http"
	"strings"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

func (h *handler) routeClientRequests(w http.ResponseWriter, r *http.Request) bool {
	var action func(http.ResponseWriter, *http.Request)
	verb, id := http.MethodPost, ""
	switch {
	case r.URL.Path == "/api/v1/connections":
		action = h.createConnection
	case r.URL.Path == "/api/v1/transactions":
		action = h.createTransaction
	case strings.HasPrefix(r.URL.Path, "/api/v1/transactions/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/transactions/"), "/")
		id = parts[0]
		switch {
		case len(parts) == 1:
			verb, action = http.MethodGet, h.getTransaction
		case len(parts) == 2 && parts[1] == "cancel":
			action = h.cancelTransaction
		}
	case strings.HasPrefix(r.URL.Path, "/api/v1/connections/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/connections/"), "/")
		id = parts[0]
		switch {
		case len(parts) == 1:
			verb, action = http.MethodGet, h.getConnection
		case len(parts) == 2 && parts[1] == "cancel":
			action = h.cancelConnection
		}
	case strings.HasPrefix(r.URL.Path, "/api/v1/sessions/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/"), "/")
		id = parts[0]
		switch {
		case len(parts) == 1:
			verb, action = http.MethodGet, h.getSession
		case len(parts) == 2 && parts[1] == "close":
			action = h.closeSession
		}
	}
	if action == nil {
		return false
	}
	if method(w, r, verb) && h.authorize(w, r) {
		if r.URL.Path != "/api/v1/connections" && r.URL.Path != "/api/v1/transactions" && !validResourceID(id) {
			failure(w, http.StatusBadRequest, "bad_request", "Resource ID must be 32 lowercase hexadecimal characters")
			return true
		}
		r.SetPathValue("id", id)
		action(w, r)
	}
	return true
}

// createConnection submits one logical connection without invoking a wallet.
// @Summary Create or retry a connection request
// @Description Requires the service Bearer, never a browser cookie. Generate a 32-character lowercase hexadecimal ID once and reuse it for transport retries. Matching content returns the original state and deadline; changed content returns 409. Both creation and replay return 200, which does not imply user consent. chainId must be a decimal or 0x hexadecimal string, never a JSON number. Body limit is 8192 bytes. Query parameters and unknown fields are rejected. No browser link or wallet invocation is provided by this endpoint.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Param request body ConnectionCreateRequest true "Immutable connection input"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,405,409,413,415,500 {object} ErrorResponse
// @Router /connections [post]
func (h *handler) createConnection(w http.ResponseWriter, r *http.Request) {
	var input ConnectionCreateRequest
	if !decodeBodyLimit(w, r, &input, false, MaxConnectionBody) {
		return
	}
	chain, ok := httpChainID(input.ChainID)
	if !ok {
		connectionFailure(w, app.ErrInvalidConnectionRequest)
		return
	}
	result, err := h.config.Connections.Create(r.Context(), app.ConnectionInput{
		ID: input.ID, ClientName: input.ClientName, Intent: input.Intent, ChainID: chain, ChainName: input.ChainName,
	})
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionResponse(result))
}

// getConnection reads the original request without accessing its Session.
// @Summary Read connection status and result
// @Description Requires the service Bearer. No body or query parameters. Expired pending requests become expired; reads never renew the resulting Session. On success, use sessionId to explicitly access the fixed wallet/account/network through GET /sessions/{id}. A terminal request remains queryable after Session closure.
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal request ID"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /connections/{id} [get]
func (h *handler) getConnection(w http.ResponseWriter, r *http.Request) {
	if !connectionReadInput(w, r) {
		return
	}
	result, err := h.config.Connections.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionResponse(result))
}

// cancelConnection does not claim that an outstanding wallet popup ended.
// @Summary Cancel a pending connection request
// @Description Requires the service Bearer. Accepts no body or an empty JSON object, limited to 1024 bytes; no query parameters. Repeated cancellation preserves the original result. A different terminal outcome, including expiry, returns 409; query the original ID to reconcile. This does not close an existing Session or dismiss a wallet popup.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal request ID"
// @Success 200 {object} ConnectionResponse
// @Failure 400,401,403,404,405,409,413,415,500 {object} ErrorResponse
// @Router /connections/{id}/cancel [post]
func (h *handler) cancelConnection(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	result, err := h.config.Connections.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, connectionResponse(result))
}

// getSession is an explicit client access, not a browser observation.
// @Summary Access a Session or its closed history
// @Description Requires the service Bearer. No body or query parameters. Only an active Session renews its five-minute idle time. Expired or explicitly closed records return state=closed with their original fixed binding and closure time; reading never revives them. GET /connections/{id} does not renew this idle time.
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal Session ID"
// @Success 200 {object} SessionResponse
// @Failure 400,401,403,404,405,500 {object} ErrorResponse
// @Router /sessions/{id} [get]
func (h *handler) getSession(w http.ResponseWriter, r *http.Request) {
	if !connectionReadInput(w, r) {
		return
	}
	result, err := h.config.Connections.Session(r.Context(), r.PathValue("id"))
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(result))
}

// closeSession preserves the original connection result and fixed binding.
// @Summary Close a Session
// @Description Requires the service Bearer. Accepts no body or an empty JSON object, limited to 1024 bytes; no query parameters. Closure is idempotent and does not delete history. A Session cannot be created or rebound by this endpoint.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Param id path string true "32-character lowercase hexadecimal Session ID"
// @Success 200 {object} SessionResponse
// @Failure 400,401,403,404,405,413,415,500 {object} ErrorResponse
// @Router /sessions/{id}/close [post]
func (h *handler) closeSession(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	result, err := h.config.Connections.CloseSession(r.Context(), r.PathValue("id"))
	if err != nil {
		connectionFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse(result))
}

func connectionReadInput(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		failure(w, http.StatusBadRequest, "bad_request", "Read endpoints accept no body or query parameters")
		return false
	}
	return true
}

func httpChainID(value string) (string, bool) {
	if strings.HasPrefix(value, "0x") {
		return value, true // The core validates and normalizes hexadecimal input.
	}
	if value == "" {
		return "", false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	number, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return "", false
	}
	return "0x" + number.Text(16), true
}

func connectionFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidConnectionRequest), errors.Is(err, app.ErrInvalidSession), errors.Is(err, app.ErrInvalidConnectionExecution):
		failure(w, http.StatusBadRequest, "bad_request", "Invalid connection input or Session ID")
	case errors.Is(err, app.ErrInvalidBrowserCredential):
		authorizationFailure(w, err)
	case errors.Is(err, app.ErrConnectionRequestNotFound):
		failure(w, http.StatusNotFound, "connection_not_found", "The connection request does not exist")
	case errors.Is(err, app.ErrSessionNotFound):
		failure(w, http.StatusNotFound, "session_not_found", "The Session does not exist")
	case errors.Is(err, app.ErrConnectionExecutionNotFound):
		failure(w, http.StatusNotFound, "execution_not_found", "No execution is recorded for this connection")
	case errors.Is(err, app.ErrConnectionExecutionConflict):
		failure(w, http.StatusConflict, "execution_conflict", "The execution owner or phase changed; query the original connection and execution")
	case errors.Is(err, app.ErrWalletBusy):
		failure(w, http.StatusConflict, "wallet_busy", "Another request occupies the wallet; check browser status for its progress")
	case errors.Is(err, app.ErrConnectionConflict), errors.Is(err, app.ErrConnectionRequestExists):
		failure(w, http.StatusConflict, "connection_conflict", "The request conflicts with the original input or outcome; query its ID")
	default:
		failure(w, http.StatusInternalServerError, "storage_error", "Cannot complete the connection or Session operation")
	}
}
