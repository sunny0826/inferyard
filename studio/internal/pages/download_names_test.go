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
	s.modelName, s.engineName = "...", "a/b"
	tt := ui.NewTester(func(c *ui.Context) { s.downloadNamesView(c) }, 1000, 800)
	for _, label := range []string{"模型下载目录名", "引擎下载目录名"} {
		inputs := designFieldInputs(tt, label)
		if len(inputs) == 0 || !inputs[0].FieldByName("invalid").Bool() {
			t.Fatal(label)
		}
	}
	click(t, tt, "恢复默认值")
	if s.modelName != "model" || s.engineName != "engine" {
		t.Fatal(s.modelName, s.engineName)
	}
	tt.Frame()
	if !tt.HasText("实际名称显示在下载任务详情中") {
		t.Fatal(tt.Texts())
	}
}

func TestBoundOutputNameExhaustion(t *testing.T) {
	root := t.TempDir()
	for n := 1; n <= 1000; n++ {
		if err := os.MkdirAll(filepath.Join(root, "bound", fmt.Sprintf("zh-core-%d", n)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	_, err := newBoundOutput(root, "zh-core")
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
	last := filepath.Join(root, "bound", "zh-core-1000")
	if err := os.Remove(last); err != nil {
		t.Fatal(err)
	}
	if path, err := newBoundOutput(root, "zh-core"); err != nil || path != last {
		t.Fatal(path, err)
	}
}
