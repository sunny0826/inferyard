package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"
)

type ownerFixture struct {
	mu       sync.Mutex
	ticks    int64
	failure  error
	signals  []os.Signal
	onSignal func(os.Signal)
}

func (f *ownerFixture) read(int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure != nil {
		return 0, f.failure
	}
	if f.ticks == 0 {
		return 0, os.ErrNotExist
	}
	return f.ticks, nil
}
func (f *ownerFixture) signal(_ int64, sig os.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, sig)
	if f.onSignal != nil {
		f.onSignal(sig)
	}
	return nil
}
func (f *ownerFixture) set(ticks int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ticks, f.failure = ticks, err
}
func (f *ownerFixture) sent() []os.Signal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]os.Signal(nil), f.signals...)
}

func ownerService(t *testing.T, f *ownerFixture) (*Service, string) {
	t.Helper()
	workspace := t.TempDir()
	path := ownerPath(workspace)
	record := engineOwner{PID: 4242, StartTicks: 123, Endpoint: "http://127.0.0.1:8080", LogFile: "engine.log", Candidate: "candidate-1"}
	if err := writeEngineOwner(path, record); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(UserSettings{WorkspaceRoot: &workspace})
	if err := os.WriteFile(settings, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(context.Background(), Options{
		SettingsPath: settings, ProcessStartTicks: f.read, SignalProcess: f.signal, OrphanStopTimeout: time.Millisecond,
		Execute: func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
			if v, ok := version(args); ok {
				return v, nil
			}
			return inferyard.Execution{}, errors.New("unexpected CLI request")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	return s, path
}
func assertOwnerGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("owner record remains", err)
	}
}

func TestOwnerMismatchAndDeadNeverSignal(t *testing.T) {
	for _, ticks := range []int64{0, 456} {
		t.Run(string(rune(ticks+65)), func(t *testing.T) {
			f := &ownerFixture{ticks: ticks}
			s, path := ownerService(t, f)
			if s.Engine().State != "stopped" {
				t.Fatal(s.Engine())
			}
			assertOwnerGone(t, path)
			if len(f.sent()) != 0 {
				t.Fatal("signalled uncertain process")
			}
		})
	}
}

func TestOrphanExplicitStopAndRecheck(t *testing.T) {
	for _, mode := range []string{"term", "kill", "reused-before-stop", "reused-after-term"} {
		t.Run(mode, func(t *testing.T) {
			f := &ownerFixture{ticks: 123}
			f.onSignal = func(sig os.Signal) {
				if mode == "term" || sig == os.Kill {
					f.ticks = 0
				}
				if mode == "reused-after-term" {
					f.ticks = 456
				}
			}
			s, path := ownerService(t, f)
			if s.Engine().State != "orphaned" || s.Engine().Pid != 4242 || len(f.sent()) != 0 {
				t.Fatal(s.Engine(), f.sent())
			}
			if mode == "reused-before-stop" {
				f.set(456, nil)
			}
			state, e := s.StopOrphanEngine()
			if e != nil || state.State != "stopping" && state.State != "stopped" {
				t.Fatal(state, e)
			}
			wait(t, func() bool { return s.Engine().State == "stopped" })
			assertOwnerGone(t, path)
			want := []os.Signal{syscall.SIGTERM}
			if mode == "kill" {
				want = append(want, os.Kill)
			}
			if mode == "reused-before-stop" {
				want = nil
			}
			if !reflect.DeepEqual(f.sent(), want) {
				t.Fatal(f.sent(), want)
			}
		})
	}
}

func TestOrphanUnreadableIdentityPreservesRecord(t *testing.T) {
	f := &ownerFixture{ticks: 123, failure: os.ErrPermission}
	s, path := ownerService(t, f)
	if s.Engine().Error == nil || s.Engine().Error.Code != "service_identity_unreadable" {
		t.Fatal(s.Engine())
	}
	if _, e := s.StopOrphanEngine(); e == nil || e.Code != "service_identity_unreadable" {
		t.Fatal(e)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if len(f.sent()) != 0 {
		t.Fatal("signalled unreadable identity")
	}
}

func TestOrphanIdentityFailureDuringStopDoesNotKill(t *testing.T) {
	f := &ownerFixture{ticks: 123}
	f.onSignal = func(os.Signal) { f.failure = os.ErrPermission }
	s, path := ownerService(t, f)
	if _, e := s.StopOrphanEngine(); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { state := s.Engine(); return state.State == "orphaned" && state.Error != nil })
	if !reflect.DeepEqual(f.sent(), []os.Signal{syscall.SIGTERM}) {
		t.Fatal(f.sent())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanNotStoppedByShutdownOrDuringRun(t *testing.T) {
	f := &ownerFixture{ticks: 123}
	s, path := ownerService(t, f)
	s.mu.Lock()
	s.runs = append(s.runs, &runEntry{Task: RunTask{State: "running"}})
	s.mu.Unlock()
	if _, e := s.StopOrphanEngine(); e == nil || e.Code != "run_active" {
		t.Fatal(e)
	}
	s.mu.Lock()
	s.runs = nil
	s.mu.Unlock()
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sent()) != 0 {
		t.Fatal("shutdown silently stopped orphan")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerRecordIsPrivateAndDoesNotOverwrite(t *testing.T) {
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		v, _ := version(args)
		return v, nil
	})
	s.identityReader = func(int64) (int64, error) { return 123, nil }
	workspace := t.TempDir()
	e := &engineEntry{state: EngineState{Pid: 4242, Endpoint: "http://127.0.0.1:8080", LogPath: filepath.Join(workspace, "logs", "engine.log"), Candidate: filepath.Join(workspace, "candidates", "candidate-1")}}
	if err := s.persistEngineOwner(e, workspace); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ownerPath(workspace))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 5 {
		t.Fatal(string(raw))
	}
	if fields["log_file"] != "engine.log" || fields["candidate"] != "candidate-1" || fields["start_ticks"] != float64(123) {
		t.Fatal(fields)
	}
	info, err := os.Stat(ownerPath(workspace))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	if err := s.persistEngineOwner(e, workspace); err == nil {
		t.Fatal("overwrote existing ownership")
	}
	after, _ := os.ReadFile(ownerPath(workspace))
	if string(after) != string(raw) {
		t.Fatal("record overwritten")
	}
}

func TestOwnerInvalidRecordDoesNotSignal(t *testing.T) {
	f := &ownerFixture{ticks: 123}
	s, path := ownerService(t, f)
	if err := os.WriteFile(path, []byte(`{"pid":4242,"start_ticks":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	s.detectEngineOwner()
	if s.Engine().Error == nil || s.Engine().Error.Code != "engine_owner_invalid" {
		t.Fatal(s.Engine())
	}
	if _, e := s.StopOrphanEngine(); e == nil {
		t.Fatal("invalid owner accepted")
	}
	if len(f.sent()) != 0 {
		t.Fatal(f.sent())
	}
}

func TestOwnerCleanupPreservesNewIdentity(t *testing.T) {
	path := ownerPath(t.TempDir())
	old := engineOwner{PID: 42, StartTicks: 1, Endpoint: "http://127.0.0.1:8080", LogFile: "old.log", Candidate: "old"}
	current := old
	current.StartTicks = 2
	if err := writeEngineOwner(path, current); err != nil {
		t.Fatal(err)
	}
	if err := removeEngineOwner(path, old); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("removed replacement owner", err)
	}
	var saved engineOwner
	if json.Unmarshal(raw, &saved) != nil || saved.StartTicks != 2 {
		t.Fatal(string(raw))
	}
}
