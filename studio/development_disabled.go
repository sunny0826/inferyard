//go:build !studio_dev || release

package main

import (
	"context"
	"flag"
)

func registerDevelopment(*flag.FlagSet, *startupOptions) {}
func runDevelopment(context.Context, context.CancelFunc, startupOptions) (bool, error) {
	return false, nil
}
