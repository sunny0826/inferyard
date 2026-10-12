package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func copyRun(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inputs.json", "manifest.json", "events.jsonl", "memory.jsonl"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "run", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(dir, name), string(raw))
	}
}
func TestIndex(t *testing.T) {
	root := t.TempDir()
	copyRun(t, filepath.Join(root, "single"))
	partial := filepath.Join(root, "batch", "runs", "partial")
	copyRun(t, partial)
	if err := os.Remove(filepath.Join(partial, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "batch", "batch.json"), `{"schema_version":3}`)
	copyRun(t, filepath.Join(root, "broken"))
	write(t, filepath.Join(root, "broken", "manifest.json"), "bad")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	index := Index(root)
	if len(index.Runs) != 2 || len(index.Batches) != 1 || index.Skipped != 2 {
		t.Fatalf("%+v", index)
	}
	if index.Batches[0].Runs[0].State != Partial || index.Batches[0].Runs[0].BatchPath != filepath.Join(root, "batch") {
		t.Fatalf("%+v", index.Batches)
	}
	if got := Index(filepath.Join(root, "single")); len(got.Runs) != 1 || got.Runs[0].State != Sealed {
		t.Fatalf("%+v", got)
	}
	if got := Index(filepath.Join(root, "missing")); got.Skipped != 1 {
		t.Fatalf("%+v", got)
	}
}
func TestIndexSealAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		manifest string
		count    int
		state    SealState
	}{
		{`{"schema_version":3,"evidence_layout_version":2,"run_id":"fixture-run","sealed":false}`, 1, Partial},
		{`{"schema_version":3,"evidence_layout_version":2,"run_id":"other","sealed":true}`, 0, ""},
		{`{}`, 0, ""},
	} {
		dir := t.TempDir()
		copyRun(t, dir)
		write(t, filepath.Join(dir, "manifest.json"), tc.manifest)
		got := Index(dir)
		if len(got.Runs) != tc.count || (tc.count > 0 && got.Runs[0].State != tc.state) {
			t.Fatalf("%+v", got)
		}
	}
}
