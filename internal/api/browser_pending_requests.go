package api

import (
	"net/http"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type PendingRequestResponse struct {
	Kind       string `json:"kind" enums:"connection,transaction"`
	ID         string `json:"id"`
	ClientName string `json:"clientName"`
	ChainID    string `json:"chainId"`
	CreatedAt  string `json:"createdAt" format:"date-time"`
	ExpiresAt  string `json:"expiresAt" format:"date-time"`
}

type PendingRequestListResponse struct {
	Requests []PendingRequestResponse `json:"requests"`
}

// @Summary List pending connection and transaction approvals
// @Description Returns unexpired requests oldest first, then by kind and ID for ties. Reading the list does not claim the wallet or renew any deadline. Requires the management cookie. Accepts no query parameters or body.
// @Produce json
// @Security BrowserCookie
// @Param Origin header string false "Must match the local service if supplied"
// @Success 200 {object} PendingRequestListResponse
// @Failure 400,401,403,405,500 {object} ErrorResponse
// @Router /browser/pending-requests [get]
func (h *handler) listPendingRequests(w http.ResponseWriter, r *http.Request, grant app.BrowserGrant) {
	if !connectionReadInput(w, r) {
		return
	}
	requests, err := h.config.Connections.PendingWalletRequests(r.Context(), grant)
	if err != nil {
		connectionFailure(w, err)
		return
	}
	items := make([]PendingRequestResponse, len(requests))
	for i, req := range requests {
		items[i] = PendingRequestResponse{
			Kind: string(req.Kind), ID: req.ID, ClientName: req.ClientName, ChainID: req.ChainID,
			CreatedAt: timestamp(req.CreatedAt), ExpiresAt: timestamp(req.ExpiresAt),
		}
	}
	writeJSON(w, http.StatusOK, PendingRequestListResponse{Requests: items})
}
