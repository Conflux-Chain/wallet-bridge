package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Conflux-Chain/wallet-bridge/internal/client"
)

const installUsage = "Usage: wallet-bridge install [--data-dir <absolute path>] [--json]"

func runInstall(args []string, deps Dependencies, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("data-dir", "", dataDirHelp)
	asJSON := flags.Bool("json", false, "Write the installed path as JSON. By default, print the path as text.")
	if code, done := parseFlags(flags, args, installUsage, stdout, stderr); done {
		return code
	}
	if flags.NArg() != 0 || (*dir != "" && !filepath.IsAbs(*dir)) {
		fmt.Fprintln(stderr, installUsage)
		return 2
	}
	if deps.Install == nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "configuration_error", Message: "Installer is unavailable"}, "")
	}
	path, err := deps.Install(*dir)
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "install_failed", Message: err.Error()}, "")
	}
	if *asJSON {
		err = json.NewEncoder(stdout).Encode(map[string]string{"path": path})
	} else {
		_, err = fmt.Fprintln(stdout, path)
	}
	if err != nil {
		return commandError(stderr, *asJSON, &client.Error{Code: "output_error", Message: "Cannot write installed path"}, "")
	}
	return 0
}
