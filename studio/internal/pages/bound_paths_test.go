package pages

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestBoundPathsSettingsView(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &core.BoundPaths{CLI: filepath.Join(root, "cli"), Workspace: filepath.Join(root, "workspace"), Results: filepath.Join(root, "workspace", "results"), Models: filepath.Join(root, "assets", "models"), Engines: filepath.Join(root, "assets", "engines"), Settings: filepath.Join(root, "settings.json"), Source: "checkout fixture", WindowTitle: "开发环境 · test"}
	p.SourceLabel = "feature/test · abcdef01 · 已修改"
	service, err := core.New(context.Background(), core.Options{Paths: p, ValidateCLI: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())
	s := testState()
	s.service = service
	s.loadSettings(service.Settings())
	s.settingsLoaded = true
	tt := ui.NewTester(func(c *ui.Context) { s.settingsView(c) }, 1000, 1000)
	for _, text := range []string{"开发环境 · 固定路径", p.CLI, p.Workspace, p.Results, p.Models, p.Engines, p.Source, "保存下载目录名"} {
		if !tt.HasText(text) {
			t.Fatal(text, tt.Texts())
		}
	}
	for _, label := range []string{"CLI", "工作区目录", "结果目录", "模型目录"} {
		if len(designFieldInputs(tt, label)) != 0 {
			t.Fatal("bound path is editable", label)
		}
	}
	for _, label := range []string{"模型下载目录名", "引擎下载目录名"} {
		if len(designFieldInputs(tt, label)) == 0 {
			t.Fatal("download name missing", label)
		}
	}
	if _, err := os.Stat(p.Settings); !os.IsNotExist(err) {
		t.Fatal("render wrote settings", err)
	}
	nav := ui.NewTester(func(c *ui.Context) { s.leftNav(c) }, 205, 800)
	if !nav.HasText("开发环境") || !nav.HasText(p.SourceLabel) {
		t.Fatal("development source is not visible", nav.Texts())
	}
}
