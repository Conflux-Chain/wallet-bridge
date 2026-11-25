package mcp

import (
	"context"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func Run(ctx context.Context, info buildinfo.Info, dataDir string) error {
	server := sdk.NewServer(&sdk.Implementation{Name: "wallet-bridge", Version: info.Version}, &sdk.ServerOptions{
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	tools := walletTools{dataDir: dataDir, uiEmbedded: info.UIEmbedded}
	sdk.AddTool(server, &sdk.Tool{
		Name:        "create_connection",
		Description: "Request a wallet connection on the specified EVM network. Generate a fresh random requestId before calling. Keep it and all input unchanged for retries. Show the returned approval link to the user immediately, then poll get_connection while the task is active.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, tools.create)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "get_connection",
		Description: "Look up the original connection by requestId, including after an interruption. While pending, leave time between checks. Stop polling on succeeded, rejected, canceled or expired. On success, call get_session with the returned sessionId to show the fixed wallet, account and network.",
	}, tools.get)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "cancel_connection",
		Description: "Cancel a pending connection when the user wants to withdraw it. Returns the effective state if approval, refusal or expiry already took effect.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true},
	}, tools.cancel)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "get_session",
		Description: "Look up a Session by sessionId and show its fixed wallet, account, chain and active or closed state. Access renews an active Session's five-minute idle deadline.",
	}, tools.getSession)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "close_session",
		Description: "Close this Session and cancel its pending requests that have not reached the wallet. Returns the closed Session, including on repeat calls. A transaction already handed to the wallet may still return a hash.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true},
	}, tools.closeSession)
	// Advertise the supported fields while preserving which fields were supplied to HTTP.
	input, err := jsonschema.For[transactionInput[api.TransactionPayload]](nil)
	if err != nil {
		return err
	}
	sdk.AddTool(server, &sdk.Tool{
		Name: "create_transaction", InputSchema: input,
		Description: "Create one transaction for browser review. Generate a fresh random requestId per intended transaction. Retry with the original ID and inputs. Provide the fixed Session sender and chain. Show the review link immediately, then use get_transaction to follow the original request. The user confirms sending in the browser wallet.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(false)},
	}, tools.createTransaction)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "get_transaction",
		Description: "Look up the original transaction by requestId. While pending or awaiting_wallet, leave time between checks. Stop automatic polling on all other states. If uncertain, tell the user it may have been broadcast and keep the ID for a later lookup. Never automatically resubmit.",
	}, tools.getTransaction)
	sdk.AddTool(server, &sdk.Tool{
		Name:        "cancel_transaction",
		Description: "Cancel a transaction still waiting for approval when the user withdraws it. Report the returned state. After handoff the wallet can still broadcast, and an earlier outcome is preserved.",
		Annotations: &sdk.ToolAnnotations{IdempotentHint: true},
	}, tools.cancelTransaction)
	return server.Run(ctx, &sdk.StdioTransport{})
}
