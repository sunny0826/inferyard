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
	registerDebugUI(flags, &options)
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	return options, nil
}
