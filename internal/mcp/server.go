package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context, version string) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "wallet-bridge", Version: version}, &sdk.ServerOptions{
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	return server.Run(ctx, &sdk.StdioTransport{})
}
