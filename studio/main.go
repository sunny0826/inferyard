package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/desktop"
	"inferyard/studio/internal/pages"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)

//go:embed mygo.config.ts
var desktopConfig string

func main() {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Print(err)
		os.Exit(2)
	}
	version := regexp.MustCompile(`(?m)^\s*version:\s*['"]([^'"]+)['"],?\s*$`).FindAllStringSubmatch(desktopConfig, -1)
	if len(version) != 1 {
		log.Fatal("desktop configuration must contain one version")
	}
	core.Version = version[0][1]
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if runDebugUI(ctx, options) {
		return
	}
	service, err := core.New(ctx, core.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := service.Shutdown(shutdownCtx); err != nil {
			log.Printf("Studio shutdown: %v", err)
		}
	}()
	runApp(ctx, func() {
		desktop.Subscribe(ctx, service.Bus, func(message desktop.Message) error {
			return mygo.NewNotification(mygo.NotificationOptions{Title: message.Title, Body: message.Body}).Show()
		}, log.Printf)
		pages.New(ctx, service, "")
	})
}

func runApp(ctx context.Context, ready func()) {
	mygo.App.WhenReady(ready)
	go func() { <-ctx.Done(); mygo.App.Quit() }()
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
	}
}
