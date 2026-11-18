//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func preparePreview(t *testing.T) (*previewSession, *ui.Tester) {
	t.Helper()
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1120, 2200)
	click(t, tt, "准备环境")
	if !p.state.busy || p.state.ready.Workspace.Initialized {
		t.Fatal("environment preparation skipped its pending state")
	}
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.step() != 1 || !p.state.busy {
		t.Fatal("device-check stage was skipped")
	}
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if p.state.busy || !p.state.preflightReady() || !tt.HasText("开始测评") {
		t.Fatal("environment preparation did not unlock benchmark", tt.Texts())
	}
	return p, tt
}
