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
