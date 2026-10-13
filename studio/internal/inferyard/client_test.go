package inferyard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func script(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	path := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestExitCodes(t *testing.T) {
	for code, want := range map[int]ExitStatus{0: Complete, 2: Blocked, 3: Incomplete, 4: Error, 130: Cancelled, 7: Unknown} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := Client{Path: script(t, fmt.Sprintf("printf 'captured'; printf 'diagnostic' >&2; exit %d", code))}
			out, err := c.Exec(context.Background())
			if err != nil || out.ExitCode != code || out.Status != want || string(out.Stdout) != "captured" || string(out.Stderr) != "diagnostic" {
				t.Fatalf("%+v %v", out, err)
			}
		})
	}
}
func TestLocate(t *testing.T) {
	path := script(t, "printf 'inferyard 1.2.3\\n'")
	for _, override := range []string{path, ""} {
		t.Setenv("PATH", filepath.Dir(path))
		b, err := Locate(context.Background(), override)
		if err != nil || b.Path != path || b.Version != "1.2.3" {
			t.Fatalf("%+v %v", b, err)
		}
	}
	if _, err := Locate(context.Background(), path+"-missing"); err == nil {
		t.Fatal("missing executable accepted")
	}
	if _, err := ParseVersion([]byte("unexpected output")); err == nil {
		t.Fatal("invalid version accepted")
	}
}
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

func TestExecArgumentsAndFailures(t *testing.T) {
	out, err := (Client{Path: script(t, "printf '%s' \"$1\"")}).Exec(context.Background(), "a b; $(false)")
	if err != nil || string(out.Stdout) != "a b; $(false)" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := (Client{Path: filepath.Join(t.TempDir(), "missing")}).Exec(context.Background()); err == nil {
		t.Fatal("missing executable accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = (Client{}).Exec(ctx)
	if !errors.Is(err, context.Canceled) || out.Status != Cancelled {
		t.Fatalf("%+v %v", out, err)
	}
}
func TestRealCLI(t *testing.T) {
	root := os.Getenv("INFERYARD_SMOKE_ROOT")
	if root == "" {
		t.Skip("set INFERYARD_SMOKE_ROOT for the two authorized read-only commands")
	}
	for _, args := range [][]string{{"--version"}, {"device-check", "--json"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		cmd := exec.CommandContext(ctx, "mise", append([]string{"exec", "--", "uv", "run", "--frozen", "inferyard"}, args...)...)
		cmd.Dir = root
		raw, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatalf("CLI %v failed: %v", args, err)
		}
		if args[0] == "--version" {
			v, err := ParseVersion(raw)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("version parsed: %s", v)
		} else {
			result, err := Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			device, err := ParseDeviceCheck(result)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("device parsed: status=%s kind=%s", result.Status, device.Kind)
		}
	}
}
