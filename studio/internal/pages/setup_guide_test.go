package pages

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The first-launch card states the network condition, the write targets and the
// honest limitation on sizes and time before the user clicks anything.
func TestSetupGuideExplainsBeforeStarting(t *testing.T) {
	root := t.TempDir()
	settings := filepath.Join(root, "settings.json")
	if err := os.WriteFile(settings, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := core.New(context.Background(), core.Options{
		SettingsPath: settings,
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Shutdown(context.Background())

	s := testState()
	s.service = service
	s.ready = core.Readiness{CLI: core.InferyardProbe{Status: "not_found"}}
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, want := range []string{
		"这一步会做什么",
		"不会修改 shell 配置",
		"会写到哪里",
		"工作区：", "结果目录：", "模型目录：", "uv 工具：",
		"下载量与耗时",
		"无法可靠预估",
		"15 分钟超时，这不是整个准备过程的预计耗时",
		"准备环境",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if !tt.HasText("复用已找到的 uv：") && !tt.HasText("若本机没有 uv，会下载固定版本的独立 uv 工具。") {
		t.Fatal("neither reuse nor download is described", tt.Texts())
	}
	texts := strings.Join(tt.Texts(), " ")
	for _, banned := range []string{"MB", "剩余时间", "%"} {
		if strings.Contains(texts, banned) {
			t.Fatalf("invented estimate %q: %s", banned, texts)
		}
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("the guide created files: %v", entries)
	}
}

// Without a service (preview before fixtures load) the explanation still avoids
// invented paths.
func TestSetupGuideWithoutServiceKeepsExplanation(t *testing.T) {
	s := testState()
	s.ready = core.Readiness{CLI: core.InferyardProbe{Status: "not_found"}}
	tt := ui.NewTester(s.setupGuide, 1120, 2000)
	if !tt.HasText("无法可靠预估") || !tt.HasText("安装完成后显示实际的 CLI 路径。") {
		t.Fatal(tt.Texts())
	}
}
