//go:build studio_dev && !release

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sessionFixture(t *testing.T, m *devManifest) devSession {
	t.Helper()
	s := devSession{Kind: "studio_dev_session.v1", SessionID: m.sessionID(), PID: 42, StartTicks: 123, State: "ready", CLIPath: m.cliPath()}
	if err := atomicDevJSON(m.sessionPath(), s); err != nil {
		t.Fatal(err)
	}
	return s
}
func matchingProcess(m *devManifest) devProcessReader {
	return devProcessReader{func(int64) (int64, error) { return 123, nil }, func(int64) (string, error) { return m.studioPath(), nil }}
}

func TestDevelopmentStopIdentityAndOwner(t *testing.T) {
	for _, scenario := range []string{"pid-reused", "wrong-executable", "gone", "gone-with-owner", "shutdown-failed", "shutdown-failed-gone", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			m := devFixture(t)
			s := sessionFixture(t, m)
			reader := matchingProcess(m)
			switch scenario {
			case "pid-reused":
				reader.ticks = func(int64) (int64, error) { return 999, nil }
			case "wrong-executable":
				reader.executable = func(int64) (string, error) { return m.cliPath(), nil }
			case "gone", "gone-with-owner":
				reader.ticks = func(int64) (int64, error) { return 0, os.ErrNotExist }
			case "shutdown-failed", "shutdown-failed-gone":
				s.State = "shutdown_failed"
				if err := atomicDevJSON(m.sessionPath(), s); err != nil {
					t.Fatal(err)
				}
				if scenario == "shutdown-failed-gone" {
					reader.ticks = func(int64) (int64, error) { return 0, os.ErrNotExist }
					if err := m.ownerAbsent(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if scenario == "gone-with-owner" {
				path := filepath.Join(m.ProfileRoot, "workspace", "logs")
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
				writeDevFile(t, filepath.Join(path, "engine-owner.json"), "keep")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			err := m.stopSession(ctx, reader)
			if (err == nil) != (scenario == "gone") {
				t.Fatal(scenario, err)
			}
			if scenario != "timeout" {
				if _, err := os.Stat(m.stopPath()); !os.IsNotExist(err) {
					t.Fatal("unexpected stop action", err)
				}
			}
			current, err := m.readSession()
			if err != nil || current.State != s.State {
				t.Fatal("session evidence changed", current, err)
			}
		})
	}
}

func TestDevelopmentStopCompletesAndPreservesEvidence(t *testing.T) {
	m := devFixture(t)
	s := sessionFixture(t, m)
	done := make(chan error, 1)
	go func() { done <- m.stopSession(context.Background(), matchingProcess(m)) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var request devStopRequest
		if readDevJSON(m.stopPath(), &request) == nil {
			if request.SessionID != m.sessionID() {
				t.Fatal(request)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no stop request")
		}
		time.Sleep(time.Millisecond)
	}
	s.State = "stopped"
	if err := atomicDevJSON(m.sessionPath(), s); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not finish")
	}
	current, err := m.readSession()
	if err != nil || current.State != "stopped" {
		t.Fatal(current, err)
	}
}

func TestDevelopmentStopWatcherMatchesSession(t *testing.T) {
	m := devFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.watchStop(ctx, cancel)
	if err := atomicDevJSON(m.stopPath(), devStopRequest{"different"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("unrelated request stopped session")
	case <-time.After(250 * time.Millisecond):
	}
	if err := atomicDevJSON(m.stopPath(), devStopRequest{m.sessionID()}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("matching request ignored")
	}
}
