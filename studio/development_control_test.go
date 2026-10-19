//go:build studio_dev && !release

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestDevelopmentControlProcess(t *testing.T) {
	path := os.Getenv("INFERYARD_DEV_CONTROL_TEST")
	if path == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := runDevelopment(ctx, cancel, startupOptions{devStop: path}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestDevelopmentStopWithMissingCLI(t *testing.T) {
	m := devFixture(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.studioPath(), program, 0700); err != nil {
		t.Fatal(err)
	}
	m.StudioSHA256, err = fileDigest(m.studioPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicDevJSON(m.path, m); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(m.cliPath()); err != nil {
		t.Fatal(err)
	}
	session := devSession{Kind: "studio_dev_session.v1", SessionID: m.sessionID(), PID: 2147483647, StartTicks: 1, State: "ready", CLIPath: m.cliPath()}
	if err := atomicDevJSON(m.sessionPath(), session); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(m.studioPath(), "-test.run=^TestDevelopmentControlProcess$")
	command.Env = append(os.Environ(), "INFERYARD_DEV_CONTROL_TEST="+m.path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("a missing CLI prevents clean dead-session recovery: %v\n%s", err, output)
	}
	if _, err := os.Stat(m.sessionPath()); err != nil {
		t.Fatal("session evidence was not retained", err)
	}
}
