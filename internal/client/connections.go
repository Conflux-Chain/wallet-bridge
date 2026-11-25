package client

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
)

func NewConnectionID() string { return NewRequestID() }

const connectionsPath = "/api/v1/connections"
const sessionsPath = "/api/v1/sessions/"

// EnsureConnectionApproval starts or reuses a service with browser consent support.
func (c *Client) EnsureConnectionApproval(ctx context.Context) error {
	status, err := c.Ensure(ctx)
	if err != nil {
		return err
	}
	if status.Info == nil || !slices.Contains(status.Info.Capabilities, "browser.connections.decisions") {
		return &Error{Code: "incompatible_service", Message: "The service does not support browser connection approval"}
	}
	return nil
}

// CreateConnection submits once. Keep input to reconcile a lost response by ID.
func (c *Client) CreateConnection(ctx context.Context, input api.ConnectionCreateRequest) (api.ConnectionResponse, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return api.ConnectionResponse{}, &Error{"bad_request", "Cannot encode the connection request", err}
	}
	return c.connection(ctx, input.ID, http.MethodPost, connectionsPath, body)
}

// GetConnection reads the request without renewing the resulting Session.
func (c *Client) GetConnection(ctx context.Context, id string) (api.ConnectionResponse, error) {
	return c.connection(ctx, id, http.MethodGet, connectionsPath+"/"+id, nil)
}

func (c *Client) CancelConnection(ctx context.Context, id string) (api.ConnectionResponse, error) {
	result, err := c.connection(ctx, id, http.MethodPost, connectionsPath+"/"+id+"/cancel", nil)
	if err == nil && result.State != "canceled" {
		return api.ConnectionResponse{}, invalidLifecycleResponse()
	}
	return result, err
}

// GetSession renews an active Session or returns its closed history.
func (c *Client) GetSession(ctx context.Context, id string) (api.SessionResponse, error) {
	return c.session(ctx, id, http.MethodGet, sessionsPath+id)
}

func (c *Client) CloseSession(ctx context.Context, id string) (api.SessionResponse, error) {
	result, err := c.session(ctx, id, http.MethodPost, sessionsPath+id+"/close")
	if err == nil && result.State != "closed" {
		return api.SessionResponse{}, invalidLifecycleResponse()
	}
	return result, err
}

func (c *Client) lifecycleRequest(ctx context.Context, capability, id, method, path string, body []byte) ([]byte, error) {
	if !validHex(id, 16) {
		return nil, &Error{"bad_request", "Use a 32-character lowercase hexadecimal resource ID", nil}
	}
	data, _, err := c.request(ctx, method, path, requestOptions{capability: capability, body: body, expected: http.StatusOK})
	if err != nil && ctx.Err() != nil {
		return nil, &Error{"canceled", "The operation was interrupted; query the original resource ID to check its state", ctx.Err()}
	}
	if errors.Is(err, errNotRunning) {
		return nil, &Error{"service_not_running", "Start the local service with service ensure", err}
	}
	return data, err
}

var canonicalChainID = regexp.MustCompile(`^0x(0|[1-9a-f][0-9a-f]*)$`)

func (c *Client) connection(ctx context.Context, id, method, path string, body []byte) (api.ConnectionResponse, error) {
	data, err := c.lifecycleRequest(ctx, connectionCapability, id, method, path, body)
	if err != nil {
		return api.ConnectionResponse{}, err
	}
	var result api.ConnectionResponse
	if json.Unmarshal(data, &result) != nil || result.ID != id || strings.TrimSpace(result.ClientName) == "" ||
		!canonicalChainID.MatchString(result.ChainID) || !validTimestamp(result.CreatedAt) || !validTimestamp(result.ExpiresAt) {
		return api.ConnectionResponse{}, invalidLifecycleResponse()
	}
	valid := false
	switch result.State {
	case "pending":
		valid = result.FinishedAt == "" && result.SessionID == ""
	case "succeeded":
		valid = validTimestamp(result.FinishedAt) && validHex(result.SessionID, 16)
	case "rejected", "canceled", "expired":
		valid = validTimestamp(result.FinishedAt) && result.SessionID == ""
	}
	if !valid {
		return api.ConnectionResponse{}, invalidLifecycleResponse()
	}
	return result, nil
}

func (c *Client) session(ctx context.Context, id, method, path string) (api.SessionResponse, error) {
	data, err := c.lifecycleRequest(ctx, connectionCapability, id, method, path, nil)
	if err != nil {
		return api.SessionResponse{}, err
	}
	var result api.SessionResponse
	if json.Unmarshal(data, &result) != nil || result.ID != id || !validHex(result.ConnectionID, 16) ||
		!strings.HasPrefix(result.Account, "0x") || !validHex(result.Account[2:], 20) ||
		!canonicalChainID.MatchString(result.ChainID) || !validTimestamp(result.CreatedAt) || !validTimestamp(result.LastAccessAt) {
		return api.SessionResponse{}, invalidLifecycleResponse()
	}
	wallet := result.Wallet
	validWallet := wallet.Kind == "legacy" || wallet.Kind == "eip6963" && strings.TrimSpace(wallet.RDNS) != ""
	validState := result.State == "active" && result.ClosedAt == "" || result.State == "closed" && validTimestamp(result.ClosedAt)
	if strings.TrimSpace(wallet.Name) == "" || !validWallet || !validState {
		return api.SessionResponse{}, invalidLifecycleResponse()
	}
	return result, nil
}

func invalidLifecycleResponse() error {
	return &Error{"request_failed", "The service returned an invalid connection or Session response", nil}
}

// Expose recognized status/code pairs with local messages safe for CLI or MCP.
func lifecycleResponseError(err error, capability string) error {
	var response *responseError
	var failure api.ErrorResponse
	if errors.As(err, &response) && json.Unmarshal(response.body, &failure) == nil {
		type mappedError struct {
			status  int
			message string
		}
		mapped := map[string]mappedError{
			"unauthenticated":        {401, "The local service credential was rejected"},
			"invalid_host":           {403, "The service rejected the request host"},
			"invalid_origin":         {403, "The service rejected the request origin"},
			"session_not_found":      {404, "The Session does not exist"},
			"method_not_allowed":     {405, "The service rejected the HTTP method"},
			"unsupported_media_type": {415, "The service rejected the request media type"},
			"storage_error":          {500, "The service could not complete the storage operation"},
		}
		if capability == transactionCapability {
			mapped["bad_request"] = mappedError{400, transactionParameterMessage(failure.Message)}
			mapped["transaction_not_found"] = mappedError{404, "The transaction request does not exist"}
			mapped["transaction_conflict"] = mappedError{409, "The request ID already identifies different transaction content"}
			mapped["session_closed"] = mappedError{409, "A new transaction requires an active Session"}
			mapped["request_too_large"] = mappedError{413, "The transaction request exceeds the 1 MiB limit"}
		} else {
			mapped["bad_request"] = mappedError{400, "Invalid connection or Session parameters"}
			mapped["connection_not_found"] = mappedError{404, "The connection request does not exist"}
			mapped["connection_conflict"] = mappedError{409, "The input or outcome conflicts with the original request; query its ID"}
			mapped["request_too_large"] = mappedError{413, "The connection request exceeds the service size limit"}
		}
		if entry, ok := mapped[failure.Code]; ok && entry.status == response.status {
			return &Error{failure.Code, entry.message, err}
		}
	}
	return &Error{"request_failed", "The verified service returned an unexpected response; query the original resource ID", err}
}
