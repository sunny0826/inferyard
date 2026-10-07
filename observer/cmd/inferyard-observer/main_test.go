package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"inferyard/observer/internal/observe"
)

func TestRejectInvalidRequestsBeforeOutput(t *testing.T) {
	for _, args := range [][]string{{"watch"}, {"watch", "--pid", "4294967298"}, {"watch", "--pid", "1", "--duration", "0s"}, {"watch", "--pid", "1", "--interval", "1ms"}, {"watch", "--pid", "1", "--bench-run", "x"}, {"watch", "--pid", "1", "--endpoint", "http://secret@127.0.0.1:80"}, {"snapshot", "secret"}} {
		var out, diag bytes.Buffer
		if code := run(context.Background(), args, &out, &diag); code != 2 || out.Len() != 0 {
			t.Fatal(args, code, out.String())
		}
		if bytes.Contains(diag.Bytes(), []byte("secret")) {
			t.Fatal("raw credential diagnostic")
		}
	}
}

func TestWatchRejectsStaleBenchmarkAndRecordsChildExit(t *testing.T) {
	if os.Getenv("LAB_OBSERVER_CLI_TEST_CHILD") == "1" {
		io.Copy(io.Discard, os.Stdin)
		return
	}
	root := t.TempDir()
	p, err := observe.NativeProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "run.json"), []byte(`{"schema_version":3,"run_id":"fixture"}`), 0600)
	config := fmt.Sprintf(`{"schema_version":3,"endpoint":{"server_pid":%d,"process_start_ticks":%d}}`, p.Target.PID, p.Target.StartTicks+1)
	os.WriteFile(filepath.Join(root, "config.frozen.json"), []byte(config), 0600)
	var out, diag bytes.Buffer
	if code := run(context.Background(), []string{"watch", "--bench-run", root}, &out, &diag); code != 2 || out.Len() != 0 {
		t.Fatal(code, out.String(), diag.String())
	}
	config = fmt.Sprintf(`{"schema_version":3,"endpoint":{"server_pid":%d,"process_start_ticks":%d}}`, p.Target.PID, p.Target.StartTicks)
	os.WriteFile(filepath.Join(root, "config.frozen.json"), []byte(config), 0600)
	if code := run(context.Background(), []string{"watch", "--bench-run", root, "--duration", "1s"}, &out, &diag); code != 0 || !bytes.Contains(out.Bytes(), []byte(`"run_id":"fixture"`)) {
		t.Fatal(code, diag.String())
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWatchRejectsStaleBenchmarkAndRecordsChildExit$")
	child.Env = append(os.Environ(), "LAB_OBSERVER_CLI_TEST_CHILD=1")
	pipe, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pipe.Close(); child.Process.Kill(); child.Wait() })
	timer := time.AfterFunc(300*time.Millisecond, func() { pipe.Close(); child.Wait() })
	defer timer.Stop()
	out.Reset()
	code := run(context.Background(), []string{"watch", "--pid", fmt.Sprint(child.Process.Pid), "--duration", "3s", "--interval", "100ms"}, &out, &diag)
	if code != 3 || !bytes.Contains(out.Bytes(), []byte(`"stop_reason":"target_unavailable"`)) || !bytes.Contains(out.Bytes(), []byte(`"state":"exited"`)) {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestWatchCompleteExclusiveOutputAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.jsonl")
	args := []string{"watch", "--pid", fmt.Sprint(os.Getpid()), "--duration", "1s", "--interval", "100ms", "--out", path}
	var out, diag bytes.Buffer
	if code := run(context.Background(), args, &out, &diag); code != 0 {
		t.Fatal(code, diag.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) < 3 || len(lines) > 12 {
		t.Fatal("bounded samples between header and end", len(lines))
	}
	var end struct {
		Kind, StopReason string
		Samples          int
		Qualified        bool
	}
	var value map[string]any
	if json.Unmarshal(lines[len(lines)-1], &value) != nil {
		t.Fatal("invalid JSON")
	}
	end.Kind = value["kind"].(string)
	end.StopReason = value["stop_reason"].(string)
	if end.Kind != "observer_end" || end.StopReason != "duration_reached" || value["samples"] != float64(len(lines)-2) || value["performance_comparison_qualified"] != false {
		t.Fatal(value)
	}
	if code := run(context.Background(), args, &out, &diag); code != 4 {
		t.Fatal("overwritten", code)
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(raw, again) {
		t.Fatal("existing evidence changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	out.Reset()
	if code := run(ctx, args[:len(args)-2], &out, &diag); code != 130 || !bytes.Contains(out.Bytes(), []byte(`"stop_reason":"cancelled"`)) {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestCLIHelpVersionAndSnapshot(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"snapshot"}} {
		var out, diag bytes.Buffer
		if code := run(context.Background(), args, &out, &diag); code != 0 || out.Len() == 0 {
			t.Fatal(args, code, diag.String())
		}
	}
}

func TestCLIBrokenStdoutReturnsIOFailure(t *testing.T) {
	if os.Getenv("LAB_OBSERVER_BROKEN_PIPE_CHILD") == "1" {
		os.Args = []string{"inferyard-observer", "snapshot"}
		main()
		return
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	defer writer.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestCLIBrokenStdoutReturnsIOFailure$")
	child.Env = append(os.Environ(), "LAB_OBSERVER_BROKEN_PIPE_CHILD=1")
	child.Stdout = writer
	var diag bytes.Buffer
	child.Stderr = &diag
	err = child.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 4 || !bytes.Contains(diag.Bytes(), []byte("output_failed")) {
		t.Fatal(err, diag.String())
	}
}
