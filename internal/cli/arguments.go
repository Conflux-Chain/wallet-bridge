package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

func parseFlags(flags *flag.FlagSet, args []string, help string, stdout, stderr io.Writer, required ...requiredArgument) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeCommandHelp(stdout, help, flags, required...)
			return 0, true
		}
		return argumentError(stderr, diagnosticJSON(flags), flagErrorMessage(flags, err)), true
	}
	if dir := flags.Lookup("data-dir"); dir != nil {
		if path := dir.Value.String(); path != "" && !filepath.IsAbs(path) {
			return argumentError(stderr, diagnosticJSON(flags), "--data-dir must be an absolute path. Use the full directory path or omit --data-dir to use the default."), true
		}
	}
	if name := flags.Lookup("client-name"); name != nil && strings.TrimSpace(name.Value.String()) == "" {
		return argumentError(stderr, diagnosticJSON(flags), "--client-name is blank. Supply a display name or omit --client-name to use Wallet Bridge CLI."), true
	}
	return 0, false
}

// The flag package includes the rejected value in parse errors. Show only option names and instructions.
func flagErrorMessage(flags *flag.FlagSet, err error) string {
	message := err.Error()
	help := "Run wallet-bridge " + flags.Name() + " --help for supported options."
	if name, ok := strings.CutPrefix(message, "flag needs an argument: -"); ok {
		if option := flags.Lookup(name); option != nil {
			_, hint := flag.UnquoteUsage(option)
			return "--" + name + " requires a value. " + hint
		}
	}
	if name, ok := strings.CutPrefix(message, "flag provided but not defined: -"); ok {
		return fmt.Sprintf("Unknown option %q. %s", "--"+name, help)
	}
	// Standard flag errors end in "for flag -name: reason" or "for -name: reason".
	if i := strings.LastIndex(message, " for "); i >= 0 {
		field, _, _ := strings.Cut(message[i+5:], ":")
		name := strings.TrimPrefix(strings.TrimPrefix(field, "flag "), "-")
		if option := flags.Lookup(name); option != nil {
			if _, ok := option.Value.(interface{ IsBoolFlag() bool }); ok {
				return "--" + name + " requires true or false. Use --" + name + " or --" + name + "=false."
			}
			_, hint := flag.UnquoteUsage(option)
			return "Invalid value for --" + name + ". " + hint
		}
	}
	return "Invalid option syntax. " + help
}

func unexpectedArguments(flags *flag.FlagSet, stderr io.Writer) int {
	return argumentError(stderr, diagnosticJSON(flags), flags.Name()+" does not accept positional arguments. Run wallet-bridge "+flags.Name()+" --help for supported options.")
}

func unknownCommand(stderr io.Writer, group string, args []string) int {
	command := strings.TrimSpace("wallet-bridge " + group)
	return argumentError(stderr, jsonInArgs(args, false), "Unknown command. Run "+command+" --help for available commands.")
}

func diagnosticJSON(flags *flag.FlagSet) bool {
	option := flags.Lookup("json")
	return option != nil && jsonInArgs(flags.Args(), option.Value.String() == "true")
}

// Parsing can stop at an error before the user's output option.
func jsonInArgs(args []string, enabled bool) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch name, value, assigned := strings.Cut(arg, "="); name {
		case "--json", "-json":
			if !assigned {
				enabled = true
			} else if parsed, err := strconv.ParseBool(value); err == nil {
				enabled = parsed
			}
		}
	}
	return enabled
}
