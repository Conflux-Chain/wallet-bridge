package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/cli"
	"github.com/Conflux-Chain/wallet-bridge/internal/client"
	"github.com/Conflux-Chain/wallet-bridge/internal/mcp"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/service"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
	"github.com/Conflux-Chain/wallet-bridge/web"
)

func main() {
	info := buildinfo.Current()
	entry, err := fs.Stat(web.Assets(), "index.html")
	info.UIEmbedded = err == nil && !entry.IsDir()
	os.Exit(cli.Run(os.Args[1:], cli.Dependencies{
		BuildInfo:   info,
		CheckUpdate: update.Check,
		StageUpdate: update.Stage,
		NewClient:   client.New,
		OpenBrowser: platform.OpenBrowser,
		RunMCP: func(dir string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return mcp.Run(ctx, info, dir)
		},
		BrowserLink: func(dir string) (api.BrowserTicketResponse, error) {
			c, err := client.New(dir)
			if err != nil {
				return api.BrowserTicketResponse{}, err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return c.BrowserLink(ctx)
		},
		ListBrowsers: func(dir string, limit int, after string) (api.BrowserGrantListResponse, error) {
			c, err := client.New(dir)
			if err != nil {
				return api.BrowserGrantListResponse{}, err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return c.ListBrowsers(ctx, limit, after)
		},
		RevokeBrowser: func(dir, id string, all bool) (api.BrowserRevokeResponse, error) {
			c, err := client.New(dir)
			if err != nil {
				return api.BrowserRevokeResponse{}, err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return c.RevokeBrowser(ctx, id, all)
		},
		ManageService: func(action, dir string) (client.Status, error) {
			c, err := client.New(dir)
			if err != nil {
				return client.Status{}, err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			switch action {
			case "ensure":
				return c.Ensure(ctx)
			case "stop":
				return c.Stop(ctx)
			default:
				return c.Status(ctx)
			}
		},
		RunService: func(dir string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return service.Run(ctx, service.Config{
				DataDir: dir, Version: info.Version, Assets: web.Assets(),
				Ready: func() error {
					_, err := fmt.Fprintf(os.Stdout, "wallet-bridge ready at %s\n", api.Origin)
					return err
				},
			})
		},
	}, os.Stdout, os.Stderr))
}
