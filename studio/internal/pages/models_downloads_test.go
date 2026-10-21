package pages

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDownloadsPageMigration(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	s.downloads = []core.DownloadTask{
		{TaskId: "model", Kind: "model", OutName: "model-fixture", State: "downloading", DownloadedBytes: 1572864, BytesPerSecond: ptr(2048.0)},
		{TaskId: "engine", Kind: "engine", OutName: "engine-fixture", State: "failed", SourceUrl: "https://huggingface.co/owner/repo/blob/0123456789abcdef0123456789abcdef01234567/file.tar.gz", Error: core.Err("download_failed")},
	}
	tt := ui.NewTester(s.view, 1120, 2600)
	for _, want := range []string{"下载任务", "新建下载任务", "model-fixture", "engine-fixture", "重新创建下载任务", "下载模型", "目录名取自设置，占用自动加序号；实际目录名见任务详情。", "打开设置"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	engine, _ := tt.Find("engine-fixture")
	model, _ := tt.Find("model-fixture")
	if engine.Y >= model.Y {
		t.Fatal("engine task must precede model task")
	}
	if s.downloads[0].TaskId != "model" {
		t.Fatal("render reordered source tasks")
	}
	click(t, tt, "需要凭据时")
	if !tt.HasText("凭据环境变量名") {
		t.Fatal(tt.Texts())
	}
	if len(designFieldInputs(tt, "新目录名")) != 0 {
		t.Fatal("per-task name input remains")
	}
	// The models page no longer carries any download UI.
	s.selected = "models"
	tt.Frame()
	if tt.HasText("新建下载任务") || tt.HasText("取消下载") || tt.HasText("下载模型") {
		t.Fatal("download UI still on models page", tt.Texts())
	}
	for _, n := range modelUINodes(tt, "text", "下载任务") {
		if n.FieldByName("role").Uint() == uint64(ui.RoleHeading) {
			t.Fatal("download task card still on models page", tt.Texts())
		}
	}
}

func TestDownloadsPageMigrationEmptyAndValidation(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 2400)
	if len(modelUINodes(tt, "text", "下载任务")) != 0 || tt.HasText("凭据环境变量名") {
		t.Fatal(tt.Texts())
	}
	if s.engineURL != core.DefaultEngineURL() || s.modelURL != core.DefaultModelURL() {
		t.Fatal("missing defaults")
	}
	engine, engineOK := tt.Find("下载引擎")
	model, modelOK := tt.Find("下载模型")
	if !engineOK || !modelOK || engine.Y >= model.Y {
		t.Fatal("engine form must precede model form")
	}
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
			amount, _ := tt.Find("已观测")
			rate, _ := tt.Find("速率")
			total, _ := tt.Find("总量")
			if amount.Y != rate.Y || rate.Y != total.Y || amount.X >= rate.X || rate.X >= total.X {
				t.Fatal("metrics are not three columns")
			}
			activity, ok := tt.Find("下载任务活动")
			if !ok || math.Abs(float64(activity.H-4)) > .001 {
				t.Fatal("missing activity progress", activity)
			}
			nodes := modelUINodes(tt, "label", "下载任务活动")
			if len(nodes) != 1 || nodes[0].FieldByName("role").Uint() != uint64(ui.RoleProgress) ||
				!nodes[0].FieldByName("hasRange").Bool() || !(nodes[0].FieldByName("accRange").Index(2).Float() < 0) {
				t.Fatal("activity must be an indeterminate native progress component")
			}
			spinners := modelUINodes(tt, "label", "下载中")
			if len(spinners) != 1 || spinners[0].FieldByName("role").Uint() != uint64(ui.RoleProgress) || !spinners[0].FieldByName("hasRange").Bool() || spinners[0].FieldByName("accRange").Index(2).Float() >= 0 {
				t.Fatal("downloading task must show an indeterminate activity indicator")
			}
			for label, reason := range map[string]string{"总量": "总量未知", "下载中": "任务正在下载，总量未知。"} {
				nodes := modelUINodes(tt, "description", reason)
				if len(nodes) != 1 {
					t.Fatal("missing accessible tooltip", label)
				}
			}

			for _, want := range []string{"实际目录名：", "文件：", "1 MiB = 1,048,576 bytes", "1 GiB = 1,073,741,824 bytes", "总量未知不显示百分比"} {
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
