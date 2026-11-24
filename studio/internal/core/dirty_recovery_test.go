package core

import (
	"context"
	"inferyard/studio/internal/evidence"
	"inferyard/studio/internal/inferyard"
	"os"
	"reflect"
	"runtime"
	"slices"
	"testing"
)

func writeDirtyHostMarker(t *testing.T, path string, dirty bool, token string, pid, ticks int64, workerPID, workerTicks int64) {
	t.Helper()
	payload := `{
  "schema_version": 1,
  "dirty": ` + boolJSON(dirty) + `,
  "dirty_token": "` + token + `",
  "run_id": "20261124T074759Z-prev",
  "kind": "run",
  "endpoint": "http://127.0.0.1:53050",
  "server_pid": ` + itoa64(pid) + `,
  "process_start_ticks": ` + itoa64(ticks)
	if workerPID > 0 {
		payload += `,
  "local_service": {
    "definition": "local-host-service.v1",
    "scope_sha256": "5eb0ffd86be23c6d0ce1d2e10d1e01e5b356cbc2a11e84fd5f5f6ef943cb416a",
    "service": {
      "pid": ` + itoa64(pid) + `,
      "start_ticks": ` + itoa64(ticks) + `,
      "workers": [{"pid": ` + itoa64(workerPID) + `, "start_ticks": ` + itoa64(workerTicks) + `}]
    }
  }`
	}
	payload += "\n}\n"
	if err := os.WriteFile(path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
}

func boolJSON(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func itoa64(v int64) string {
	var buf [32]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	if v == 0 {
		return "0"
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestWithDirtyRecoveryArgsSafetyBoundaries(t *testing.T) {
	s := testService(t, nil)
	live := map[int64]int64{}
	s.identityReader = func(pid int64) (int64, error) {
		if ticks, ok := live[pid]; ok {
			return ticks, nil
		}
		return 0, os.ErrNotExist
	}
	base := []string{"run", "--config", "/tmp/config.toml"}

	// Clean host state: no recovery flags appended.
	writeDirtyHostMarker(t, s.hostStatePath, false, "tok-clean", 100, 1000, 0, 0)
	if got := s.withDirtyRecoveryArgs(base, 200, 2000); !slices.Equal(got, base) {
		t.Fatalf("unexpected recovery args on clean marker: %v", got)
	}

	// Dirty state with old process and worker gone + new process started: recovery flags appended.
	writeDirtyHostMarker(t, s.hostStatePath, true, "tok-dirty", 100, 1000, 101, 1001)
	want := []string{"run", "--config", "/tmp/config.toml", "--recovery-confirm", "tok-dirty", "--recovery-note", studioDirtyRecoveryNote}
	if got := s.withDirtyRecoveryArgs(base, 200, 2000); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// Same process identity as dirty marker: never append recovery flags.
	if got := s.withDirtyRecoveryArgs(base, 100, 1000); !slices.Equal(got, base) {
		t.Fatalf("must not append recovery for same process: %v", got)
	}

	// Old server process still alive: never append recovery flags.
	live[100] = 1000
	if got := s.withDirtyRecoveryArgs(base, 200, 2000); !slices.Equal(got, base) {
		t.Fatalf("must not append recovery while old server alive: %v", got)
	}
	delete(live, 100)

	// Old local worker still alive: never append recovery flags.
	live[101] = 1001
	if got := s.withDirtyRecoveryArgs(base, 200, 2000); !slices.Equal(got, base) {
		t.Fatalf("must not append recovery while old worker alive: %v", got)
	}

	// PID reused with different start_ticks counts as old worker gone.
	live[101] = 9999
	if got := s.withDirtyRecoveryArgs(base, 200, 2000); !slices.Equal(got, want) {
		t.Fatalf("expected recovery when old PID has new start_ticks, got %v", got)
	}
}

func TestLocalQueueAutoRecoversDirtyHostOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local process isolation is unavailable on Windows")
	}
	f := newLocalQueueFixture(t, "polystrata")
	writeDirtyHostMarker(t, f.s.hostStatePath, true, "tok-polystrata-cancel", 999991, 888881, 999992, 888882)

	var recordedRunArgs [][]string
	origExec := f.s.exec
	f.s.exec = func(ctx context.Context, bin string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "run" {
			f.mu.Lock()
			recordedRunArgs = append(recordedRunArgs, append([]string{}, args...))
			f.mu.Unlock()
			if slices.Contains(args, "--recovery-confirm") {
				// Simulate CLI clearing dirty state upon successful recovery.
				writeDirtyHostMarker(t, f.s.hostStatePath, false, "tok-polystrata-cancel", 999991, 888881, 0, 0)
			}
		}
		return origExec(ctx, bin, args...)
	}

	tasks, failure := f.s.StartCandidateRuns(CandidateRunsRequest{Candidates: f.candidates})
	if failure != nil {
		t.Fatal(failure)
	}
	for _, task := range f.done(t, tasks) {
		if task.State != "done" || task.Exit.ExitCode != 0 {
			t.Fatalf("%+v", task)
		}
	}
	if len(recordedRunArgs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(recordedRunArgs))
	}
	firstTail := recordedRunArgs[0][len(recordedRunArgs[0])-4:]
	wantTail := []string{"--recovery-confirm", "tok-polystrata-cancel", "--recovery-note", studioDirtyRecoveryNote}
	if !reflect.DeepEqual(firstTail, wantTail) {
		t.Fatalf("first run args missing recovery tail: %v", recordedRunArgs[0])
	}
	if slices.Contains(recordedRunArgs[1], "--recovery-confirm") {
		t.Fatalf("second run must not resend consumed recovery token: %v", recordedRunArgs[1])
	}
}

func TestBaselinePhaseFromResourcesTransitionsToFormal(t *testing.T) {
	inputs := []byte(`{"documents":{"selection":{"payload":{"case_ids":["case-1","case-2"]}}}}`)
	a := newAggregate(inputs)

	// Warmup residual event sets phase to "residual".
	a.events([]evidence.Record{{
		Seq:       1,
		Phase:     "residual",
		EventType: "native_observed",
		Raw:       []byte(`{"seq":1,"utc":"2026-11-24T07:49:56Z","phase":"residual","event_type":"native_observed","data":{}}`),
	}})
	if str(a.Progress.Phase) != "residual" {
		t.Fatalf("expected residual phase, got %v", str(a.Progress.Phase))
	}

	// Baseline resource sample updates phase to "baseline" while no formal case has started.
	a.resources([]evidence.Record{{
		Seq: 2,
		Raw: []byte(`{"metric_name":"service_rss","collector":"macos","source":"psutil","clock_id":"c1","unit":"bytes","semantics":"rss","utc":"2026-11-24T07:49:57Z","read_started_ns":100,"read_finished_ns":110,"phase":"baseline","value":1024}`),
	}})
	if str(a.Progress.Phase) != "baseline" {
		t.Fatalf("expected baseline phase from resource sample, got %v", str(a.Progress.Phase))
	}

	// Formal request_started transitions to "formal" with active case, and lagged baseline samples do not clobber it.
	a.events([]evidence.Record{{
		Seq:       3,
		Phase:     "formal",
		EventType: "request_started",
		Raw:       []byte(`{"seq":3,"utc":"2026-11-24T07:50:02Z","phase":"formal","event_type":"request_started","request_id":"req-1","data":{"case_id":"case-1","plan_index":0}}`),
	}})
	a.resources([]evidence.Record{{
		Seq: 4,
		Raw: []byte(`{"metric_name":"service_rss","collector":"macos","source":"psutil","clock_id":"c1","unit":"bytes","semantics":"rss","utc":"2026-11-24T07:50:02Z","read_started_ns":200,"read_finished_ns":210,"phase":"baseline","value":1024}`),
	}})
	if str(a.Progress.Phase) != "formal" || str(a.Progress.CurrentCaseId) != "case-1" {
		t.Fatalf("expected formal phase with active case-1, got phase=%v case=%v", str(a.Progress.Phase), str(a.Progress.CurrentCaseId))
	}
}
