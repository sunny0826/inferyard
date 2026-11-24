package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextSpeedZeroBatchExcludedFromOrdinaryHistory(t *testing.T) {
	s := testService(t, nil)
	root := t.TempDir()
	s.settings.ResultsRoot = &root
	batch := filepath.Join(root, "zero-speed")
	if err := os.MkdirAll(filepath.Join(batch, "frozen-plan"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string]string{"batch.json": `{}`, "frozen-plan/plan.json": `{"experiment":{"profile":"context-speed.v1"}}`} {
		if err := os.WriteFile(filepath.Join(batch, path), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	history, e := s.scan(s.userSettings())
	if e != nil || len(history.Batches) != 0 || len(history.StandaloneRuns) != 0 {
		t.Fatalf("specialist leaked into ordinary history %+v %v", history, e)
	}
}
