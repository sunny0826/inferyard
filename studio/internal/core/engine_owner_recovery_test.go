package core

import (
	"os"
	"testing"
)

func TestDiscardOrphanRecordAllowsRestart(t *testing.T) {
	for _, mode := range []string{"invalid", "unreadable-identity", "stale-after-startup", "cleanup-failed"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ENGINE_HELPER", "1")
			workspace, candidate := engineWorkspace(t, freePort(t))
			s := engineService(t, workspace)
			path := ownerPath(workspace)
			owner := engineOwner{PID: 4242, StartTicks: 123, Endpoint: "http://127.0.0.1:8080", LogFile: "engine.log", Candidate: "c1"}
			if err := writeEngineOwner(path, owner); err != nil {
				t.Fatal(err)
			}
			f := &ownerFixture{ticks: 123}
			s.identityReader, s.processSignal = f.read, f.signal
			if mode == "unreadable-identity" {
				f.failure = os.ErrPermission
			} else {
				if err := os.WriteFile(path, []byte(`{"pid":4242}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "stale-after-startup" {
				s.detectEngineOwner()
			}
			if mode == "cleanup-failed" {
				s.engine = &engineEntry{state: EngineState{State: "failed"}}
			}
			if _, e := s.StartEngine(candidate); e == nil || e.Code != "engine_orphaned" {
				t.Fatal("start did not preserve stale record", e)
			}
			if s.Engine().State != "orphaned" {
				t.Fatal(s.Engine())
			}
			state, e := s.DiscardOrphanRecord()
			if e != nil || state.State != "stopped" || s.Engine().State != "stopped" {
				t.Fatal(state, e, s.Engine())
			}
			assertOwnerGone(t, path)
			if len(f.sent()) != 0 {
				t.Fatal("record deletion signalled a process", f.sent())
			}
			s.identityReader = nil
			if state, e := s.StartEngine(candidate); e != nil || state.State != "starting" {
				t.Fatal(state, e)
			}
			pollEngine(t, s, "running")
			if _, e := s.StopEngine(); e != nil {
				t.Fatal(e)
			}
			pollEngine(t, s, "stopped")
		})
	}
}

func TestDiscardOrphanRecordRejectsMatchingIdentity(t *testing.T) {
	for _, repaired := range []bool{false, true} {
		f := &ownerFixture{ticks: 123}
		s, path := ownerService(t, f)
		if repaired {
			f.set(123, os.ErrPermission)
			s.detectEngineOwner()
			f.set(123, nil)
		}
		if _, e := s.DiscardOrphanRecord(); e == nil || e.Code != "engine_orphaned" {
			t.Fatal("bypassed matching process stop", e)
		}
		if _, err := os.Stat(path); err != nil || s.Engine().State != "orphaned" || len(f.sent()) != 0 {
			t.Fatal(err, s.Engine(), f.sent())
		}
	}
}

func TestDiscardOrphanRecordRechecksReplacedRecord(t *testing.T) {
	f := &ownerFixture{ticks: 123}
	s, path := ownerService(t, f)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s.detectEngineOwner()
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := s.DiscardOrphanRecord(); e == nil || e.Code != "engine_orphaned" {
		t.Fatal(e)
	}
	if len(f.sent()) != 0 {
		t.Fatal(f.sent())
	}
}

func TestManagedEngineCleanupFailureCanRecover(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	workspace, candidate := engineWorkspace(t, freePort(t))
	s := engineService(t, workspace)
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	pollEngine(t, s, "running")
	if err := os.WriteFile(ownerPath(workspace), []byte("damaged record"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, e := s.StopEngine(); e != nil {
		t.Fatal(e)
	}
	state := pollEngine(t, s, "orphaned")
	if state.Error == nil || state.Error.Code != "engine_owner_remove_failed" {
		t.Fatal(state)
	}
	if _, e := s.DiscardOrphanRecord(); e != nil {
		t.Fatal(e)
	}
	if _, e := s.StartEngine(candidate); e != nil {
		t.Fatal(e)
	}
	pollEngine(t, s, "running")
}

func TestDiscardOrphanRecordRespectsActiveWork(t *testing.T) {
	for _, mode := range []string{"run", "stopping", "closing"} {
		t.Run(mode, func(t *testing.T) {
			f := &ownerFixture{ticks: 123, failure: os.ErrPermission}
			s, path := ownerService(t, f)
			s.mu.Lock()
			switch mode {
			case "run":
				s.runs = []*runEntry{{Task: RunTask{State: "running"}}}
			case "stopping":
				s.orphan.state.State = "stopping"
			case "closing":
				s.closing = true
			}
			s.mu.Unlock()
			_, e := s.DiscardOrphanRecord()
			s.mu.Lock()
			s.runs, s.closing, s.orphan.state.State = nil, false, "orphaned"
			s.mu.Unlock()
			want := map[string]string{"run": "run_active", "stopping": "engine_stopping", "closing": "service_closing"}[mode]
			if e == nil || e.Code != want {
				t.Fatal(e)
			}
			if _, err := os.Stat(path); err != nil || len(f.sent()) != 0 {
				t.Fatal(err, f.sent())
			}
		})
	}
}
