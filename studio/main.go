package main

import (
	"context"
	_ "embed"
	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/desktop"
	"inferyard/studio/internal/pages"
	"log"
	"os"
	"os/signal"
	"regexp"
)

//go:embed mygo.config.ts
var desktopConfig string

func main() {
	version := regexp.MustCompile(`(?m)^\s*version:\s*['"]([^'"]+)['"],?\s*$`).FindAllStringSubmatch(desktopConfig, -1)
	if len(version) != 1 {
		log.Fatal("desktop configuration must contain one version")
	}
	core.Version = version[0][1]
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	service, err := core.New(ctx, core.Options{})
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.WhenReady(func() {
		desktop.Subscribe(ctx, service.Bus, false, func(message desktop.Message) error {
			return mygo.NewNotification(mygo.NotificationOptions{Title: message.Title, Body: message.Body}).Show()
		}, log.Printf)
		reports := pages.NewReports(service)
		pages.New(ctx, service, "", reports)
	})
	go func() { <-ctx.Done(); mygo.App.Quit() }()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
