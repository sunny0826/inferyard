package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/desktop"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/pages"
)

//go:embed mygo.config.ts
var desktopConfig string

//go:embed resources/icon.png
var appIconPNG []byte

func main() {
	os.Exit(runMain())
}

func runMain() (exitCode int) {
	options, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		log.Print(err)
		return 2
	}
	version := regexp.MustCompile(`(?m)^\s*version:\s*['"]([^'"]+)['"],?\s*$`).FindAllStringSubmatch(desktopConfig, -1)
	if len(version) != 1 {
		log.Print("desktop configuration must contain one version")
		return 1
	}
	core.Version = version[0][1]
	mygo.App.SetLanguages(l10n.SupportedLanguages()...)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if runDebugUI(ctx, options) {
		return 0
	}
	boundPaths, windowStates, status, err := developmentBindings(options)
	if err != nil {
		log.Print(err)
		return 2
	}
	service, err := core.New(ctx, core.Options{Paths: boundPaths})
	if err != nil {
		log.Print(err)
		return 1
	}
	service.StartAutoCLIUpgrade()
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := service.Shutdown(shutdownCtx); err != nil {
			log.Printf("Studio shutdown: %v", err)
			status.report("shutdown_failed", err.Error())
			exitCode = 1
			return
		}
		status.report("stopped", "")
	}()
	if windowStates != nil {
		mygo.App.SetWindowStateStore(windowStates)
	}
	runApp(ctx, func() {
		var tray *desktop.TrayController
		suppress := pages.New(ctx, service, "", pages.WindowHooks{
			BindTray: func(win *mygo.Window) func(bool) error {
				tray = desktop.NewTrayController(desktop.TrayConfig{
					Window:  win,
					IconPNG: appIconPNG,
					ToolTip: win.Title(),
					Logf:    log.Printf,
				})
				return tray.SetEnabled
			},
			OnLocaleChanged: func(string) {
				if tray != nil {
					tray.RefreshLocale()
				}
			},
		})
		service.SetUpdateQuitHook(func() { mygo.App.Quit() })
		service.StartBackgroundUpdateCheck()
		desktop.SubscribeWithFilter(ctx, service.Bus, suppress, func(message desktop.Message) error {
			return mygo.NewNotification(mygo.NotificationOptions{Title: message.Title, Body: message.Body}).Show()
		}, log.Printf)
		status.report("ready", "")
	})
	return exitCode
}

func runApp(ctx context.Context, ready func()) {
	mygo.App.WhenReady(func() {
		if err := mygo.App.Dock.SetIcon(appIconPNG); err != nil {
			log.Printf("Studio dock icon: %v", err)
		}
		ready()
	})
	go func() { <-ctx.Done(); mygo.App.Quit() }()
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
	}
}
