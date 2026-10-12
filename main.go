package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"inferyard-studio/internal/server"
	"inferyard-studio/web"
)

func main() {
	sessionFD := flag.Int("session-fd", -1, "write the session URL once to an inherited private pipe (never to logs)")
	headless := flag.Bool("headless", false, "serve the embedded frontend without a desktop window")
	evidence := flag.String("spike-evidence", "", "write M0 desktop screenshot to this directory")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	assets, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		log.Fatal(err)
	}
	app, err := server.New(ctx, assets)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := server.Listen("127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	defer httpServer.Close()
	url := "http://" + listener.Addr().String() + "/?token=" + app.Token()
	log.Printf("Studio listening on %s", listener.Addr())
	if *sessionFD >= 3 {
		pipe := os.NewFile(uintptr(*sessionFD), "session-pipe")
		if pipe == nil {
			log.Fatal("invalid session pipe")
		}
		_, err := fmt.Fprintln(pipe, url)
		pipe.Close()
		if err != nil {
			log.Fatal("session pipe write failed")
		}
	}
	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server: %v", err)
			cancel()
		}
	}()
	if *headless {
		<-ctx.Done()
		return
	}
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard Studio", Width: 960, Height: 640})
		win.Page().OnDidFinishLoad(func() {
			log.Print("desktop page loaded from local HTTP")
			if *evidence != "" {
				go recordDesktop(win, *evidence)
			}
		})
		win.Page().OnDidFailLoad(func(err *mygo.LoadError) { log.Print("desktop load failed") })
		if err := win.Page().LoadURL(url); err != nil {
			log.Print("desktop navigation failed")
			mygo.App.Quit()
		}
	})
	go func() { <-ctx.Done(); mygo.App.Quit() }()
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func recordDesktop(win *mygo.Window, dir string) {
	for sample := 1; sample <= 2; sample++ {
		time.Sleep(3 * time.Second)
		value, err := win.Page().Eval(`document.title`)
		log.Printf("desktop DOM sample=%d title=%v error=%v", sample, value, err)
	}
	png, err := win.CapturePage()
	if err == nil {
		err = os.MkdirAll(dir, 0755)
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "desktop.png"), png, 0644)
	}
	log.Printf("desktop screenshot error=%v", err)
}
