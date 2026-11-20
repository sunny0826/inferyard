//go:build release

package main

import (
	"flag"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
)

func registerDevelopment(*flag.FlagSet, *startupOptions) {}

func developmentBindings(startupOptions) (*core.BoundPaths, mygo.WindowStateStore, error) {
	return nil, nil, nil
}
