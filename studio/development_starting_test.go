//go:build studio_dev && !release

package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestDevelopmentStartingCanStopBeforeWindowReady(t *testing.T) {
	m := devFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan devSession, 1)
	cleanup := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- m.runSession(ctx, cancel, func(*devSession) error {
			s, err := m.readSession()
			if err != nil {
				return err
			}
			entered <- s
			<-ctx.Done()
			<-cleanup
			return ctx.Err()
		})
	}()
	var session devSession
	select {
	case session = <-entered:
	case <-time.After(time.Second):
		t.Fatal("starting was not published before startup")
	}
	if session.State != "starting" || session.PID != int64(os.Getpid()) || session.CLIPath != m.cliPath() {
		t.Fatal(session)
	}
	reader := matchingProcess(m)
	reader.ticks = func(int64) (int64, error) { return session.StartTicks, nil }
	stopDone := make(chan error, 1)
	go func() { stopDone <- m.stopSession(context.Background(), reader) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("starting session ignored stop")
	}
	current, err := m.readSession()
	if err != nil || current.State != "starting" {
		t.Fatal("reported stopped before cleanup", current, err)
	}
	close(cleanup)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup did not finish")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
	current, err = m.readSession()
	if err != nil || current.State != "stopped" {
		t.Fatal(current, err)
	}
}

func TestDevelopmentStartingHonorsExistingStop(t *testing.T) {
	m := devFixture(t)
	if err := m.ensureStopRequest(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	err := m.runSession(ctx, cancel, func(*devSession) error { called = true; return nil })
	if called || !errors.Is(err, context.Canceled) {
		t.Fatal(called, err)
	}
	s, err := m.readSession()
	if err != nil || s.State != "stopped" {
		t.Fatal(s, err)
	}
}

func TestDevelopmentStartingFailureKeepsShutdownOutcome(t *testing.T) {
	for _, failed := range []bool{false, true} {
		m := devFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		failure := errors.New("startup fixture failed")
		err := m.runSession(ctx, cancel, func(s *devSession) error {
			if failed {
				s.State = "shutdown_failed"
			}
			return failure
		})
		cancel()
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		s, err := m.readSession()
		want := "stopped"
		if failed {
			want = "shutdown_failed"
		}
		if err != nil || s.State != want {
			t.Fatal(s, err)
		}
	}
}
