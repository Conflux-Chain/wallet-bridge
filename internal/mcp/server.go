package mcp

import (
	"context"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context, info buildinfo.Info, dataDir string) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "wallet-bridge", Version: info.Version}, &sdk.ServerOptions{
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	tools := connectionTools{dataDir: dataDir, uiEmbedded: info.UIEmbedded}
	sdk.AddTool(server, &sdk.Tool{
		Name:        "create_connection",
		Description: "Request a wallet connection on the specified EVM network. Generate a fresh random requestId before calling. Keep it and all input unchanged for retries. Show the returned approval link to the user immediately, then poll get_connection while the task is active.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, tools.create)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "get_connection",
		Description: "Look up the original connection by requestId, including after an interruption. While pending, leave time between checks. Stop polling on succeeded, rejected, canceled or expired.",
	}, tools.get)
	return server.Run(ctx, &sdk.StdioTransport{})
}
