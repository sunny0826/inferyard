package main

import (
	"flag"
	"fmt"
	"io"
)

type startupOptions struct {
	debugUI bool
}

func parseOptions(args []string, output io.Writer) (startupOptions, error) {
	var options startupOptions
	flags := flag.NewFlagSet("inferyard-studio", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.BoolVar(&options.debugUI, "debug-ui", false, "Open native UI previews with simulated data; no model, download, or settings operations")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	return options, nil
}
