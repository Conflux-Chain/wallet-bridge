package api

import "net/http"

type OpenBrowserRequest struct {
	Kind string `json:"kind" enums:"connection,transaction"`
	ID   string `json:"id" minLength:"32" maxLength:"32"`
}

// @Summary Open an approval link in the system default browser
// @Description Accepts only a request kind and ID. The service constructs a fixed local URL. No management grant is needed or transferred. The destination page checks its own authorization.
// @Accept json
// @Param Origin header string true "Exact local service origin"
// @Param request body OpenBrowserRequest true "Request from the current approval link"
// @Success 204
// @Failure 400,403,405,413,415,503 {object} ErrorResponse
// @Router /browser/open-request [post]
func (h *handler) openBrowserRequest(w http.ResponseWriter, r *http.Request) {
	var request OpenBrowserRequest
	if !decodeBody(w, r, &request, false) {
		return
	}
	if (request.Kind != "connection" && request.Kind != "transaction") || !validResourceID(request.ID) {
		failure(w, http.StatusBadRequest, "bad_request", "Expected a connection or transaction and its original request ID")
		return
	}
	target := Origin + "/?" + request.Kind + "=" + request.ID
	if h.config.OpenBrowser == nil || h.config.OpenBrowser(target) != nil {
		failure(w, http.StatusServiceUnavailable, "browser_open_failed", "Open the original approval link in your wallet browser")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
