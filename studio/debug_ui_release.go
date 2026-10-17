//go:build !ui_debug || release

package main

import (
	"context"
	"flag"
)

func registerDebugUI(*flag.FlagSet, *startupOptions) {}

func runDebugUI(context.Context, startupOptions) bool { return false }
