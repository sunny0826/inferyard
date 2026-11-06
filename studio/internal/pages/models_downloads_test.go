package pages

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDownloadsPageMigrationEmptyAndValidation(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 2400)
	for _, kind := range []string{"model", "engine"} {
		t.Run(kind, func(t *testing.T) {
			source := "http://example.com/model.gguf"
			label := "下载模型"
			if kind == "model" {
				s.modelURL = source
			} else {
				s.engineURL = source
				label = "下载引擎"
			}
			tt.Frame()
			click(t, tt, label)
			tt.Frame()
			if s.failure == nil || s.busy || s.selected != "downloads" || len(s.downloads) != 0 || !tt.HasText("请输入完整的 https URL。") {
				t.Fatal("non-https submission did not stay on page", tt.Texts())
			}
			if kind == "model" && s.modelURL != source || kind == "engine" && s.engineURL != source {
				t.Fatal("edited URL lost")
			}
		})
	}
	s.modelURL = core.DefaultModelURL()
	s.failure = nil
	tt.Frame()
	click(t, tt, "需要凭据时")
	tt.Advance(250 * time.Millisecond)
	tt.Frame()
	click(t, tt, "凭据环境变量名（可选）")
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("secret-value")
	tt.Frame()
	if s.tokenEnv != "secret-value" {
		t.Fatalf("credential field = %q model=%q engine=%q", s.tokenEnv, s.modelURL, s.engineURL)
	}
	click(t, tt, "下载模型")
	tt.Frame()
	if s.failure == nil || s.failure.Message != "只接受环境变量名，请勿输入明文 token。" || s.busy || len(s.downloads) != 0 {
		message := ""
		if s.failure != nil {
			message = s.failure.Message
		}
		t.Fatalf("credential value accepted: message=%q busy=%v downloads=%d", message, s.busy, len(s.downloads))
	}
}

func TestModelsUnifiedDownloadMeasurements(t *testing.T) {
	for _, task := range []core.DownloadTask{
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "downloading", ProgressReason: ptr("engine_transfer_progress_unavailable")},
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
	} {
		t.Run(task.Kind, func(t *testing.T) {
			s := testState()
			s.downloads = []core.DownloadTask{task}
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.downloadsView(c) }) }, 1000, 1200)
			for _, want := range []string{task.OutName, "已观测", "速率", "总量", "总量未知", "取消下载"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
			if task.Kind == "engine" {
				if !tt.HasText("已下载 缺测 · 速率 缺测 · 归档获取接口未提供字节读数") || tt.HasText("已下载 0 bytes") {
					t.Fatal(tt.Texts())
				}
			} else if !tt.HasText("1.50 MiB") || !tt.HasText("2.0 KiB/s") || !tt.HasText("速率 2,048 bytes/s") || !tt.HasText("已下载 1572864 bytes") {
				t.Fatal(tt.Texts())
			}
			enabled := false
			for i := 0; i < 12; i++ {
				tt.Key(0, ui.KeyTab)
				if tt.Focused("取消下载") {
					enabled = true
					break
				}
			}
			if !enabled {
				t.Fatal("cancel is not keyboard accessible")
			}
			for _, state := range []string{"failed", "cancelled", "done"} {
				s.downloads[0].State = state
				tt.Frame()
				if _, ok := tt.Find("下载任务活动"); ok {
					t.Fatal("terminal activity progress")
				}
				if tt.HasText("取消下载") {
					t.Fatal("terminal cancel")
				}
				if state == "failed" && !tt.HasText("重新创建下载任务") {
					t.Fatal("failed task must offer recreate", tt.Texts())
				}
				if state != "failed" && tt.HasText("重新创建下载任务") {
					t.Fatal("recreate is only for failed tasks", tt.Texts())
				}
				if state != "done" && !tt.HasText("输出目录保留；再次下载自动使用新目录名。") {
					t.Fatal(tt.Texts())
				}
			}
		})
	}
}

func TestModelsAdoptCompletedEngine(t *testing.T) {
	for _, initial := range []string{"", "existing · llama-server"} {
		s := testState()
		s.engine, s.registry = initial, modelRegistryFixture()
		s.checks = eligibleChecks()
		s.adoptDownloadedEngine()
		want := initial
		if want == "" {
			want = "engine-2 · llama-server"
		}
		if s.engine != want {
			t.Fatal(s.engine, want)
		}
	}
	// Without a verdict nothing is auto-selected while the CLI can verify.
	s := testState()
	s.registry = modelRegistryFixture()
	s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{
		"engine-2 · llama-server": {State: core.EngineCheckChecking},
	}}
	s.adoptDownloadedEngine()
	if s.engine != "" {
		t.Fatal("adopted an unverified engine", s.engine)
	}
	// An outdated CLI degrades to the registry default (config create decides).
	s.checks = core.EngineCheckOverview{Supported: false}
	s.adoptDownloadedEngine()
	if s.engine != s.registry.DefaultEngine {
		t.Fatal(s.engine)
	}
	// Adoption never overwrites an explicit selection.
	s.engine = "release · inference-engine"
	s.checks = eligibleChecks()
	s.adoptDownloadedEngine()
	if s.engine != "release · inference-engine" {
		t.Fatal("overwrote selection")
	}
}

func modelUINodes(tt *ui.Tester, field, value string) []reflect.Value {
	root := reflect.ValueOf(tt).Elem().FieldByName("rt").Elem().FieldByName("c").FieldByName("root")
	var matches []reflect.Value
	var visit func(reflect.Value)
	visit = func(node reflect.Value) {
		if node.IsNil() {
			return
		}
		n := node.Elem()
		if n.FieldByName(field).String() == value {
			matches = append(matches, n)
		}
		for child := n.FieldByName("first"); !child.IsNil(); child = child.Elem().FieldByName("next") {
			visit(child)
		}
	}
	visit(root)
	return matches
}
