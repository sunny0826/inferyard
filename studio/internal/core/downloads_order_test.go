package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"inferyard/studio/internal/inferyard"
)

// TestDownloadsNewestFirst pins the ordering the pages rely on: a later task
// comes first. The assertion uses task IDs rather than StartedAt strings,
// because RFC3339Nano drops trailing zeros and two nearby timestamps can
// compare out of order lexicographically.
func TestDownloadsNewestFirst(t *testing.T) {
	gate := make(chan struct{})
	done := make(chan struct{}, 2)
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		target := args[len(args)-1]
		_ = os.MkdirAll(target, 0o700)
		_ = os.WriteFile(filepath.Join(target, "model.gguf.part"), []byte("1234"), 0o600)
		done <- struct{}{}
		select {
		case <-ctx.Done():
			return result(130), ctx.Err()
		case <-gate:
		}
		return inferyard.Execution{ExitCode: 0, Stdout: []byte(`{"command":"model","status":"complete","completeness":"complete","details":{"bytes":4,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`)}, nil
	})
	root := t.TempDir()
	s.settings.ModelsRoot = &root
	source := "https://huggingface.co/o/r/blob/0123456789012345678901234567890123456789/model.gguf"

	var created []DownloadTask
	for range 2 {
		task, e := s.CreateModelDownload(CreateDownloadRequest{SourceUrl: source, OutName: uniqueName(t)}, "")
		if e != nil {
			t.Fatal(e)
		}
		created = append(created, task)
		<-done
	}
	tasks := s.Downloads()
	if len(tasks) != 2 {
		t.Fatalf("downloads = %d", len(tasks))
	}
	first, second := created[0], created[1]
	if tasks[0].TaskId != second.TaskId || tasks[1].TaskId != first.TaskId {
		t.Fatalf("downloads are not newest-first: got %q then %q, want %q then %q",
			tasks[0].TaskId, tasks[1].TaskId, second.TaskId, first.TaskId)
	}
}

// uniqueName keeps each task off the occupied-name path without sleeping.
func uniqueName(t *testing.T) string {
	t.Helper()
	return "task-" + time.Now().Format("150405.000000000")
}
