//go:build release

package main

import (
	"flag"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
)

type devStatusReporter struct{}

func (devStatusReporter) report(string, string) {}

func registerDevelopment(*flag.FlagSet, *startupOptions) {}

func developmentBindings(startupOptions) (*core.BoundPaths, mygo.WindowStateStore, devStatusReporter, error) {
	return nil, nil, devStatusReporter{}, nil
}
