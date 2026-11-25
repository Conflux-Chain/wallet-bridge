package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type requestLookup struct {
	RequestID string `json:"requestId" jsonschema:"Original request ID as 32 lowercase hexadecimal characters."`
}

type createConnectionInput struct {
	requestLookup
	ChainID   string `json:"chainId" jsonschema:"Target EVM chain ID as a decimal or 0x hexadecimal string."`
	ChainName string `json:"chainName,omitempty" jsonschema:"Optional display name for the target network."`
	Intent    string `json:"intent,omitempty" jsonschema:"Optional explanation of why the connection is requested."`
}

type connectionResult struct {
	api.ConnectionResponse
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type walletTools struct {
	dataDir    string
	uiEmbedded bool
}

func (t walletTools) create(ctx context.Context, req *sdk.CallToolRequest, in createConnectionInput) (*sdk.CallToolResult, connectionResult, error) {
	if !t.uiEmbedded {
		return nil, connectionResult{}, errors.New("ui_unavailable: use a build with the embedded approval page")
	}
	name, err := callerName(req)
	if err != nil {
		return nil, connectionResult{}, err
	}
	c, err := client.New(t.dataDir)
	if err != nil {
		return nil, connectionResult{}, err
	}
	if err := c.EnsureConnectionApproval(ctx); err != nil {
		return nil, connectionResult{}, err
	}
	r, err := c.CreateConnection(ctx, api.ConnectionCreateRequest{
		ID: in.RequestID, ClientName: name, ChainID: in.ChainID, Intent: in.Intent, ChainName: in.ChainName,
	})
	return connectionReceipt(r, err)
}

func callerName(req *sdk.CallToolRequest) (string, error) {
	params := req.Session.InitializeParams()
	if params != nil && params.ClientInfo != nil {
		for _, value := range []string{params.ClientInfo.Title, params.ClientInfo.Name} {
			if name := strings.TrimSpace(value); name != "" {
				return name, nil
			}
		}
	}
	return "", errors.New("bad_request: supply a client title or name during MCP initialization")
}

func (t walletTools) get(ctx context.Context, _ *sdk.CallToolRequest, in requestLookup) (*sdk.CallToolResult, connectionResult, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, connectionResult{}, err
	}
	return connectionReceipt(c.GetConnection(ctx, in.RequestID))
}

func (t walletTools) cancel(ctx context.Context, _ *sdk.CallToolRequest, in requestLookup) (*sdk.CallToolResult, connectionResult, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, connectionResult{}, err
	}
	r, err := c.CancelConnection(ctx, in.RequestID)
	var failure *client.Error
	if errors.As(err, &failure) && failure.Code == "connection_conflict" {
		return connectionReceipt(c.GetConnection(ctx, in.RequestID))
	}
	return connectionReceipt(r, err)
}

func (t walletTools) readyClient(ctx context.Context) (*client.Client, error) {
	c, err := client.New(t.dataDir)
	if err != nil {
		return nil, err
	}
	_, err = c.Ensure(ctx)
	return c, err
}

func connectionReceipt(r api.ConnectionResponse, err error) (*sdk.CallToolResult, connectionResult, error) {
	if err != nil {
		return nil, connectionResult{}, err
	}
	url := api.Origin + "/?connection=" + r.ID
	text := fmt.Sprintf("Connection %s (%s), chain %s. [Open the connection request](%s)", r.ID, r.State, r.ChainID, url)
	if r.State == "pending" {
		text += "\nApproval expires at " + r.ExpiresAt
	}
	if r.SessionID != "" {
		text += "\nSession ID: " + r.SessionID
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: text}}}, connectionResult{r, "connection", url}, nil
}
