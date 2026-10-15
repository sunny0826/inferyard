package e2e

import (
	"context"
	"errors"
	"os"
	"testing"

	"inferyard/studio/internal/inferyard"
)

// TestRecorderCapturesCalls verifies the boundary transcript pins argv, exit
// status, stream files and hashes without altering the wrapped result.
func TestRecorderCapturesCalls(t *testing.T) {
	dir := t.TempDir()
	fake := func(_ context.Context, _ string, _ ...string) (inferyard.Execution, error) {
		return inferyard.Execution{
			Stdout: []byte(`{"command":"status"}`), Stderr: []byte("warn"),
			ExitCode: 2, Status: inferyard.Blocked,
		}, nil
	}
	recorder := NewRecorder(t, dir, fake)
	for i := 0; i < 2; i++ {
		out, err := recorder.Exec(context.Background(), "/cli/inferyard", "status", "--json")
		if err != nil || out.ExitCode != 2 || out.Status != inferyard.Blocked {
			t.Fatal("wrapped result altered", out, err)
		}
	}
	records := recorder.Records()
	if len(records) != 2 {
		t.Fatal("records", records)
	}
	for i, record := range records {
		if record.Seq != i+1 || record.ExitCode != 2 || record.Status != "blocked" {
			t.Fatal("record mismatch", record)
		}
		if record.Argv[0] != "/cli/inferyard" || record.Argv[1] != "status" {
			t.Fatal("argv mismatch", record.Argv)
		}
		stdout, err := os.ReadFile(record.StdoutFile)
		if err != nil || string(stdout) != `{"command":"status"}` {
			t.Fatal("stdout file mismatch", record.StdoutFile)
		}
		if record.StdoutSHA256 != hexSHA256(stdout) {
			t.Fatal("stdout hash mismatch")
		}
		stderr, err := os.ReadFile(record.StderrFile)
		if err != nil || string(stderr) != "warn" {
			t.Fatal("stderr file mismatch")
		}
	}
}

// TestRecorderPropagatesErrors ensures launch failures stay visible.
func TestRecorderPropagatesErrors(t *testing.T) {
	boom := errors.New("launch failed")
	recorder := NewRecorder(t, t.TempDir(), func(context.Context, string, ...string) (inferyard.Execution, error) {
		return inferyard.Execution{ExitCode: -1, Status: inferyard.Unknown}, boom
	})
	_, err := recorder.Exec(context.Background(), "/missing", "status")
	if !errors.Is(err, boom) {
		t.Fatal("error not propagated", err)
	}
	if len(recorder.Records()) != 1 {
		t.Fatal("failed calls must still be recorded")
	}
}
