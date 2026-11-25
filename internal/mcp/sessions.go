package mcp

import (
	"context"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sessionLookup struct {
	SessionID string `json:"sessionId" jsonschema:"Session ID from a successful connection, as 32 lowercase hexadecimal characters."`
}

func (t walletTools) getSession(ctx context.Context, _ *sdk.CallToolRequest, in sessionLookup) (*sdk.CallToolResult, api.SessionResponse, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, api.SessionResponse{}, err
	}
	session, err := c.GetSession(ctx, in.SessionID)
	return nil, session, err
}

func (t walletTools) closeSession(ctx context.Context, _ *sdk.CallToolRequest, in sessionLookup) (*sdk.CallToolResult, api.SessionResponse, error) {
	c, err := t.readyClient(ctx)
	if err != nil {
		return nil, api.SessionResponse{}, err
	}
	session, err := c.CloseSession(ctx, in.SessionID)
	return nil, session, err
}
