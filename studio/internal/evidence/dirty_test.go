package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDirtyStateAndDirtyRun(t *testing.T) {
	dir := t.TempDir()
	missingPath := filepath.Join(dir, "missing.json")
	if st, err := ReadDirtyState(missingPath); err != nil || st != nil {
		t.Fatalf("expected nil on missing file, got %+v, %v", st, err)
	}

	cleanPath := filepath.Join(dir, "clean.json")
	if err := os.WriteFile(cleanPath, []byte(`{"schema_version":1,"dirty":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if st, err := ReadDirtyState(cleanPath); err != nil || st != nil {
		t.Fatalf("expected nil on clean state, got %+v, %v", st, err)
	}

	dirtyPath := filepath.Join(dir, "dirty.json")
	raw := `{
  "schema_version": 1,
  "dirty": true,
  "dirty_token": "tok-123",
  "run_id": "run-abc",
  "kind": "run",
  "server_pid": 24175,
  "process_start_ticks": 1795506469721527,
  "local_service": {
    "definition": "local-host-service.v1",
    "service": {
      "pid": 24175,
      "start_ticks": 1795506469721527,
      "workers": [{"pid": 24243, "start_ticks": 1795506470032423}]
    }
  }
}`
	if err := os.WriteFile(dirtyPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := ReadDirtyState(dirtyPath)
	if err != nil || st == nil {
		t.Fatalf("unexpected error reading dirty state: %+v, %v", st, err)
	}
	if st.DirtyToken != "tok-123" || st.ServerPID != 24175 || len(st.LocalProcesses) != 2 || st.LocalProcesses[1].PID != 24243 {
		t.Fatalf("unexpected parsed dirty state: %+v", st)
	}
	if ok, err := DirtyRun(dirtyPath, "run-abc"); err != nil || !ok {
		t.Fatalf("expected DirtyRun true, got %v, %v", ok, err)
	}

	corruptLocal := filepath.Join(dir, "corrupt-local.json")
	if err := os.WriteFile(corruptLocal, []byte(`{"schema_version":1,"dirty":true,"run_id":"r","kind":"run","local_service":{"service":{"pid":1,"start_ticks":2}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDirtyState(corruptLocal); err == nil {
		t.Fatal("expected error when local_service.service.workers is missing")
	}
}
