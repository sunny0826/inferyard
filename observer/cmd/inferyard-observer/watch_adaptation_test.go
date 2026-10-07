package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/observer/internal/observe"
)

func boundRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	p, err := observe.NativeProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{"schema_version":3,"endpoint":{"server_pid":%d,"process_start_ticks":%d}}`, p.Target.PID, p.Target.StartTicks)
	for name, data := range map[string]string{"run.json": `{"schema_version":3,"run_id":"fixture"}`, "config.frozen.json": config} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBenchmarkWatchReadsRunDiskRatherThanCWD(t *testing.T) {
	root := boundRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(200*time.Millisecond, cancel)
	defer timer.Stop()
	var out, diag bytes.Buffer
	o := options{root: root, interval: 100 * time.Millisecond, duration: time.Second}
	code := watch(ctx, o, "a-session", filepath.Join(t.TempDir(), "absent"), &out, &diag)
	if code != 130 {
		t.Fatal(code, diag.String())
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	var header observe.Header
	var sample observe.Sample
	if len(lines) < 3 || json.Unmarshal(lines[0], &header) != nil || json.Unmarshal(lines[1], &sample) != nil {
		t.Fatal("missing records")
	}
	if header.Definition != "lab_observer.v2" || header.DiskScope != "benchmark_run_filesystem" || sample.Host.DiskAvailable.Value == nil || *sample.Host.DiskAvailable.Value == 0 {
		t.Fatal(header, sample.Host)
	}
}

func TestBenchmarkWatchCannotWriteIntoRun(t *testing.T) {
	root := boundRoot(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, parent := range []string{root, nested} {
		path := filepath.Join(parent, "monitor.jsonl")
		var out, diag bytes.Buffer
		code := run(context.Background(), []string{"watch", "--bench-run", root, "--out", path}, &out, &diag)
		if code != 2 || out.Len() != 0 || !bytes.Contains(diag.Bytes(), []byte("outside_benchmark_run")) {
			t.Fatal(code, diag.String())
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("run was modified", err)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if inside, err := outputInsideRun(root, filepath.Join(alias, "new.jsonl")); err != nil || !inside {
		t.Fatal("source symlink bypass", inside, err)
	}
	if inside, err := outputInsideRun(root, filepath.Join(t.TempDir(), "new.jsonl")); err != nil || inside {
		t.Fatal("independent output rejected", inside, err)
	}
}
