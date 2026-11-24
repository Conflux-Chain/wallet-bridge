package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type ActivityResponse struct {
	Kind       string `json:"kind" enums:"connection,transaction"`
	ID         string `json:"id"`
	ClientName string `json:"clientName"`
	ChainID    string `json:"chainId"`
	CreatedAt  string `json:"createdAt" format:"date-time"`
	State      string `json:"state" enums:"pending,succeeded,awaiting_wallet,sent,rejected,uncertain,canceled,expired"`
	SessionID  string `json:"sessionId,omitempty"`
	TxHash     string `json:"txHash,omitempty"`
}

type ActivityListResponse struct {
	Activities []ActivityResponse `json:"activities"`
	NextAfter  string             `json:"nextAfter,omitempty"`
}

func activityCursor(key app.ActivityKey) string {
	return strconv.FormatInt(key.CreatedAt.UnixMilli(), 10) + "." + key.ID + "." + string(key.Kind)
}

func parseActivityCursor(value string) *app.ActivityKey {
	millis, rest, ok := strings.Cut(value, ".")
	id, kind, _ := strings.Cut(rest, ".")
	created, err := strconv.ParseInt(millis, 10, 64)
	if !ok || err != nil || created < 0 || !validResourceID(id) ||
		(kind != string(app.WalletConnection) && kind != string(app.WalletTransaction)) {
		return nil
	}
	return &app.ActivityKey{CreatedAt: time.UnixMilli(created), ID: id, Kind: app.WalletRequestKind(kind)}
}

// @Summary List local wallet activity
// @Description Lists connection and transaction requests newest first, including pending requests and the latest wallet outcome. Requires a browser management cookie. Reading activity does not renew a Session or claim the wallet.
// @Produce json
// @Security BrowserCookie
// @Param limit query int false "Page size (default 50)" minimum(1) maximum(50)
// @Param after query string false "Pass nextAfter from the previous page"
// @Success 200 {object} ActivityListResponse
// @Failure 400,401,403,405,500 {object} ErrorResponse
// @Router /browser/activity [get]
func (h *handler) listActivity(w http.ResponseWriter, r *http.Request, grant app.BrowserGrant) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		failure(w, http.StatusBadRequest, "bad_request", "Activity reads accept no body")
		return
	}
	limit, query, ok := paginationQuery(w, r)
	if !ok {
		return
	}
	var after *app.ActivityKey
	if query.Has("after") {
		after = parseActivityCursor(query.Get("after"))
		if after == nil {
			failure(w, http.StatusBadRequest, "bad_request", "Invalid activity cursor")
			return
		}
	}
	records, err := h.config.Connections.Activity(r.Context(), grant, after, limit+1)
	if err != nil {
		connectionFailure(w, err)
		return
	}
	var result ActivityListResponse
	if len(records) > limit {
		result.NextAfter = activityCursor(records[limit-1].ActivityKey)
		records = records[:limit]
	}
	result.Activities = make([]ActivityResponse, len(records))
	for i, record := range records {
		result.Activities[i] = ActivityResponse{Kind: string(record.Kind), ID: record.ID, CreatedAt: timestamp(record.CreatedAt),
			ClientName: record.ClientName, ChainID: record.ChainID, State: record.State, SessionID: record.SessionID, TxHash: record.TxHash}
	}
	writeJSON(w, http.StatusOK, result)
}
