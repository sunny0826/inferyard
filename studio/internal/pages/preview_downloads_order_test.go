//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

// TestPreviewDownloadsStayNewestFirst aligns the simulated list with
// Service.Downloads: a later task comes first, so the guide can tell a newer
// retry from an older success of the same source.
func TestPreviewDownloadsStayNewestFirst(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "模型 · 可用组合"))
	p.state.downloads = nil
	p.startDownload(previewDownload{kind: "model", source: "https://example.test/model.gguf"})
	p.advance(time.Second)
	p.startDownload(previewDownload{kind: "model", source: "https://example.test/model.gguf"})

	if len(p.state.downloads) != 2 {
		t.Fatalf("downloads = %d", len(p.state.downloads))
	}
	newer, older := p.state.downloads[0], p.state.downloads[1]
	if newer.StartedAt <= older.StartedAt {
		t.Fatalf("later task is not first: %q then %q", newer.StartedAt, older.StartedAt)
	}

	// The older task fails after the newer one started: the newer task resolves
	// it, and the failure does not reopen the retry prompt.
	p.state.downloads[1].State = "failed"
	if got := p.state.unresolvedDownloadFailures(); got != 0 {
		t.Fatalf("newer task did not resolve the older failure: %d", got)
	}

	// The reverse order keeps the failure: an older success cannot hide it.
	done := core.DownloadTask{TaskId: "older", Kind: "model", State: "done", SourceUrl: "https://example.test/other.gguf", StartedAt: "2026-10-17T00:00:00Z"}
	failed := core.DownloadTask{TaskId: "later", Kind: "model", State: "failed", SourceUrl: done.SourceUrl, StartedAt: "2026-10-17T00:01:00Z"}
	p.state.downloads = []core.DownloadTask{failed, done}
	if got := p.state.unresolvedDownloadFailures(); got != 1 {
		t.Fatalf("older success masked the later failure: %d", got)
	}
}
