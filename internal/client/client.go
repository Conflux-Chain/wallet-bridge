// Package client connects CLI and MCP callers to the fixed local service.
package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Cause }

type Status struct {
	State     string            `json:"state"`
	Origin    string            `json:"origin"`
	StartedAt string            `json:"startedAt,omitempty"`
	Info      *api.InfoResponse `json:"info,omitempty"`
}

type Client struct {
	dir              string
	dial             func(context.Context, string, string) (net.Conn, error)
	start            func(string) (*platform.BackgroundProcess, error)
	operationTimeout time.Duration
	ensureTimeout    time.Duration
	stopTimeout      time.Duration
	interval         time.Duration
}

func New(dir string) (*Client, error) {
	if dir == "" {
		var err error
		dir, err = platform.DataDir()
		if err != nil {
			return nil, &Error{"data_unavailable", "Cannot resolve the user data directory", err}
		}
	}
	if !filepath.IsAbs(dir) {
		return nil, &Error{"data_unavailable", "The data directory must be absolute", nil}
	}
	return &Client{
		dir: dir, dial: (&net.Dialer{}).DialContext, start: platform.StartBackground,
		operationTimeout: 2 * time.Second, ensureTimeout: 15 * time.Second, stopTimeout: 5 * time.Second, interval: 100 * time.Millisecond,
	}, nil
}

var errNotRunning = errors.New("service is not listening")
var errConnectionChanged = errors.New("the verified connection closed")

type requestOptions struct {
	capability string
	body       []byte
	expected   int
}

const (
	browserCapability    = "browser.authorization"
	connectionCapability = "connections.lifecycle"
	defaultResponseLimit = 16 << 10
	// The service may escape each byte of an 8 KiB input as six JSON bytes.
	connectionResponseLimit = 8 * api.MaxConnectionBody
)

// request pins the proof and authenticated request to a single connection.
// A transport retry cannot dial a replacement endpoint with a Bearer token.
func (c *Client) request(ctx context.Context, method, path string, options requestOptions) ([]byte, api.InfoResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.operationTimeout)
	defer cancel()
	var empty api.InfoResponse
	conn, err := c.dial(ctx, "tcp4", api.Address)
	if err != nil {
		if platform.IsConnectionRefused(err) {
			return nil, empty, errNotRunning
		}
		return nil, empty, &Error{"service_unavailable", "Cannot connect to the fixed local service", err}
	}
	defer conn.Close()
	token, err := credentials.Read(c.dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, empty, &Error{"credential_error", "Cannot read a valid private service credential", err}
		}
		// A service may have bound the port while initializing its credentials.
		if locked, _ := platform.ServiceLocked(c.dir); locked {
			return nil, empty, &Error{"service_unavailable", "The service has not completed credential initialization", err}
		}
		return nil, empty, &Error{"identity_unverified", "Cannot verify the service with this data directory", err}
	}
	var supplied atomic.Bool
	transport := &http.Transport{
		Proxy: nil, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, MaxResponseHeaderBytes: 8 << 10,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			if supplied.Swap(true) {
				return nil, errConnectionChanged
			}
			return conn, nil
		},
	}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: c.operationTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, empty, &Error{"identity_unverified", "Cannot generate a service challenge", err}
	}
	challenge := hex.EncodeToString(nonce[:])
	probe, _ := http.NewRequestWithContext(ctx, "GET", api.Origin+"/api/v1/info?challenge="+challenge, nil)
	response, err := httpClient.Do(probe)
	if err != nil {
		return nil, empty, &Error{"service_unavailable", "The service did not answer its identity challenge", err}
	}
	data, err := readResponse(response, http.StatusOK, defaultResponseLimit)
	var info api.InfoResponse
	if err != nil || json.Unmarshal(data, &info) != nil || !verify(info, challenge, token) {
		return nil, empty, &Error{"identity_unverified", "The service identity proof is invalid", err}
	}
	if info.APIVersion.Major != 1 || info.APIVersion.Minor < 2 || !hasCapabilities(info.Capabilities) {
		return nil, empty, &Error{"incompatible_service", "The service API or required capabilities are incompatible; use its matching program to stop it before changing versions", nil}
	}
	if options.capability != "" && !slices.Contains(info.Capabilities, options.capability) ||
		options.capability == connectionCapability && info.APIVersion.Minor < 3 {
		return nil, empty, &Error{"incompatible_service", "The service does not support the requested operation; use its matching program to stop it before changing versions", nil}
	}
	if !info.Ready {
		return nil, empty, &Error{"service_unavailable", "The verified service is not ready", nil}
	}
	req, _ := http.NewRequestWithContext(ctx, method, api.Origin+path, bytes.NewReader(options.body))
	req.Header.Set("Authorization", "Bearer "+token)
	if len(options.body) != 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err = httpClient.Do(req)
	if err != nil {
		if errors.Is(err, errConnectionChanged) {
			return nil, empty, &Error{"identity_unverified", "The verified service connection closed; retry the command to verify again", err}
		}
		return nil, empty, &Error{"request_failed", "The verified service request failed", err}
	}
	limit := defaultResponseLimit
	if options.capability == connectionCapability {
		limit = connectionResponseLimit
	}
	data, err = readResponse(response, options.expected, limit)
	if err != nil {
		switch options.capability {
		case browserCapability:
			return nil, empty, browserResponseError(err)
		case connectionCapability:
			return nil, empty, connectionResponseError(err)
		}
		return nil, empty, &Error{"request_failed", "The verified service returned an unexpected response", err}
	}
	info.Challenge, info.Proof = "", ""
	return data, info, nil
}

func readResponse(response *http.Response, expected, limit int) ([]byte, error) {
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	if response.StatusCode != expected {
		return nil, &responseError{status: response.StatusCode, body: data}
	}
	return data, nil
}

type responseError struct {
	status int
	body   []byte
}

func (*responseError) Error() string { return "unexpected HTTP status" }

func verify(info api.InfoResponse, challenge, token string) bool {
	if info.Name != "wallet-bridge" || info.Challenge != challenge {
		return false
	}
	proof, err := hex.DecodeString(info.Proof)
	if err != nil || len(proof) != 32 || hex.EncodeToString(proof) != info.Proof {
		return false
	}
	key, err := hex.DecodeString(token)
	if err != nil {
		return false
	}
	info.Proof = ""
	data, err := json.Marshal(info)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("wallet-bridge.info.v1\n"))
	mac.Write(data)
	return hmac.Equal(proof, mac.Sum(nil))
}

func hasCapabilities(capabilities []string) bool {
	return slices.Contains(capabilities, "service.status") &&
		slices.Contains(capabilities, "service.stop") && slices.Contains(capabilities, "service.auth.shared")
}
