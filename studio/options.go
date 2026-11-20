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
	devStatus  string
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
	devVisited := false
	emptyDevStatus := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "dev-profile", "dev-assets", "dev-cli", "dev-label", "dev-status":
			devVisited = true
			if f.Name == "dev-status" && f.Value.String() == "" {
				emptyDevStatus = true
			}
		}
	})
	if devVisited && options.debugUI {
		return options, fmt.Errorf("development flags and --debug-ui are mutually exclusive")
	}
	if devVisited && (options.devProfile == "" || options.devCLI == "") {
		return options, fmt.Errorf("--dev-profile and --dev-cli are both required for development mode")
	}
	if emptyDevStatus {
		return options, fmt.Errorf("--dev-status path must not be empty")
	}
	return options, nil
}
