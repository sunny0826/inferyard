package pages

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestFirstUseMissingAssetsOffersNextStep(t *testing.T) {
	for _, reason := range []string{"", "no_local_model"} {
		t.Run(reason, func(t *testing.T) {
			s := testState()
			s.progress = &inferyard.WorkspaceProgress{
				NextAction:      "prepare_candidate",
				DevicePreflight: inferyard.ProgressPreflight{Status: "ok"},
				Bundles: []inferyard.BundleProgress{
					{Bundle: "zh-core", Phase: "candidate_missing"},
					{Bundle: "zh-svg-pelican", Phase: "candidate_missing"},
				},
			}
			if reason != "" {
				for i := range s.progress.Bundles {
					s.progress.Bundles[i].BlockedReason = ptr(reason)
				}
			}
			tt := ui.NewTester(s.view, 1120, 1000)
			for _, label := range []string{"准备模型与引擎", "模型 · 待下载", "引擎 · 待下载", "到下载页"} {
				if !tt.HasText(label) {
					t.Fatalf("missing %q: %v", label, tt.Texts())
				}
			}
			for _, label := range []string{"尚未识别的原因码", "工作区进度", "运行 benchmark", "使用 CLI 推荐模型"} {
				if tt.HasText(label) {
					t.Fatalf("unprepared workspace exposed %q: %v", label, tt.Texts())
				}
			}
			click(t, tt, "到下载页")
			if s.selected != "downloads" || !tt.HasText("返回任务") {
				t.Fatal("download route has no return path", tt.Texts())
			}
		})
	}
}

func TestDownloadHeaderStableAcrossRefresh(t *testing.T) {
	s := testState()
	s.downloads = []core.DownloadTask{{TaskId: "model", Kind: "model", State: "downloading"}}
	tt := ui.NewTester(s.cornerStatus, 1120, 200)
	before := slices.Clone(tt.Texts())
	for _, refreshing := range []bool{true, false, true, false} {
		s.refreshing = refreshing
		tt.Frame()
		if !slices.Equal(before, tt.Texts()) || tt.HasText("执行中") {
			t.Fatalf("background refresh changed header: before=%v after=%v", before, tt.Texts())
		}
	}
	if !tt.HasText("下载中 1 项") {
		t.Fatal("download lifecycle absent from header", tt.Texts())
	}
}

func TestLiveBundleDoesNotShowOfflineResumeWarning(t *testing.T) {
	for _, state := range []string{"starting", "running", "cancelling"} {
		t.Run(state, func(t *testing.T) {
			s := testState()
			s.progress.Bundles = []inferyard.BundleProgress{
				{Bundle: "zh-core", Phase: "run_incomplete", BlockedReason: ptr("single_run_resume_not_supported")},
				{Bundle: "zh-svg-pelican", Phase: "run_incomplete", BlockedReason: ptr("duration_resume_requires_new_window")},
			}
			s.tasks = []core.RunTask{{TaskId: "live", Bundle: ptr("zh-core"), State: state, Queued: state == "starting"}}
			tt := ui.NewTester(s.conclusionView, 1120, 1000)
			if tt.HasText("单次运行不支持断点续跑") || tt.HasText("zh-core · 运行未完成") {
				t.Fatal("offline recovery guidance leaked into live task", tt.Texts())
			}
			if !tt.HasText("zh-core · "+taskLabel(s.tasks[0])) || !tt.HasText(human("duration_resume_requires_new_window")) {
				t.Fatal("live status or unrelated recovery guidance lost", tt.Texts())
			}
			s.tasks[0].State = "cancelled"
			tt.Frame()
			if !tt.HasText(human("single_run_resume_not_supported")) {
				t.Fatal("terminal recovery guidance hidden", tt.Texts())
			}
		})
	}
}

func TestOnlyMissingModelIsPreparationGuidance(t *testing.T) {
	for _, tc := range []struct {
		exit    int
		reasons []string
		want    bool
	}{
		{2, []string{"no_local_model"}, true},
		{4, []string{"no_local_model"}, false},
		{2, []string{"no_local_model", "insufficient_host_memory"}, false},
		{2, []string{"insufficient_host_memory"}, false},
	} {
		if got := missingModelResult(core.WizardResult{ExitCode: tc.exit, Limitations: tc.reasons}); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestSingleAvailableModelDefaultPreservesChoice(t *testing.T) {
	s := testState()
	s.registry.Models = []core.ModelAsset{{Name: "broken"}, {Name: "ready.gguf", Available: true}}
	s.adoptOnlyModel()
	if s.model != "ready.gguf" {
		t.Fatal(s.model)
	}
	s.registry.Models = append(s.registry.Models, core.ModelAsset{Name: "other.gguf", Available: true})
	s.adoptOnlyModel()
	if s.model != "ready.gguf" {
		t.Fatal("changed explicit selection", s.model)
	}
	s.model = ""
	s.adoptOnlyModel()
	if s.model != "" {
		t.Fatal("guessed between multiple models", s.model)
	}
}

func TestResourceGuideKeepsOtherWorkspaceBlockers(t *testing.T) {
	s := testState()
	s.progress.NextAction = "prepare_candidate"
	s.progress.Bundles = []inferyard.BundleProgress{
		{Bundle: "zh-core", Phase: "candidate_missing", BlockedReason: ptr("unsupported_platform")},
		{Bundle: "zh-svg-pelican", Phase: "candidate_missing", BlockedReason: ptr("unsupported_platform")},
	}
	tt := ui.NewTester(s.view, 1120, 1000)
	count := 0
	for _, text := range tt.Texts() {
		if text == human("unsupported_platform") {
			count++
		}
	}
	if !tt.HasText("准备模型与引擎") || count != 1 {
		t.Fatal("resource guide hid or duplicated an independent blocker", tt.Texts())
	}
}
