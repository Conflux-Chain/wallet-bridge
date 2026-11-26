package api

import "net/http"

type UpgradeBusyResponse struct {
	ErrorResponse
	ActiveWalletRequest ActiveWalletRequestResponse `json:"activeWalletRequest"`
}

// A successful admission holds the read lock through the request or wallet claim.
func (h *handler) admit(w http.ResponseWriter) bool {
	h.admission.RLock()
	if h.stoppingForUpgrade {
		h.admission.RUnlock()
		failure(w, http.StatusServiceUnavailable, "upgrade_in_progress", "Retry after the service upgrade")
		return false
	}
	return true
}

// @Summary Stop an idle wallet service for upgrade
// @Description Atomically check the wallet and close admission before requesting shutdown. If busy, handle the identified request and retry explicitly. HTTP 202 acknowledges shutdown. Wait for the service to exit before switching versions.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Success 202 {object} StopResponse
// @Failure 409 {object} UpgradeBusyResponse
// @Failure 400,401,403,405,413,415,500,503 {object} ErrorResponse
// @Router /control/stop-for-upgrade [post]
func (h *handler) stopForUpgrade(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	h.admission.Lock()
	defer h.admission.Unlock()
	if h.stoppingForUpgrade {
		failure(w, http.StatusServiceUnavailable, "upgrade_in_progress", "A service upgrade is already stopping this instance")
		return
	}
	request, err := h.config.Connections.UpgradeBlocker(r.Context())
	if err != nil {
		failure(w, http.StatusInternalServerError, "storage_error", "Cannot determine which request occupies the wallet")
		return
	}
	if request != nil {
		writeJSON(w, http.StatusConflict, UpgradeBusyResponse{
			ErrorResponse:       ErrorResponse{Code: "wallet_busy", Message: "Handle the wallet request and retry the upgrade"},
			ActiveWalletRequest: ActiveWalletRequestResponse{Kind: string(request.Kind), ID: request.ID, Phase: string(request.Phase)},
		})
		return
	}
	h.stoppingForUpgrade = true
	writeJSON(w, http.StatusAccepted, StopResponse{State: "stopping"})
	h.config.Stop()
}
