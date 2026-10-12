package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	defer httpServer.Close()
	url := "http://" + listener.Addr().String() + "/?token=" + app.Token()
	log.Printf("STUDIO_URL=%s", url)
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
		win := mygo.NewWindow(mygo.WindowOptions{Title: "InferYard Studio · M0", Width: 960, Height: 640})
		win.Page().OnDidFinishLoad(func() {
			log.Print("desktop page loaded from local HTTP")
			if *evidence != "" {
				go recordDesktop(win, *evidence)
			}
		})
		win.Page().OnDidFailLoad(func(err *mygo.LoadError) { log.Printf("desktop load failed: %v", err) })
		if err := win.Page().LoadURL(url); err != nil {
			log.Printf("desktop navigation: %v", err)
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
		value, err := win.Page().Eval(`document.querySelector('#count').textContent`)
		log.Printf("desktop DOM sample=%d count=%v error=%v", sample, value, err)
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
