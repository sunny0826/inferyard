package main

import (
	"flag"
	"fmt"
	"io"
)

type startupOptions struct {
	debugUI                        bool
	devManifest, devCheck, devStop string
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
	emptyDevelopment := false
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "dev-manifest" || f.Name == "dev-check" || f.Name == "dev-stop") && f.Value.String() == "" {
			emptyDevelopment = true
		}
	})
	if emptyDevelopment {
		return options, fmt.Errorf("development manifest path is required")
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	count := 0
	for _, path := range []string{options.devManifest, options.devCheck, options.devStop} {
		if path != "" {
			count++
		}
	}
	if count > 1 || (count > 0 && options.debugUI) {
		return options, fmt.Errorf("development modes and UI preview are mutually exclusive")
	}
	return options, nil
}
