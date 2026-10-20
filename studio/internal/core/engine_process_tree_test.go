package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEngineTreeHelper(t *testing.T) {
	role := os.Getenv("INFERYARD_TREE_HELPER")
	if role == "" {
		return
	}
	dir := os.Getenv("INFERYARD_TREE_DIR")
	if role == "worker" {
		signal.Ignore(syscall.SIGTERM)
		_ = os.WriteFile(filepath.Join(dir, "worker"), []byte(strconv.Itoa(os.Getpid())), 0600)
		for {
			time.Sleep(time.Second)
		}
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestEngineTreeHelper$")
	cmd.Env = append(os.Environ(), "INFERYARD_TREE_HELPER=worker")
	if err := cmd.Start(); err != nil {
		os.Exit(2)
	}
	go func() { _ = cmd.Wait() }()
	go func() {
		for {
			if _, err := os.Stat(filepath.Join(dir, "exit")); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	http.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) })
	if err := http.ListenAndServe(os.Getenv("INFERYARD_TREE_ADDRESS"), nil); err != nil {
		os.Exit(3)
	}
}

func startTreeFixture(t *testing.T) (*Service, *engineEntry, string, int64) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local process groups require macOS or Linux")
	}
	dir := t.TempDir()
	s := engineService(t, dir)
	s.orphanStopTimeout = 100 * time.Millisecond
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	startup := Startup{Path: os.Args[0], Args: []string{"-test.run=^TestEngineTreeHelper$"}, Local: true, Kind: "direct_process", WorkingDirectory: dir, Environment: map[string]string{"INFERYARD_TREE_HELPER": "root", "INFERYARD_TREE_DIR": dir, "INFERYARD_TREE_ADDRESS": address}, Endpoint: "http://" + address}
	cmd := exec.Command(startup.Path, startup.Args...)
	if err := configureEngineProcess(cmd, startup); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "engine.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	e := &engineEntry{cmd: cmd, logFile: log, stop: make(chan struct{}), done: make(chan struct{}), state: EngineState{State: "starting", Pid: int64(cmd.Process.Pid), Local: true, Endpoint: startup.Endpoint, LogPath: log.Name(), Candidate: "test-candidate"}}
	if failure := s.persistEngineOwner(e, dir); failure != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(failure)
	}
	s.mu.Lock()
	s.engine = e
	s.mu.Unlock()
	go s.monitorEngine(e, startup.Endpoint)
	var worker int64
	wait(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(dir, "worker"))
		if err != nil {
			return false
		}
		worker, _ = strconv.ParseInt(string(raw), 10, 64)
		return worker > 0
	})
	t.Cleanup(func() {
		if process, err := os.FindProcess(int(worker)); err == nil {
			_ = process.Kill()
			_ = process.Release()
		}
	})
	pollEngine(t, s, "running")
	return s, e, dir, worker
}

func TestLocalEngineTreeStopAndRootExit(t *testing.T) {
	for _, mode := range []string{"stop", "root-exit", "handoff"} {
		t.Run(mode, func(t *testing.T) {
			s, e, dir, worker := startTreeFixture(t)
			// Read only the atomic persisted snapshot while the monitor owns e.owner.
			raw, err := os.ReadFile(e.ownerPath)
			if err != nil {
				t.Fatal(err)
			}
			var owner engineOwner
			if json.Unmarshal(raw, &owner) != nil || len(owner.Workers) != 1 || owner.Workers[0].PID != worker {
				t.Fatalf("worker not persisted: %s", raw)
			}
			if s.Engine().StartTicks <= 0 || !s.Engine().Local {
				t.Fatal(s.Engine())
			}
			switch mode {
			case "root-exit":
				if err := os.WriteFile(filepath.Join(dir, "exit"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "handoff":
				s.mu.Lock()
				s.runs = append(s.runs, &runEntry{Task: RunTask{State: "queued", Queued: true}})
				s.mu.Unlock()
				if _, failure := s.StopEngine(); failure == nil || failure.Code != "run_active" {
					t.Fatal(failure)
				}
				if failure := s.stopEngineForHandoff(context.Background()); failure != nil {
					t.Fatal(failure)
				}
				s.mu.Lock()
				s.runs = nil
				s.mu.Unlock()
			default:
				if _, failure := s.StopEngine(); failure != nil {
					t.Fatal(failure)
				}
			}
			select {
			case <-e.done:
			case <-time.After(5 * time.Second):
				t.Fatal("tree not reaped")
			}
			if _, err := processStartTicks(worker); !os.IsNotExist(err) {
				t.Fatalf("worker survived: %v", err)
			}
			if _, err := processStartTicks(e.state.Pid); !os.IsNotExist(err) {
				t.Fatalf("root survived: %v", err)
			}
			assertOwnerGone(t, e.ownerPath)
			if mode == "root-exit" && s.Engine().State != "failed" {
				t.Fatal(s.Engine())
			}
		})
	}
}

func TestLocalEngineEnvironmentAndValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process groups require macOS or Linux")
	}
	t.Setenv("INFERYARD_UNDECLARED_SECRET", "must-not-leak")
	t.Setenv("PYTHONPATH", "must-not-leak")
	startup := Startup{Path: os.Args[0], Local: true, Kind: "direct_process", WorkingDirectory: t.TempDir(), Environment: map[string]string{"OMP_NUM_THREADS": "2"}, Endpoint: "http://127.0.0.1:8765"}
	if _, _, err := validateStartup(startup); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(startup.Path)
	if err := configureEngineProcess(cmd, startup); err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != startup.WorkingDirectory {
		t.Fatal(cmd.Dir)
	}
	values := map[string]string{}
	for _, item := range cmd.Env {
		k, v, _ := strings.Cut(item, "=")
		values[k] = v
	}
	if values["HOME"] != os.Getenv("HOME") || values["OMP_NUM_THREADS"] != "2" || values["PYTHONPATH"] != "" || values["INFERYARD_UNDECLARED_SECRET"] != "" {
		t.Fatal("incorrect environment isolation")
	}
	startup.Environment["HOME"] = "/unexpected"
	if _, _, err := validateStartup(startup); err == nil || err.Code != "engine_environment_invalid" {
		t.Fatal(err)
	}
	startup.Kind = "lmstudio"
	if _, _, err := validateStartup(startup); err == nil || err.Code != "local_engine_external_binding_required" {
		t.Fatal(err)
	}
	legacy := exec.Command(os.Args[0])
	if err := configureEngineProcess(legacy, Startup{}); err != nil || legacy.Env != nil || legacy.Dir != "" || legacy.SysProcAttr != nil {
		t.Fatal("legacy changed")
	}
}

func TestEngineTreeOwnerCompareReplace(t *testing.T) {
	path := ownerPath(t.TempDir())
	owner := engineOwner{PID: 42, StartTicks: 1}
	if err := writeEngineOwner(path, owner); err != nil {
		t.Fatal(err)
	}
	next := owner
	next.Workers = []engineProcessIdentity{{43, 2}}
	if err := replaceEngineOwner(path, owner, next); err != nil {
		t.Fatal(err)
	}
	if err := replaceEngineOwner(path, owner, owner); err == nil {
		t.Fatal("stale owner replaced latest snapshot")
	}
	if err := removeEngineOwner(path, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stale owner removed latest snapshot")
	}
}

func TestHandoffRejectsRealCLIChildAndOrphan(t *testing.T) {
	s := &Service{runs: []*runEntry{{commandActive: true}}}
	if err := s.stopEngineForHandoff(context.Background()); err == nil || err.Code != "run_active" {
		t.Fatal(err)
	}
	s.runs = nil
	s.orphan = &orphanEngine{}
	if err := s.stopEngineForHandoff(context.Background()); err == nil || err.Code != "engine_orphaned" {
		t.Fatal(err)
	}
}

func treeIdentityFixture(t *testing.T) (*Service, engineOwner, string, map[int64]int64, *[]int64) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local process groups require macOS or Linux")
	}
	owner := engineOwner{Local: true, PID: 2000000000, ProcessGroup: 2000000000, StartTicks: 10, Workers: []engineProcessIdentity{{2000000001, 11}}, Endpoint: "http://127.0.0.1:8765", LogFile: "engine.log", Candidate: "candidate"}
	workspace := t.TempDir()
	path := ownerPath(workspace)
	if err := writeEngineOwner(path, owner); err != nil {
		t.Fatal(err)
	}
	ticks := map[int64]int64{owner.Workers[0].PID: 11}
	var signals []int64
	s := &Service{settings: UserSettings{WorkspaceRoot: &workspace}, Bus: NewEventBus(), orphanStopTimeout: time.Millisecond}
	s.identityReader = func(pid int64) (int64, error) {
		if value, ok := ticks[pid]; ok {
			return value, nil
		}
		return 0, os.ErrNotExist
	}
	s.processSignal = func(pid int64, sig os.Signal) error { signals = append(signals, pid); delete(ticks, pid); return nil }
	return s, owner, path, ticks, &signals
}

func TestLocalOrphanRootGoneKeepsWorkerAndStopsIt(t *testing.T) {
	s, owner, path, _, signals := treeIdentityFixture(t)
	s.detectEngineOwner()
	if s.Engine().State != "orphaned" {
		t.Fatal(s.Engine())
	}
	if _, err := s.DiscardOrphanRecord(); err == nil || err.Code != "engine_orphaned" {
		t.Fatal(err)
	}
	if failure := s.terminateOwnedTree(&owner, path); failure != nil {
		t.Fatal(failure)
	}
	if len(*signals) != 1 || (*signals)[0] != owner.Workers[0].PID {
		t.Fatal(*signals)
	}
	s.detectEngineOwner()
	assertOwnerGone(t, path)
}

func TestLocalOrphanWorkerReuseNeverSignals(t *testing.T) {
	s, owner, path, ticks, signals := treeIdentityFixture(t)
	ticks[owner.Workers[0].PID] = 99
	s.detectEngineOwner()
	if s.Engine().State != "stopped" {
		t.Fatal(s.Engine())
	}
	assertOwnerGone(t, path)
	if len(*signals) != 0 {
		t.Fatal(*signals)
	}
}

func TestLocalUnknownWorkerBlocksStopAndDiscard(t *testing.T) {
	s, owner, path, _, signals := treeIdentityFixture(t)
	original := s.identityReader
	s.identityReader = func(pid int64) (int64, error) {
		if pid == owner.Workers[0].PID {
			return 0, os.ErrPermission
		}
		return original(pid)
	}
	s.detectEngineOwner()
	if s.Engine().State != "orphaned" {
		t.Fatal(s.Engine())
	}
	if _, err := s.StopOrphanEngine(); err == nil || err.Code != "service_identity_unreadable" {
		t.Fatal(err)
	}
	if _, err := s.DiscardOrphanRecord(); err == nil || err.Code != "service_identity_unreadable" {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if len(*signals) != 0 {
		t.Fatal(*signals)
	}
}
