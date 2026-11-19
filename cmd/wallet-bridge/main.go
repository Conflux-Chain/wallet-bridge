package main

import (
	"io/fs"
	"os"

	"github.com/Conflux-Chain/wallet-bridge/internal/buildinfo"
	"github.com/Conflux-Chain/wallet-bridge/internal/cli"
	"github.com/Conflux-Chain/wallet-bridge/web"
)

func main() {
	info := buildinfo.Current()
	entry, err := fs.Stat(web.Assets(), "index.html")
	info.UIEmbedded = err == nil && !entry.IsDir()
	os.Exit(cli.Run(os.Args[1:], info, os.Stdout, os.Stderr))
}
