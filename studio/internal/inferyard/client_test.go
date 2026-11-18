package inferyard

import (
	"context"
	"errors"
	"fmt"
	"os"

	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCancellation(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprint(ignore), func(t *testing.T) {
			ready := filepath.Join(t.TempDir(), "ready")
			if runtime.GOOS == "windows" {
				t.Skip("SIGINT cancellation requires Unix")
			}
			path, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			// Readiness follows signal registration, with no shell/child startup race.
			grace := time.Duration(0)
			if ignore {
				grace = 100 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type response struct {
				out Execution
				err error
			}
			done := make(chan response, 1)
			go func() {
				out, err := (Client{Path: path, GracePeriod: grace}).Exec(ctx, "-test.run=^TestCancellationProcess$", "--", ready, fmt.Sprint(ignore))
				done <- response{out, err}
			}()
			deadline := time.After(3 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				select {
				case <-deadline:
					t.Fatal("process did not become ready")
				case <-time.After(time.Millisecond):
				}
			}
			started := time.Now()
			cancel()
			select {
			case got := <-done:
				if !errors.Is(got.err, context.Canceled) || got.out.Status != Cancelled {
					t.Fatalf("%+v", got)
				}
				if !ignore && (got.out.ExitCode != 130 || !strings.Contains(string(got.out.Stdout), "interrupted")) {
					t.Fatalf("SIGINT handler did not run: %+v", got)
				}
				if ignore && (got.out.ExitCode != -1 || time.Since(started) < grace) {
					t.Fatalf("expected forced signal exit after grace period: %+v", got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation hung")
			}
		})
	}
}

// TestCancellationProcess runs only when explicitly selected as a subprocess.
func TestCancellationProcess(t *testing.T) {
	args := os.Args
	if len(args) < 4 || args[len(args)-3] != "--" {
		return
	}
	interrupts := make(chan os.Signal, 1)
	if args[len(args)-1] == "true" {
		signal.Ignore(os.Interrupt)
	} else {
		signal.Notify(interrupts, os.Interrupt)
	}
	if err := os.WriteFile(args[len(args)-2], []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupts:
	case <-time.After(time.Minute):
		os.Exit(1)
	}
	fmt.Println("interrupted")
	os.Exit(130)
}
