package main

import (
	"flag"
	"fmt"
	"io"
)

type startupOptions struct {
	debugUI    bool
	devProfile string
	devAssets  string
	devCLI     string
	devLabel   string
}

func parseOptions(args []string, output io.Writer) (startupOptions, error) {
	var options startupOptions
	flags := flag.NewFlagSet("inferyard-studio", flag.ContinueOnError)
	flags.SetOutput(output)
	registerDebugUI(flags, &options)
	registerDevelopment(flags, &options)
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	hasDev := options.devProfile != "" || options.devAssets != "" || options.devCLI != "" || options.devLabel != ""
	if hasDev && options.debugUI {
		return options, fmt.Errorf("development flags and --debug-ui are mutually exclusive")
	}
	if hasDev && (options.devProfile == "" || options.devCLI == "") {
		return options, fmt.Errorf("--dev-profile and --dev-cli are both required for development mode")
	}
	return options, nil
}
