//go:build ui_debug && !release

package main

import (
	"context"
	"flag"

	"inferyard/studio/internal/pages"
)

func registerDebugUI(flags *flag.FlagSet, options *startupOptions) {
	flags.BoolVar(&options.debugUI, "debug-ui", false, "Open native UI previews with simulated data; no model, download, or settings operations")
}

func runDebugUI(ctx context.Context, options startupOptions) bool {
	if !options.debugUI {
		return false
	}
	runApp(ctx, func() { pages.NewPreview(ctx) })
	return true
}
