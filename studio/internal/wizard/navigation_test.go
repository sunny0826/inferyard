package wizard

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
	"testing"
)

func TestOpenMainUsesNativePageFactory(t *testing.T) {
	m := &Manager{}
	page := ""
	calls := 0
	m.SetMainFactory(func(value string) { page = value; calls++ })
	w := viewState{loaded: true, openMain: m.openMain}
	tt := ui.NewTester(w.view, 1000, 740)
	if e := tt.Click("手动指定路径"); e != nil {
		t.Fatal(e)
	}
	if page != "settings" || calls != 1 {
		t.Fatalf("settings: %q %d", page, calls)
	}
	w.progress = &inferyard.WorkspaceProgress{DefaultScopeComplete: true}
	tt.Frame()
	if e := tt.Click("在主窗口打开报告"); e != nil {
		t.Fatal(e)
	}
	if page != "reports" || calls != 2 {
		t.Fatalf("reports: %q %d", page, calls)
	}
}
