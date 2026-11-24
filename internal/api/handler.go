package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
)

type Config struct {
	Version              string
	ServiceToken         string
	Assets               fs.FS
	StartedAt            time.Time
	Stop                 func()
	BrowserAuthorization *app.BrowserAuthorization
	Connections          *app.ConnectionService
	Transactions         TransactionRequests
}

type handler struct {
	config Config
	info   InfoResponse
	key    []byte
}

func New(config Config) (http.Handler, error) {
	key, err := hex.DecodeString(config.ServiceToken)
	if err != nil || len(key) != 32 || hex.EncodeToString(key) != config.ServiceToken ||
		config.Stop == nil || config.BrowserAuthorization == nil || config.Connections == nil || config.Transactions == nil {
		return nil, errors.New("invalid local API configuration")
	}
	return &handler{config: config, key: key, info: InfoResponse{
		Name: "wallet-bridge", Version: config.Version, APIVersion: APIVersion{Major: 1, Minor: 11},
		Capabilities: []string{"service.status", "service.stop", "browser.authorization", "service.auth.shared", "connections.lifecycle", "browser.connections.coordination", "browser.connections.decisions", "browser.wallet-operation", "transactions.lifecycle", "browser.transactions.read", "browser.transactions.coordination", "browser.transactions.decisions"}, Ready: true,
	}}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Host != Address {
		failure(w, http.StatusForbidden, "invalid_host", "The fixed loopback Host is required")
		return
	}
	if origins := r.Header.Values("Origin"); len(origins) != 0 && (len(origins) != 1 || origins[0] != Origin) {
		failure(w, http.StatusForbidden, "invalid_origin", "Origin must match the local service")
		return
	}
	switch r.URL.Path {
	case "/api/v1/info":
		if method(w, r, http.MethodGet) {
			h.getInfo(w, r)
		}
	case "/api/v1/status":
		if method(w, r, http.MethodGet) {
			if grant, ok := h.authorizeStatus(w, r); ok {
				h.getStatus(w, r, grant)
			}
		}
	case "/api/v1/control/stop":
		if method(w, r, http.MethodPost) && h.authorize(w, r) {
			h.stop(w, r)
		}
	case "/api/v1/control/browser-grants":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			failure(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported HTTP method")
		} else if h.authorize(w, r) {
			if r.Method == http.MethodPost {
				h.issueBrowserTicket(w, r)
			} else {
				h.listBrowserGrants(w, r)
			}
		}
	case "/api/v1/control/browser-grants/revoke":
		if method(w, r, http.MethodPost) && h.authorize(w, r) {
			h.revokeBrowserGrants(w, r)
		}
	case "/api/v1/browser/authorize":
		if method(w, r, http.MethodPost) && sameOrigin(w, r) {
			h.authorizeBrowser(w, r)
		}
	case "/api/v1/browser/logout":
		if method(w, r, http.MethodPost) && sameOrigin(w, r) {
			h.logoutBrowser(w, r)
		}
	default:
		if h.routeBrowserTransactions(w, r) {
			return
		}
		if h.routeBrowserConnections(w, r) {
			return
		}
		if h.routeClientRequests(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			failure(w, http.StatusNotFound, "not_found", "Unknown API endpoint")
			return
		}
		h.static(w, r)
	}
}

func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	failure(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported HTTP method")
	return false
}

func (h *handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Authorization")
	var token string
	if len(values) == 1 {
		parts := strings.Fields(values[0])
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token = parts[1]
		}
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.config.ServiceToken)) == 1 {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="wallet-bridge"`)
	failure(w, http.StatusUnauthorized, "unauthenticated", "A valid Bearer token is required")
	return false
}

// getInfo reports readiness and optionally proves service identity.
// @Summary Read service version and capabilities
// @Description A 64-character lowercase hexadecimal challenge produces an HMAC-SHA256 proof binding the response metadata. No credentials or data directory are returned.
// @Produce json
// @Param challenge query string false "32-byte nonce encoded as lowercase hexadecimal" minlength(64) maxlength(64)
// @Success 200 {object} InfoResponse
// @Failure 400 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 405 {object} ErrorResponse
// @Router /info [get]
func (h *handler) getInfo(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	response := h.info
	if err != nil || len(query) > 1 || (len(query) == 1 && len(query["challenge"]) != 1) {
		failure(w, http.StatusBadRequest, "bad_request", "Only a single challenge parameter is accepted")
		return
	}
	if len(query) != 0 {
		challenge := query.Get("challenge")
		decoded, err := hex.DecodeString(challenge)
		if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != challenge {
			failure(w, http.StatusBadRequest, "bad_request", "Challenge must encode 32 bytes as lowercase hexadecimal")
			return
		}
		response.Challenge = challenge
		data, _ := json.Marshal(response)
		mac := hmac.New(sha256.New, h.key)
		mac.Write([]byte("wallet-bridge.info.v1\n"))
		mac.Write(data)
		response.Proof = hex.EncodeToString(mac.Sum(nil))
	}
	writeJSON(w, http.StatusOK, response)
}

// getStatus reports the running service's state.
// @Summary Read authenticated service status
// @Description With an active browser cookie, activeWalletRequest identifies the connection or transaction occupying the wallet. For connections, walletOperation also supplies the API 1.6 representation. Both fields are omitted for an idle wallet and for service Bearer callers. Reading status does not renew or acquire an execution.
// @Produce json
// @Security ServiceBearer
// @Security BrowserCookie
// @Success 200 {object} StatusResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 405 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /status [get]
func (h *handler) getStatus(w http.ResponseWriter, r *http.Request, browser *app.BrowserGrant) {
	response := StatusResponse{
		State: "running", StartedAt: h.config.StartedAt.UTC().Format(time.RFC3339Nano), Info: h.info,
	}
	if browser != nil {
		metadata := browserGrantResponse(*browser, time.Now())
		response.Browser = &metadata
		occupied, err := h.config.Connections.ActiveWalletRequest(r.Context(), *browser)
		if err != nil {
			connectionFailure(w, err)
			return
		}
		if occupied != nil {
			response.ActiveWalletRequest = &ActiveWalletRequestResponse{Kind: string(occupied.Kind), ID: occupied.ID, Phase: string(occupied.Phase)}
			if occupied.Kind == app.WalletConnection {
				response.WalletOperation = &WalletOperationResponse{ConnectionID: occupied.ID, Phase: string(occupied.Phase)}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

// stop requests graceful shutdown; the response acknowledges the request.
// @Summary Request graceful service shutdown
// @Description Requires the local service token. Accepts no body or an empty JSON object, limited to 1024 bytes. HTTP 202 means shutdown was requested, not that the process has exited.
// @Accept json
// @Produce json
// @Security ServiceBearer
// @Success 202 {object} StopResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 405 {object} ErrorResponse
// @Failure 413 {object} ErrorResponse
// @Failure 415 {object} ErrorResponse
// @Router /control/stop [post]
func (h *handler) stop(w http.ResponseWriter, r *http.Request) {
	if !decodeBody(w, r, &struct{}{}, true) {
		return
	}
	writeJSON(w, http.StatusAccepted, StopResponse{State: "stopping"})
	h.config.Stop()
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool) bool {
	return decodeBodyLimit(w, r, target, allowEmpty, MaxControlBody)
}

func decodeBodyLimit(w http.ResponseWriter, r *http.Request, target any, allowEmpty bool, limit int64) bool {
	if r.URL.RawQuery != "" {
		failure(w, http.StatusBadRequest, "bad_request", "Query parameters are not accepted")
		return false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			failure(w, http.StatusRequestEntityTooLarge, "request_too_large", fmt.Sprintf("Request exceeds %d bytes", limit))
		} else {
			failure(w, http.StatusBadRequest, "bad_request", "Cannot read request body")
		}
		return false
	}
	if data = bytes.TrimSpace(data); len(data) != 0 {
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			failure(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "A JSON request body requires application/json")
			return false
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if data[0] != '{' || decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			failure(w, http.StatusBadRequest, "bad_request", "Expected one JSON object with only supported fields")
			return false
		}
	} else if !allowEmpty {
		failure(w, http.StatusBadRequest, "bad_request", "A JSON request body is required")
		return false
	}
	return true
}

func (h *handler) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		failure(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported HTTP method")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if h.config.Assets == nil || !fs.ValidPath(name) {
		failure(w, http.StatusNotFound, "not_found", "Unknown page resource")
		return
	}
	data, err := fs.ReadFile(h.config.Assets, name)
	if err != nil {
		failure(w, http.StatusNotFound, "not_found", "Unknown page resource")
		return
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func failure(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: message})
}
