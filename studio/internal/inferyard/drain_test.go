package inferyard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSpeedDrainProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "speed-drain" {
		return
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("ready"), 0600); err != nil {
		os.Exit(4)
	}
	<-interrupts
	time.Sleep(3 * time.Second)
	fmt.Println("sealed-after-drain")
	os.Exit(130)
}

func TestContextSpeedDrainGrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal helper")
	}
	path, _ := os.Executable()
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(WithDrainGrace(t.Context(), 10*time.Second))
	defer cancel()
	done := make(chan struct{})
	var out Execution
	var err error
	go func() {
		out, err = (Client{Path: path}).Exec(ctx, "-test.run=^TestSpeedDrainProcess$", "speed-drain", ready)
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(ready); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper not ready")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		t.Fatal("unbounded drain")
	}
	if !errors.Is(err, context.Canceled) || out.ExitCode != 130 || string(out.Stdout) != "sealed-after-drain\n" {
		t.Fatalf("premature kill or lost result: %+v %v", out, err)
	}
}
