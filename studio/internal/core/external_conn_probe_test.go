package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProbePortUsingRunner(t *testing.T) {
	selfPID := os.Getpid()
	serverPID := selfPID + 1001
	externalPID := selfPID + 2002

	t.Run("no_connections_exit1_empty_is_exclusive", func(t *testing.T) {
		runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
			return nil, nil, 1, errors.New("exit status 1")
		}
		res := probePortUsingRunner(context.Background(), 8080, []int{serverPID}, runner)
		if res.Status != ExclusivityExclusive || res.Count != 0 || res.Port != 8080 {
			t.Fatalf("expected exclusive, got %+v", res)
		}
	})

	t.Run("only_server_and_studio_pids_is_exclusive", func(t *testing.T) {
		output := fmt.Sprintf("p%d\ncllama-server\nn127.0.0.1:8080->127.0.0.1:50001\np%d\ncstudio\nn127.0.0.1:50001->127.0.0.1:8080\n", serverPID, selfPID)
		runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
			return []byte(output), nil, 0, nil
		}
		res := probePortUsingRunner(context.Background(), 8080, []int{serverPID}, runner)
		if res.Status != ExclusivityExclusive || len(res.Clients) != 0 {
			t.Fatalf("expected exclusive when only server + studio connected, got %+v", res)
		}
	})

	t.Run("external_client_detected_with_unescaped_name", func(t *testing.T) {
		output := fmt.Sprintf(
			"p%d\ncllama-server\nn127.0.0.1:8080->127.0.0.1:54321\np%d\ncCherry\\x20Studio\nn127.0.0.1:54321->127.0.0.1:8080\n",
			serverPID, externalPID,
		)
		runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
			return []byte(output), nil, 0, nil
		}
		res := probePortUsingRunner(context.Background(), 8080, []int{serverPID}, runner)
		if res.Status != ExclusivityExternal || res.Count != 1 {
			t.Fatalf("expected external with count=1, got %+v", res)
		}
		want := fmt.Sprintf("Cherry Studio (PID %d)", externalPID)
		if len(res.Clients) != 1 || res.Clients[0] != want {
			t.Fatalf("expected client %q, got %+v", want, res.Clients)
		}
	})

	t.Run("lsof_missing_returns_unknown_never_exclusive", func(t *testing.T) {
		runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
			return nil, nil, -1, exec.ErrNotFound
		}
		res := probePortUsingRunner(context.Background(), 8080, []int{serverPID}, runner)
		if res.Status != ExclusivityUnknown {
			t.Fatalf("expected unknown when lsof missing, got %+v", res)
		}
	})

	t.Run("lsof_stderr_error_returns_unknown_never_exclusive", func(t *testing.T) {
		runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
			return nil, []byte("lsof: permission denied\n"), 1, errors.New("exit status 1")
		}
		res := probePortUsingRunner(context.Background(), 8080, []int{serverPID}, runner)
		if res.Status != ExclusivityUnknown {
			t.Fatalf("expected unknown on lsof stderr error, got %+v", res)
		}
	})

	t.Run("invalid_port_returns_unknown", func(t *testing.T) {
		res := ProbePortExternalConnections(context.Background(), 0, nil)
		if res.Status != ExclusivityUnknown {
			t.Fatalf("expected unknown for port 0, got %+v", res)
		}
	})
}

func TestMergeExclusivityStatus(t *testing.T) {
	if got := mergeExclusivityStatus(ExclusivityExclusive, ExclusivityExternal); got != ExclusivityExternal {
		t.Fatalf("expected external, got %q", got)
	}
	if got := mergeExclusivityStatus(ExclusivityExternal, ExclusivityExclusive); got != ExclusivityExternal {
		t.Fatalf("expected external to stay locked, got %q", got)
	}
	if got := mergeExclusivityStatus(ExclusivityUnknown, ExclusivityExclusive); got != ExclusivityUnknown {
		t.Fatalf("expected unknown + exclusive to stay unknown, got %q", got)
	}
	if got := mergeExclusivityStatus(ExclusivityExclusive, ExclusivityExclusive); got != ExclusivityExclusive {
		t.Fatalf("expected exclusive, got %q", got)
	}
}

func TestReportExclusivityPersistenceAndServiceProbes(t *testing.T) {
	ws := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())
	wsRaw, _ := json.Marshal(ws)
	if _, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"workspace_root": wsRaw}); apiErr != nil {
		t.Fatal(apiErr)
	}

	if got := svc.ReportExclusivity("missing-report"); got != ExclusivityUnknown {
		t.Fatalf("expected unknown for missing report, got %q", got)
	}

	svc.recordRunExclusivity("run-1", ExclusivityExclusive)
	svc.recordRunExclusivity("run-2", ExclusivityExternal)
	svc.recordReportExclusivity("rep-clean", []string{"run-1"})
	svc.recordReportExclusivity("rep-mixed", []string{"run-1", "run-2"})

	if got := svc.ReportExclusivity("rep-clean"); got != ExclusivityExclusive {
		t.Fatalf("expected rep-clean=exclusive, got %q", got)
	}
	if got := svc.ReportExclusivity("rep-mixed"); got != ExclusivityExternal {
		t.Fatalf("expected rep-mixed=external, got %q", got)
	}
	if got := svc.ReportExclusivity("run-2"); got != ExclusivityExternal {
		t.Fatalf("expected run-2=external, got %q", got)
	}

	origProbe := probePortFunc
	defer func() { probePortFunc = origProbe }()
	probePortFunc = func(ctx context.Context, port int, excludePIDs []int) ExternalConnProbe {
		return ExternalConnProbe{Status: ExclusivityExclusive, Port: port}
	}

	res := svc.ProbeEndpointConnections(context.Background(), "http://127.0.0.1:11434/v1")
	if res.Status != ExclusivityExclusive || res.Port != 11434 {
		t.Fatalf("unexpected ProbeEndpointConnections result: %+v", res)
	}
}
