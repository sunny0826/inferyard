package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLsofEstablishedLANRemoteClientOnServerPID(t *testing.T) {
	// Server PID 5000 is excluded, but it holds an ESTABLISHED connection from a LAN peer 192.168.1.25.
	lsofOut := []byte(
		"p5000\n" +
			"cllama-server\n" +
			"n127.0.0.1:8080->127.0.0.1:51000\n" +
			"n192.168.1.10:8080->192.168.1.25:54321\n",
	)
	runner := func(ctx context.Context, port int) ([]byte, []byte, int, error) {
		return lsofOut, nil, 0, nil
	}
	res := probePortUsingRunner(context.Background(), 8080, []int{5000}, runner)
	if res.Status != ExclusivityExternal {
		t.Fatalf("expected LAN peer on server PID to mark status=external, got %+v", res)
	}
	if len(res.Clients) != 1 || res.Clients[0] != "LAN (192.168.1.25)" {
		t.Fatalf("unexpected LAN clients list: %+v", res.Clients)
	}

	// Only loopback connections on excluded server PID -> exclusive.
	loopbackOut := []byte(
		"p5000\n" +
			"cllama-server\n" +
			"n127.0.0.1:8080->127.0.0.1:51000\n" +
			"n[::1]:8080->[::1]:51001\n" +
			"n[::ffff:127.0.0.1]:8080->[::ffff:127.0.0.1]:51002\n",
	)
	resLoop := probePortUsingRunner(context.Background(), 8080, []int{5000}, func(ctx context.Context, port int) ([]byte, []byte, int, error) {
		return loopbackOut, nil, 0, nil
	})
	if resLoop.Status != ExclusivityExclusive {
		t.Fatalf("expected loopback-only server connections to be exclusive, got %+v", resLoop)
	}
}

func TestProbePortExcludesStudioDescendantsAndExternalServerPID(t *testing.T) {
	origDesc := descendantPIDsFunc
	descendantPIDsFunc = func(rootPID int) []int { return []int{7777} }
	defer func() { descendantPIDsFunc = origDesc }()

	// PID 7777 is a Studio descendant (e.g. inferyard CLI); PID 8888 is external server passed in extraExcludePIDs.
	lsofOut := []byte(
		"p7777\n" +
			"cinferyard\n" +
			"n127.0.0.1:51234->127.0.0.1:11434\n" +
			"p8888\n" +
			"collama\n" +
			"n127.0.0.1:11434->127.0.0.1:51234\n",
	)
	origRunner := lsofRunner
	lsofRunner = func(ctx context.Context, port int) ([]byte, []byte, int, error) {
		return lsofOut, nil, 0, nil
	}
	defer func() { lsofRunner = origRunner }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	res := svc.ProbeEndpointConnections(ctx, "http://127.0.0.1:11434", 8888)
	if res.Status != ExclusivityExclusive {
		t.Fatalf("expected Studio descendant and discovered server PID to be excluded -> exclusive, got %+v", res)
	}
}

func TestStartRunExclusivityLocksInitialExternalAndResumeTarget(t *testing.T) {
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

	origProbe := probePortFunc
	probePortFunc = func(ctx context.Context, port int, excludePIDs []int) ExternalConnProbe {
		return ExternalConnProbe{Status: ExclusivityExclusive, Port: port}
	}
	defer func() { probePortFunc = origProbe }()

	runID := "run-locked-ext"
	entry := &runEntry{
		Task:               RunTask{TaskId: "t-lock", RunId: &runID},
		initialExclusivity: ExclusivityExternal,
		probeEndpoint:      "http://127.0.0.1:8080",
		probePID:           4242,
	}
	tr := svc.startRunExclusivity(ctx, entry)
	tr.poll(ctx, entry)
	tr.finish(entry)
	if entry.Task.Exclusivity != ExclusivityExternal {
		t.Fatalf("expected InitialExclusivity=external to stay locked even when probe is exclusive, got %q", entry.Task.Exclusivity)
	}
	if got := svc.ReportExclusivity(runID); got != ExclusivityExternal {
		t.Fatalf("expected persisted exclusivity=external for %s, got %q", runID, got)
	}

	// Resume task with explicit probeEndpoint/probePID resolves target instead of unknown.
	resumeEntry := &runEntry{
		Task:          RunTask{TaskId: "t-resume", Kind: "resume"},
		probeEndpoint: "http://127.0.0.1:9090",
		probePID:      6100,
	}
	port, pids := svc.resolveRunProbeTarget(resumeEntry)
	if port != 9090 || len(pids) == 0 || pids[0] != 6100 {
		t.Fatalf("expected resume probe target port=9090 pid=6100, got port=%d pids=%v", port, pids)
	}

	// Batch ReportExclusivities resolves statuses from RunIds in one sidecar read.
	svc.recordRunExclusivity("run-clean", ExclusivityExclusive)
	batch := svc.ReportExclusivities([]ReportSummary{
		{ReportId: "rep-ext", RunIds: []string{runID}},
		{ReportId: "rep-clean", RunIds: []string{"run-clean"}},
		{ReportId: "rep-none", RunIds: []string{"run-missing"}},
	})
	if batch["rep-ext"] != ExclusivityExternal || batch["rep-clean"] != ExclusivityExclusive || batch["rep-none"] != ExclusivityUnknown {
		t.Fatalf("unexpected batch ReportExclusivities: %+v", batch)
	}
	_ = os.RemoveAll(ws)
}
