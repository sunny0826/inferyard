package core

import (
	"context"
	"encoding/json"
	"errors"
	"inferyard/studio/internal/inferyard"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
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

func TestConfigureEngineProcessInheritsHostEnvAndStripsLoaderInjection(t *testing.T) {
	t.Setenv("HF_ENDPOINT", "https://hf-mirror.com")
	t.Setenv("TOKENIZERS_PARALLELISM", "false")
	t.Setenv("OPENAI_API_KEY", "sk-host-secret")
	t.Setenv("PGPASSWORD", "pg-secret")
	t.Setenv("HF_TOKEN", "hf_host_secret")
	t.Setenv("HTTPS_PROXY", "http://user:pa,ss;1@proxy.example.com:8080")
	t.Setenv("DYLD_INSERT_LIBRARIES", "/tmp/evil.dylib")
	t.Setenv("LD_PRELOAD", "/tmp/evil.so")

	cmd := exec.Command("/bin/echo")
	err := configureEngineProcess(cmd, Startup{
		Local:            true,
		WorkingDirectory: t.TempDir(),
		Environment: map[string]string{
			"CUDA_VISIBLE_DEVICES":  "0",
			"DYLD_INSERT_LIBRARIES": "/tmp/override.dylib",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cmd.Env, "HF_ENDPOINT=https://hf-mirror.com") {
		t.Fatalf("expected HF_ENDPOINT in cmd.Env, got %v", cmd.Env)
	}
	if !slices.Contains(cmd.Env, "TOKENIZERS_PARALLELISM=false") {
		t.Fatalf("expected TOKENIZERS_PARALLELISM in cmd.Env, got %v", cmd.Env)
	}
	if !slices.Contains(cmd.Env, "CUDA_VISIBLE_DEVICES=0") {
		t.Fatalf("expected CUDA_VISIBLE_DEVICES in cmd.Env, got %v", cmd.Env)
	}
	for _, entry := range cmd.Env {
		if entry == "DYLD_INSERT_LIBRARIES=/tmp/evil.dylib" ||
			entry == "DYLD_INSERT_LIBRARIES=/tmp/override.dylib" ||
			entry == "LD_PRELOAD=/tmp/evil.so" ||
			entry == "OPENAI_API_KEY=sk-host-secret" ||
			entry == "PGPASSWORD=pg-secret" ||
			entry == "HF_TOKEN=hf_host_secret" ||
			entry == "HTTPS_PROXY=http://user:pa,ss;1@proxy.example.com:8080" {
			t.Fatalf("unexpected blocked/secret variable in cmd.Env: %s", entry)
		}
	}
}
