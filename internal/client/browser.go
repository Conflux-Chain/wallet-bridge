package client

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
)

// BrowserLink ensures the service is running before issuing a one-use link.
// Callers must explicitly choose whether to open or disclose the returned URL.
func (c *Client) BrowserLink(ctx context.Context) (api.BrowserTicketResponse, error) {
	if _, err := c.Ensure(ctx); err != nil {
		return api.BrowserTicketResponse{}, err
	}
	data, err := c.browserRequest(ctx, "POST", "/api/v1/control/browser-grants", nil, 201)
	if err != nil {
		return api.BrowserTicketResponse{}, err
	}
	var result api.BrowserTicketResponse
	const prefix = api.Origin + "/#authorize="
	if json.Unmarshal(data, &result) != nil || !strings.HasPrefix(result.URL, prefix) || !validHex(strings.TrimPrefix(result.URL, prefix), 32) {
		return api.BrowserTicketResponse{}, invalidBrowserResponse()
	}
	if expires, err := time.Parse(time.RFC3339Nano, result.ExpiresAt); err != nil || !expires.After(time.Now()) {
		return api.BrowserTicketResponse{}, invalidBrowserResponse()
	}
	return result, nil
}

// ListBrowsers returns one bounded page without starting a stopped service.
func (c *Client) ListBrowsers(ctx context.Context, limit int, after string) (api.BrowserGrantListResponse, error) {
	if limit < 1 || limit > 50 || (after != "" && !validHex(after, 16)) {
		return api.BrowserGrantListResponse{}, &Error{"bad_request", "Use a page size between 1 and 50 and a lowercase grant ID cursor", nil}
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if after != "" {
		query.Set("after", after)
	}
	data, err := c.browserRequest(ctx, "GET", "/api/v1/control/browser-grants?"+query.Encode(), nil, 200)
	if err != nil {
		return api.BrowserGrantListResponse{}, err
	}
	var result api.BrowserGrantListResponse
	if json.Unmarshal(data, &result) != nil || result.Grants == nil || len(result.Grants) > limit ||
		(result.NextAfter != "" && (len(result.Grants) == 0 || result.NextAfter != result.Grants[len(result.Grants)-1].ID)) {
		return api.BrowserGrantListResponse{}, invalidBrowserResponse()
	}
	for _, grant := range result.Grants {
		if !validHex(grant.ID, 16) || !validTimestamp(grant.IssuedAt) || !validTimestamp(grant.ExpiresAt) ||
			(grant.RevokedAt != "" && !validTimestamp(grant.RevokedAt)) {
			return api.BrowserGrantListResponse{}, invalidBrowserResponse()
		}
		switch grant.State {
		case "active", "expired", "revoked", "not_yet_valid":
		default:
			return api.BrowserGrantListResponse{}, invalidBrowserResponse()
		}
	}
	return result, nil
}

func (c *Client) RevokeBrowser(ctx context.Context, id string, all bool) (api.BrowserRevokeResponse, error) {
	if (id == "" && !all) || (id != "" && (all || !validHex(id, 16))) {
		return api.BrowserRevokeResponse{}, &Error{"bad_request", "Choose one lowercase grant ID or --all", nil}
	}
	body, _ := json.Marshal(api.BrowserRevokeRequest{ID: id, All: all})
	data, err := c.browserRequest(ctx, "POST", "/api/v1/control/browser-grants/revoke", body, 200)
	if err != nil {
		return api.BrowserRevokeResponse{}, err
	}
	var result api.BrowserRevokeResponse
	if json.Unmarshal(data, &result) != nil || result.State != "revoked" {
		return api.BrowserRevokeResponse{}, invalidBrowserResponse()
	}
	return result, nil
}

func (c *Client) browserRequest(ctx context.Context, method, path string, body []byte, expected int) ([]byte, error) {
	data, _, err := c.request(ctx, method, path, requestOptions{control: true, browser: true, body: body, expected: expected})
	if ctx.Err() != nil {
		return nil, &Error{"canceled", "The browser authorization operation was canceled; query state before retrying a change", ctx.Err()}
	}
	if errors.Is(err, errNotRunning) {
		return nil, &Error{"service_not_running", "Start the local service with service ensure before managing browser authorizations", err}
	}
	return data, err
}

func validHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size && hex.EncodeToString(decoded) == value
}

func validTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func invalidBrowserResponse() error {
	return &Error{"request_failed", "The service returned an invalid browser authorization response", nil}
}

// Never pass service-supplied messages or response bodies to a terminal.
func browserResponseError(err error) error {
	var response *responseError
	if errors.As(err, &response) {
		var failure api.ErrorResponse
		if json.Unmarshal(response.body, &failure) == nil {
			messages := map[string]struct {
				status  int
				message string
			}{
				"bad_request":             {400, "The browser authorization parameters were rejected"},
				"unauthenticated":         {401, "The local control credential was rejected"},
				"forbidden":               {403, "The service denied control access"},
				"browser_grant_not_found": {404, "The browser authorization does not exist"},
				"authorization_limit":     {429, "Too many outstanding authorization links; wait two minutes before requesting another"},
				"authorization_error":     {500, "The service could not complete browser authorization; inspect the local service"},
			}
			if mapped, ok := messages[failure.Code]; ok && response.status == mapped.status {
				return &Error{failure.Code, mapped.message, err}
			}
		}
	}
	return &Error{"request_failed", "The verified service returned an unexpected browser authorization response; query state before retrying a change", err}
}
