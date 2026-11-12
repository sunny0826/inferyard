package pages

import (
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadNamesSettingsView(t *testing.T) {
	s := testState()
	s.selected = "settings"
	s.modelName, s.engineName = "...", "a/b"
	tt := ui.NewTester(s.view, 1120, 800)
	// Drive the real long page by scrolling until its action can be clicked.
	var clickErr error
	for range 12 {
		tt.Scroll(700, 500, 0, 200)
		clickErr = tt.Click("恢复默认值")
		if clickErr == nil {
			break
		}
	}
	if clickErr != nil {
		t.Fatal("cannot restore names from the settings page", clickErr)
	}
	if s.modelName != "model" || s.engineName != "engine" {
		t.Fatal(s.modelName, s.engineName)
	}
}

func TestBoundOutputNameExhaustion(t *testing.T) {
	root := t.TempDir()
	for n := 1; n <= 1000; n++ {
		if err := os.MkdirAll(filepath.Join(root, "bound", fmt.Sprintf("zh-core-%d", n)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	_, err := core.NextOutput(root, "bound", "zh-core")
	var api *core.ApiError
	if !errors.As(err, &api) || api.Code != "output_name_exhausted" {
		t.Fatal(err)
	}
	s := testState()
	s.progress = &inferyard.WorkspaceProgress{Workspace: root, Bundles: []inferyard.BundleProgress{{Bundle: "zh-core", Phase: "run_missing"}}}
	s.executeBinding(s.bindCommand("42", "http://127.0.0.1:8080"))
	if s.failure == nil || s.failure.Code != "output_name_exhausted" || s.busy {
		t.Fatal("binding did not report exhausted names", s.failure)
	}
	last, resolveErr := core.Resolve(root, filepath.Join("bound", "zh-core-1000"))
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err := os.Remove(last); err != nil {
		t.Fatal(err)
	}
	if path, e := core.NextOutput(root, "bound", "zh-core"); e != nil || path != last {
		t.Fatal(path, e)
	}
}
