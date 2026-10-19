//go:build studio_dev && !release

package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDevelopmentNativeChild(t *testing.T) {
	mode := os.Getenv("INFERYARD_NATIVE_CHILD")
	if mode == "" {
		return
	}
	var unlock func()
	if mode != "process" {
		var err error
		unlock, err = lockEngineOwner(ownerPath(mode))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	fmt.Println("ready")
	io.Copy(io.Discard, os.Stdin)
	if unlock != nil {
		if err := os.WriteFile(ownerPath(mode), []byte("owner-written-before-unlock"), 0600); err != nil {
			os.Exit(3)
		}
		unlock()
	}
	os.Exit(0)
}

func developmentNativeChild(t *testing.T, mode string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestDevelopmentNativeChild$")
	cmd.Env = append(os.Environ(), "INFERYARD_NATIVE_CHILD="+mode)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal(line, err)
	}
	return cmd, input
}

func TestDevelopmentNativeProcessObservation(t *testing.T) {
	cmd, input := developmentNativeChild(t, "process")
	pid := int64(cmd.Process.Pid)
	if state, err := DevelopmentObserveProcess(pid); err != nil || state != DevelopmentProcessPresent {
		t.Fatal("live child", state, err)
	}
	input.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := DevelopmentObserveProcess(pid)
		if err != nil {
			if state != DevelopmentProcessUnknown {
				t.Fatal(state, err)
			}
			t.Logf("unreaped child is conservatively Unknown on this host: %v", err)
			break
		}
		if state == DevelopmentProcessAbsent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("exited child did not become absent before wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if state, err := DevelopmentObserveProcess(pid); err != nil || state != DevelopmentProcessAbsent {
		t.Fatal("waited child", state, err)
	}
	for _, pid := range []int64{0, -1, 1 << 32} {
		if _, err := DevelopmentObserveProcess(pid); err == nil {
			t.Fatal("invalid PID accepted", pid)
		}
	}
}

func TestDevelopmentOwnerLockNativeDeadlineAndRace(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd, input := developmentNativeChild(t, workspace)
	path := ownerPath(workspace) + ".lock"
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	unlock, err := DevelopmentLockEngineOwner(ctx, workspace)
	if unlock != nil {
		unlock()
		t.Fatal("bypassed the real owner lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("missing bounded cancellation", err)
	}
	input.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	unlock, err = DevelopmentLockEngineOwner(ctx2, workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if raw, err := os.ReadFile(ownerPath(workspace)); err != nil || string(raw) != "owner-written-before-unlock" {
		t.Fatal("owner writer was not observed", string(raw), err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("permanent owner lock inode changed", err)
	}
}

func TestDevelopmentOwnerLockRejectsInvalidWorkspace(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), filepath.Join(root, "file")} {
		if filepath.Base(path) == "file" {
			os.WriteFile(path, []byte("file"), 0600)
		}
		if unlock, err := DevelopmentLockEngineOwner(context.Background(), path); err == nil {
			unlock()
			t.Fatal("invalid workspace accepted")
		}
	}
}
