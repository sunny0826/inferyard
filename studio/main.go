package main

import (
	"context"
	_ "embed"
	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/desktop"
	"inferyard/studio/internal/pages"
	"inferyard/studio/internal/wizard"
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
	guide := wizard.New(ctx, service)
	mygo.App.WhenReady(func() {
		desktop.Subscribe(ctx, service.Bus, false, func(message desktop.Message) error {
			return mygo.NewNotification(mygo.NotificationOptions{Title: message.Title, Body: message.Body}).Show()
		}, log.Printf)
		var mainWindow *pages.Window
		reports := pages.NewReports(service)
		createMain := func(page string) {
			if mainWindow != nil && !mainWindow.Win.IsDestroyed() {
				mainWindow.Navigate(page)
				return
			}
			mainWindow = pages.New(ctx, service, page, guide.Open, reports)
			guide.SetMain(mainWindow.Win)
		}
		guide.SetMainFactory(createMain)
		if service.NeedsOnboarding() {
			guide.Open()
			return
		}
		createMain("")
		guide.OpenIfNeeded()
	})
	go func() { <-ctx.Done(); mygo.App.Quit() }()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
