package api

import (
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

const browserCookieName = "wallet_bridge_browser"
const browserCookiePath = "/api/v1"

func sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 1 && values[0] == Origin {
		return true
	}
	failure(w, http.StatusForbidden, "invalid_origin", "The browser request requires the exact local Origin")
	return false
}

func browserCookie(r *http.Request) (string, bool) {
	var token string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == browserCookieName {
			token = cookie.Value
			count++
		}
	}
	return token, count == 1 && token != ""
}

func (h *handler) authorizeStatus(w http.ResponseWriter, r *http.Request) (*app.BrowserGrant, bool) {
	if len(r.Header.Values("Authorization")) != 0 {
		return nil, h.authorize(w, r, false)
	}
	token, ok := browserCookie(r)
	if !ok {
		authorizationFailure(w, app.ErrInvalidBrowserCredential)
		return nil, false
	}
	grant, err := h.config.BrowserAuthorization.Authenticate(r.Context(), token)
	if err != nil {
		authorizationFailure(w, err)
		return nil, false
	}
	return &grant, true
}

// issueBrowserTicket grants a short-lived, one-use authorization URL.
// @Summary Issue a browser authorization link
// @Description Only the control token can issue links. The URL contains a two-minute, one-use secret in its fragment. Do not log or expose it in ordinary status output. Accepts no body or an empty JSON object.
// @Accept json
// @Produce json
// @Security ControlBearer
// @Success 201 {object} BrowserTicketResponse
// @Failure 400,401,403,405,413,415,429,500 {object} ErrorResponse
// @Router /control/browser-grants [post]
func (h *handler) issueBrowserTicket(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	ticket, err := h.config.BrowserAuthorization.Issue(r.Context())
	if err != nil {
		authorizationFailure(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, BrowserTicketResponse{
		URL: Origin + "/#authorize=" + ticket.Token, ExpiresAt: timestamp(ticket.ExpiresAt),
	})
}

// authorizeBrowser consumes a ticket and sets a persistent browser cookie.
// @Summary Exchange a one-time ticket for browser authorization
// @Description Requires Origin http://127.0.0.1:18765, including for native HTTP callers. The ticket is consumed once; after a failed or lost exchange request a new link. Cookie is host-only, HttpOnly, SameSite=Strict, Path=/api/v1, with fixed expiration. The HTTP loopback cookie does not use Secure. JSON never returns the cookie secret. Request body is limited to 1024 bytes; query parameters are rejected.
// @Accept json
// @Produce json
// @Param Origin header string true "Exact local service origin"
// @Param request body BrowserAuthorizeRequest true "One-use ticket from the link fragment"
// @Success 200 {object} BrowserGrantResponse
// @Header 200 {string} Set-Cookie "Persistent browser management cookie"
// @Failure 400,401,403,405,409,413,415,500 {object} ErrorResponse
// @Router /browser/authorize [post]
func (h *handler) authorizeBrowser(w http.ResponseWriter, r *http.Request) {
	var request BrowserAuthorizeRequest
	if !decodeBody(w, r, &request, false) {
		return
	}
	result, err := h.config.BrowserAuthorization.Authorize(r.Context(), request.Ticket)
	if err != nil {
		authorizationFailure(w, err)
		return
	}
	cookie := managementCookie(result.Token)
	cookie.Expires = result.Grant.ExpiresAt
	cookie.MaxAge = max(1, int(time.Until(result.Grant.ExpiresAt)/time.Second))
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, browserGrantResponse(result.Grant, time.Now()))
}

// logoutBrowser revokes only the current browser's persistent authorization.
// @Summary Log out the current browser
// @Description Requires the browser cookie and exact local Origin, even when a Bearer token is also present. Revokes the persistent grant before expiring its cookie. Accepts no body or an empty JSON object, limited to 1024 bytes; query parameters are rejected.
// @Accept json
// @Produce json
// @Security BrowserCookie
// @Param Origin header string true "Exact local service origin"
// @Success 200 {object} BrowserRevokeResponse
// @Header 200 {string} Set-Cookie "Expires the current browser management cookie"
// @Failure 400,401,403,405,413,415,500 {object} ErrorResponse
// @Router /browser/logout [post]
func (h *handler) logoutBrowser(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	token, ok := browserCookie(r)
	if !ok {
		authorizationFailure(w, app.ErrInvalidBrowserCredential)
		return
	}
	if err := h.config.BrowserAuthorization.Logout(r.Context(), token); err != nil {
		authorizationFailure(w, err)
		return
	}
	cookie := managementCookie("")
	cookie.MaxAge, cookie.Expires = -1, time.Unix(1, 0).UTC()
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, BrowserRevokeResponse{State: "revoked"})
}

// listBrowserGrants exposes only management metadata to the local controller.
// @Summary List browser authorizations
// @Description Includes active, expired and revoked records, newest first. The cursor is the last grant ID from the preceding page. Browser cookies and client tokens cannot list grants. Secrets and their digests are never returned.
// @Produce json
// @Security ControlBearer
// @Param limit query int false "Page size (default 50)" minimum(1) maximum(50)
// @Param after query string false "Last grant ID of the preceding page"
// @Success 200 {object} BrowserGrantListResponse
// @Failure 400,401,403,405,500 {object} ErrorResponse
// @Router /control/browser-grants [get]
func (h *handler) listBrowserGrants(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	limit, after := 50, query.Get("after")
	if err != nil || len(query) > 2 {
		failure(w, http.StatusBadRequest, "bad_request", "Invalid pagination query")
		return
	}
	for key, values := range query {
		if (key != "limit" && key != "after") || len(values) != 1 {
			failure(w, http.StatusBadRequest, "bad_request", "Only single limit and after parameters are accepted")
			return
		}
	}
	if values, exists := query["limit"]; exists {
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 1 || limit > 50 {
			failure(w, http.StatusBadRequest, "bad_request", "Page size must be between 1 and 50")
			return
		}
	}
	grants, err := h.config.BrowserAuthorization.List(r.Context())
	if err != nil {
		authorizationFailure(w, err)
		return
	}
	start := 0
	if _, exists := query["after"]; exists {
		start = -1
		if validGrantID(after) {
			for index, grant := range grants {
				if grant.ID == after {
					start = index + 1
					break
				}
			}
		}
		if start < 0 {
			failure(w, http.StatusBadRequest, "bad_request", "The pagination cursor is invalid")
			return
		}
	}
	end := min(start+limit, len(grants))
	response := BrowserGrantListResponse{Grants: []BrowserGrantResponse{}}
	now := time.Now()
	for _, grant := range grants[start:end] {
		response.Grants = append(response.Grants, browserGrantResponse(grant, now))
	}
	if end < len(grants) {
		response.NextAfter = grants[end-1].ID
	}
	writeJSON(w, http.StatusOK, response)
}

// revokeBrowserGrants invalidates a named grant or all grants and pending links.
// @Summary Revoke browser authorization
// @Description Requires the control token. Choose a single grant id or all=true, exclusively. Repeating revocation of an existing record succeeds without changing its first revocation time. Revoke-all also invalidates all outstanding authorization links. Request body is limited to 1024 bytes; query parameters are rejected.
// @Accept json
// @Produce json
// @Security ControlBearer
// @Param request body BrowserRevokeRequest true "Grant ID or all=true"
// @Success 200 {object} BrowserRevokeResponse
// @Failure 400,401,403,404,405,413,415,500 {object} ErrorResponse
// @Router /control/browser-grants/revoke [post]
func (h *handler) revokeBrowserGrants(w http.ResponseWriter, r *http.Request) {
	var request BrowserRevokeRequest
	if !decodeBody(w, r, &request, false) {
		return
	}
	if (request.ID == "" && !request.All) || (request.ID != "" && (request.All || !validGrantID(request.ID))) {
		failure(w, http.StatusBadRequest, "bad_request", "Choose a lowercase grant ID or all=true, exclusively")
		return
	}
	var err error
	if request.All {
		_, err = h.config.BrowserAuthorization.RevokeAll(r.Context())
	} else {
		err = h.config.BrowserAuthorization.Revoke(r.Context(), request.ID)
	}
	if err != nil {
		authorizationFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, BrowserRevokeResponse{State: "revoked"})
}

func managementCookie(token string) *http.Cookie {
	return &http.Cookie{Name: browserCookieName, Value: token, Path: browserCookiePath, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func browserGrantResponse(grant app.BrowserGrant, now time.Time) BrowserGrantResponse {
	response := BrowserGrantResponse{ID: grant.ID, IssuedAt: timestamp(grant.IssuedAt), ExpiresAt: timestamp(grant.ExpiresAt), State: "active"}
	switch {
	case grant.RevokedAt != nil:
		response.RevokedAt, response.State = timestamp(*grant.RevokedAt), "revoked"
	case !now.Before(grant.ExpiresAt):
		response.State = "expired"
	case now.Before(grant.IssuedAt):
		response.State = "not_yet_valid"
	}
	return response
}

func validGrantID(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16 && hex.EncodeToString(value) == id
}

func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func authorizationFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidBrowserTicket):
		failure(w, http.StatusUnauthorized, "invalid_browser_ticket", "The authorization link is invalid, expired or already consumed; request a new link")
	case errors.Is(err, app.ErrInvalidBrowserCredential):
		failure(w, http.StatusUnauthorized, "unauthenticated", "A valid browser authorization is required")
	case errors.Is(err, app.ErrBrowserTicketLimit):
		failure(w, http.StatusTooManyRequests, "authorization_limit", "Too many outstanding authorization links; wait for expiration")
	case errors.Is(err, app.ErrBrowserGrantNotFound):
		failure(w, http.StatusNotFound, "browser_grant_not_found", "The browser grant does not exist")
	case errors.Is(err, app.ErrInvalidBrowserGrant):
		failure(w, http.StatusBadRequest, "bad_request", "Invalid browser grant metadata")
	case errors.Is(err, app.ErrBrowserGrantExists):
		failure(w, http.StatusConflict, "authorization_conflict", "Cannot create a unique authorization; request a new link")
	default:
		failure(w, http.StatusInternalServerError, "authorization_error", "Cannot complete the browser authorization operation")
	}
}
