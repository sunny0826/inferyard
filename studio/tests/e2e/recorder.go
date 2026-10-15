package e2e

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

// Record is one CLI invocation captured at the Executor boundary. It is the
// blame-attribution artifact: argv is Studio's output, exit code and streams
// are the CLI's output, and both sides are pinned to files.
type Record struct {
	Seq          int      `json:"seq"`
	Argv         []string `json:"argv"`
	ExitCode     int      `json:"exit_code"`
	Status       string   `json:"status"`
	DurationMS   int64    `json:"duration_ms"`
	StdoutFile   string   `json:"stdout_file"`
	StderrFile   string   `json:"stderr_file"`
	StdoutSHA256 string   `json:"stdout_sha256"`
	StderrSHA256 string   `json:"stderr_sha256"`
}

// Recorder wraps a core Executor and appends one JSONL record per call.
type Recorder struct {
	mu   sync.Mutex
	dir  string
	seq  int
	next func(context.Context, string, ...string) (inferyard.Execution, error)
}

func NewRecorder(t *testing.T, dir string, next func(context.Context, string, ...string) (inferyard.Execution, error)) *Recorder {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return &Recorder{dir: dir, next: next}
}

// Exec is the executor middleware; it never alters the wrapped result.
func (r *Recorder) Exec(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
	started := time.Now()
	out, err := r.next(ctx, path, args...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	stem := filepath.Join(r.dir, padSeq(r.seq))
	stdoutFile := stem + ".stdout"
	stderrFile := stem + ".stderr"
	_ = os.WriteFile(stdoutFile, out.Stdout, 0o600)
	_ = os.WriteFile(stderrFile, out.Stderr, 0o600)
	record := Record{
		Seq: r.seq, Argv: append([]string{path}, args...),
		ExitCode: out.ExitCode, Status: string(out.Status),
		DurationMS: time.Since(started).Milliseconds(),
		StdoutFile: stdoutFile, StderrFile: stderrFile,
		StdoutSHA256: hexSHA256(out.Stdout), StderrSHA256: hexSHA256(out.Stderr),
	}
	line, _ := json.Marshal(record)
	f, openErr := os.OpenFile(filepath.Join(r.dir, "transcript.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr == nil {
		_, _ = f.Write(append(line, '\n'))
		_ = f.Close()
	}
	return out, err
}

// Records returns every recorded call in order.
func (r *Recorder) Records() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(r.dir, "transcript.jsonl"))
	if err != nil {
		return nil
	}
	var records []Record
	for _, line := range splitLines(data) {
		var record Record
		if json.Unmarshal(line, &record) == nil {
			records = append(records, record)
		}
	}
	return records
}

func padSeq(seq int) string {
	return fmt.Sprintf("%04d", seq)
}

func hexSHA256(data []byte) string {
	return hex.EncodeToString(sha256Of(data))
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
