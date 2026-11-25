package pages

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestP4ShellOptionsLeftToggleTrailing(t *testing.T) {
	opts := pageShellOptions("InferYard Studio", "test-key", func(*ui.Context) {}, func(*ui.Context) {})
	if !opts.LeftToggleTrailing {
		t.Fatal("expected pageShellOptions to enable LeftToggleTrailing")
	}
}

func TestP4LibraryModelEvidenceAndClickOpensReport(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "library"
	s.libraryTab = 0
	best := core.EngineSpeedRecord{
		Engine:          "llama.cpp",
		TokensPerSecond: 68.4,
		ReportRef:       "rep-best",
		Exclusivity:     core.ExclusivityExclusive,
	}
	second := core.EngineSpeedRecord{
		Engine:          "mlx-lm",
		TokensPerSecond: 54.2,
		ReportRef:       "rep-mlx",
		Exclusivity:     core.ExclusivityExclusive,
	}
	s.modelEvidence = map[string]core.ModelEvidenceSummary{
		core.NormalizeModelEvidenceKey("Qwen3-8B-Q4.gguf"): {
			Model:    "Qwen3-8B-Q4.gguf",
			Best:     &best,
			ByEngine: []core.EngineSpeedRecord{best, second},
		},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "实测最佳：llama.cpp 68.4 tok/s · mlx-lm 54.2 tok/s") {
		t.Fatalf("expected model evidence with second engine comparison, got:\n%s", texts)
	}
	if !strings.Contains(texts, "尚未测评") {
		t.Fatalf("expected untested badge for second model, got:\n%s", texts)
	}

	click(t, tt, "打开 Qwen3-8B-Q4.gguf · GGUF 的测评报告")
	tt.Frame()
	if s.selected != "reports" || s.openedReport == nil || s.openedReport.reference != "rep-best" {
		t.Fatalf("expected clicking evidence chip to open report rep-best, got selected=%q opened=%+v", s.selected, s.openedReport)
	}
}

func TestP4ServiceCardMeasuredVsEstimatedMemoryAndSpeed(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	measuredGB := 18.2
	id1 := core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default")
	id2 := core.ServiceIDFor("Llama-3-8B.gguf", "llama-cpp-default")
	id3 := core.ServiceIDFor("Unknown-Mem.gguf", "llama-cpp-default")
	s.managedServices = []core.EngineState{
		{
			ServiceID:         id1,
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			Family:            "llama-cpp",
			EngineName:        "llama-cpp-default",
			ModelName:         "Qwen3-8B-Q4.gguf",
			Port:              8080,
			EstimatedMemoryGB: 19.0,
			RuntimeMemoryGB:   &measuredGB,
			ContextSize:       32768,
			Endpoint:          "http://127.0.0.1:8080",
		},
		{
			ServiceID:         id2,
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			Family:            "llama-cpp",
			EngineName:        "llama-cpp-default",
			ModelName:         "Llama-3-8B.gguf",
			Port:              8081,
			EstimatedMemoryGB: 6.2,
			ContextSize:       8192,
			Endpoint:          "http://127.0.0.1:8081",
		},
		{
			ServiceID:   id3,
			State:       "running",
			Origin:      core.EngineOriginStandalone,
			Family:      "llama-cpp",
			EngineName:  "llama-cpp-default",
			ModelName:   "Unknown-Mem.gguf",
			Port:        8082,
			ContextSize: 4096,
			Endpoint:    "http://127.0.0.1:8082",
		},
	}
	s.expandedServices = map[string]bool{id1: true, id2: true, id3: true}
	best := core.EngineSpeedRecord{
		Engine:          "llama.cpp",
		TokensPerSecond: 68.4,
		ReportRef:       "rep-svc",
	}
	s.modelEvidence = map[string]core.ModelEvidenceSummary{
		core.NormalizeModelEvidenceKey("Qwen3-8B-Q4.gguf"): {
			Model:    "Qwen3-8B-Q4.gguf",
			Best:     &best,
			ByEngine: []core.EngineSpeedRecord{best},
		},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"18.2 GB · 实测", "6.2 GB · 预估", "68.4 tok/s", "查看报告 ›"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected service card to contain %q, got:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "0.0 GB") {
		t.Fatalf("must not fabricate 0.0 GB for missing memory, got:\n%s", texts)
	}

	click(t, tt, "查看 Qwen3-8B-Q4.gguf 的测评报告")
	tt.Frame()
	if s.selected != "reports" || s.openedReport == nil || s.openedReport.reference != "rep-svc" {
		t.Fatalf("expected clicking service report link to open rep-svc, got selected=%q opened=%+v", s.selected, s.openedReport)
	}
}

func TestP4PreBenchmarkExternalConnDialogBranches(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	// 48 GiB unified memory -> 36 GB budget; model ~4.8 GB -> enough for dedicated instance.
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":51539607552}}`)
	svc := core.EngineState{
		ServiceID:         core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default"),
		State:             "running",
		Origin:            core.EngineOriginStandalone,
		EngineName:        "llama-cpp-default",
		ModelName:         "Qwen3-8B-Q4.gguf",
		Port:              8080,
		EstimatedMemoryGB: 5.5,
		ContextSize:       8192,
		Endpoint:          "http://127.0.0.1:8080",
	}
	s.managedServices = []core.EngineState{svc}
	s.engineState = svc

	// 1) exclusive & unknown bypass dialog directly.
	for _, status := range []string{core.ExclusivityExclusive, core.ExclusivityUnknown} {
		s.selected = "services"
		s.benchFlowOpen = false
		s.extCheckDialog = extConnCheckDialogState{}
		s.probeServiceConnFunc = func(context.Context, string) core.ExternalConnProbe {
			return core.ExternalConnProbe{Status: status, Port: 8080}
		}
		s.startBenchmarkForManagedService(svc)
		if s.extCheckDialog.open {
			t.Fatalf("expected probe status %q to bypass external-connection dialog", status)
		}
		if s.selected != "benchmarks" || !s.benchFlowOpen {
			t.Fatalf("expected probe status %q to open benchmarks flow, got selected=%q flow=%v", status, s.selected, s.benchFlowOpen)
		}
	}

	// 2) external + sufficient memory -> primary button is 另起测评专用实例.
	s.selected = "services"
	s.benchFlowOpen = false
	s.probeServiceConnFunc = func(context.Context, string) core.ExternalConnProbe {
		return core.ExternalConnProbe{
			Status:  core.ExclusivityExternal,
			Clients: []string{"Cherry Studio (PID 4182)"},
			Count:   1,
			Port:    8080,
		}
	}
	s.startBenchmarkForManagedService(svc)
	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"检测到外部连接", "8080", "Cherry Studio (PID 4182)", "另起测评专用实例", "仍然测评（报告标注）", "取消"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected sufficient-memory external dialog to contain %q, got:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "正在被调用") {
		t.Fatalf("dialog must never claim '正在被调用', got:\n%s", texts)
	}

	// 3) external + insufficient memory -> no 另起测评专用实例, only 取消 + 仍然测评（报告标注）.
	s.extCheckDialog = extConnCheckDialogState{}
	// 16 GiB unified memory -> 12 GB budget; service uses 9.5 GB, model needs ~9.5 GB -> insufficient.
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":17179869184}}`)
	bigSize := int64(8 * (1 << 30))
	s.registry.Models[0].SizeBytes = &bigSize
	svc.EstimatedMemoryGB = 9.5
	s.managedServices = []core.EngineState{svc}
	s.startBenchmarkForManagedService(svc)
	tt.Frame()
	tightTexts := strings.Join(tt.Texts(), "\n")
	if _, ok := tt.AccessByRole(ui.RoleButton, "另起测评专用实例"); ok {
		t.Fatalf("expected dedicated-instance button hidden when memory insufficient, got:\n%s", tightTexts)
	}
	for _, want := range []string{"检测到外部连接", "仍然测评（报告标注）", "取消"} {
		if !strings.Contains(tightTexts, want) {
			t.Fatalf("expected tight-memory external dialog to contain %q, got:\n%s", want, tightTexts)
		}
	}
	click(t, tt, "仍然测评（报告标注）")
	tt.Frame()
	if s.extCheckDialog.open || s.selected != "benchmarks" || !s.benchFlowOpen {
		t.Fatalf("expected 'benchmark anyway' to close dialog and open benchmarks flow, got open=%v selected=%q flow=%v",
			s.extCheckDialog.open, s.selected, s.benchFlowOpen)
	}
}

func TestP4ReportsAndBenchmarksExclusivityBadges(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "reports"
	s.reports = []core.ReportSummary{
		{ReportId: "rep-excl", RunIds: []string{"r1"}, RunSummaries: []core.ReportListRun{{RunID: "r1", Model: "M1"}}},
		{ReportId: "rep-ext", RunIds: []string{"r2"}, RunSummaries: []core.ReportListRun{{RunID: "r2", Model: "M2"}}},
		{ReportId: "rep-unk", RunIds: []string{"r3"}, RunSummaries: []core.ReportListRun{{RunID: "r3", Model: "M3"}}},
	}
	s.reportExclusivities = map[string]string{
		"rep-excl": core.ExclusivityExclusive,
		"rep-ext":  core.ExclusivityExternal,
		"rep-unk":  core.ExclusivityUnknown,
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"独占", "有外部连接", "未检测"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected reports list to show exclusivity badge %q, got:\n%s", want, texts)
		}
	}

	// Benchmarks list task row exclusivity badge.
	s.selected = "benchmarks"
	s.benchFlowOpen = false
	bundle := "zh-core"
	s.tasks = []core.RunTask{
		{TaskId: "t1", State: "done", Bundle: &bundle, Exclusivity: core.ExclusivityExternal},
	}
	tt.Frame()
	benchTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(benchTexts, "有外部连接") {
		t.Fatalf("expected benchmarks list to show task exclusivity badge, got:\n%s", benchTexts)
	}
}

func TestP4EngineUpdateBadgeAndAction(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.selected = "library"
	s.libraryTab = 1
	s.engineUpdates = map[string]core.EngineUpdateInfo{
		"llama-cpp-default": {
			Engine:           "llama-cpp-default",
			InstalledVersion: "b4800",
			LatestVersion:    "b5400",
			HasUpdate:        true,
		},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"有新版 b5400", "更新"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected library engines tab to contain %q, got:\n%s", want, texts)
		}
	}

	click(t, tt, "更新 llama-cpp-default")
	tt.Frame()
	if s.downloadsUI == nil || !s.downloadsUI.engineDrawerOpen {
		t.Fatalf("expected clicking engine Update button to open engine download drawer, got %+v", s.downloadsUI)
	}
}
