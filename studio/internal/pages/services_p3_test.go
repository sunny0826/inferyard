package pages

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestServicesP3MultiServiceRenderAndIndependentStop(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":51539607552}}`)
	id1 := core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default")
	id2 := core.ServiceIDFor("Llama-3-8B.gguf", "llama-cpp-default")
	s.managedServices = []core.EngineState{
		{
			ServiceID:         id1,
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			Adopted:           true,
			EngineName:        "llama-cpp-default",
			ModelName:         "Qwen3-8B-Q4.gguf",
			Port:              8080,
			LANAccess:         true,
			EstimatedMemoryGB: 6.2,
			ContextSize:       32768,
			Endpoint:          "http://0.0.0.0:8080",
			LogPath:           "/tmp/svc1.log",
			Pid:               2001,
		},
		{
			ServiceID:         id2,
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			EngineName:        "llama-cpp-default",
			ModelName:         "Llama-3-8B.gguf",
			Port:              8081,
			EstimatedMemoryGB: 5.4,
			ContextSize:       8192,
			Endpoint:          "http://127.0.0.1:8081",
			LogPath:           "/tmp/svc2.log",
			Pid:               2002,
		},
	}
	s.engineState = s.managedServices[0]
	// Both running services should be filtered out of Recent Services.
	s.recentServices = []core.ServicePreset{
		{Model: "Qwen3-8B-Q4.gguf", Engine: "llama-cpp-default", Port: 8080, ContextSize: 32768},
		{Model: "Llama-3-8B.gguf", Engine: "llama-cpp-default", Port: 8081, ContextSize: 8192},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"2 个托管服务运行中", "已占内存 11.6 GB", "Qwen3-8B-Q4.gguf", "Llama-3-8B.gguf", "已接管", "局域网开放", "6.2 GB"} {
		if !strings.Contains(texts, want) {
			t.Fatalf("expected multi-service view to contain %q, got:\n%s", want, texts)
		}
	}
	if strings.Contains(texts, "最近启动过") {
		t.Fatalf("expected Recent Services hidden when all presets are already running, got:\n%s", texts)
	}

	// Second card is collapsed by default; expand it and arm independent stop confirmation.
	if s.isServiceExpanded(id2, 1) {
		t.Fatal("expected second service card collapsed by default")
	}
	s.toggleServiceExpanded(id2, 1)
	tt.Frame()
	if !s.isServiceExpanded(id2, 1) {
		t.Fatal("expected second service card expanded after toggle")
	}
	click(t, tt, "停止 · Llama-3-8B.gguf")
	tt.Frame()
	if destructiveKind(s.pendingDestructive.kind) != destructiveStopService || s.pendingDestructive.id != id2 {
		t.Fatalf("expected independent stop confirmation for %q, got %+v", id2, s.pendingDestructive)
	}
}

func TestServicesP3MemoryBudgetDisablesRecentAndDrawerStart(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	// 16 GiB unified memory -> 12.0 GB available ceiling.
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":true,"memory_total_bytes":17179869184}}`)
	bigSize := int64(8 * (1 << 30)) // ~9.5+ GB estimated
	s.registry.Models[0].SizeBytes = &bigSize
	s.registry.Models[1].SizeBytes = &bigSize

	// First service uses 9.5 GB -> only 2.5 GB remaining.
	s.managedServices = []core.EngineState{
		{
			ServiceID:         core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default"),
			State:             "running",
			Origin:            core.EngineOriginStandalone,
			EngineName:        "llama-cpp-default",
			ModelName:         "Qwen3-8B-Q4.gguf",
			Port:              8080,
			EstimatedMemoryGB: 9.5,
			ContextSize:       8192,
			Endpoint:          "http://127.0.0.1:8080",
		},
	}
	s.engineState = s.managedServices[0]
	s.recentServices = []core.ServicePreset{
		{Model: "Llama-3-8B.gguf", Engine: "llama-cpp-default", Port: 0, ContextSize: 8192},
	}

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "内存不足") {
		t.Fatalf("expected memory insufficient hint on Recent card, got:\n%s", texts)
	}
	if info, ok := tt.AccessByRole(ui.RoleButton, "▶ 启动 · Llama-3-8B.gguf"); !ok || !info.Disabled {
		t.Fatalf("expected Recent Start button disabled on memory shortfall, got ok=%v info=%+v", ok, info)
	}

	// Open Launch Params drawer for Llama-3-8B.gguf: memory bar + disabled Start button.
	s.openServiceParamsDrawerForPreset(s.recentServices[0])
	tt.Frame()
	drawerTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(drawerTexts, "内存余量预估") || !strings.Contains(drawerTexts, "可用内存不足") {
		t.Fatalf("expected memory budget bar and insufficient warning in drawer, got:\n%s", drawerTexts)
	}
	if info, ok := tt.AccessByRole(ui.RoleButton, "启动服务"); !ok || !info.Disabled {
		t.Fatalf("expected drawer Start Service disabled on memory shortfall, got ok=%v info=%+v", ok, info)
	}
}

func TestServicesP3LANSwitchSecurityWarningAndPresetPersist(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, svc := servicesP2State(t)
	s.openServiceParamsDrawerForNew()
	tt := newServicesTester(s)
	tt.Frame()

	if s.serviceParams.lanAccess {
		t.Fatal("expected LAN access disabled by default")
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "安全提醒") {
		t.Fatal("expected no LAN security warning while switch is off")
	}

	click(t, tt, "切换允许局域网访问")
	tt.Frame()
	if !s.serviceParams.lanAccess {
		t.Fatal("expected LAN access enabled after clicking switch")
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), "安全提醒") {
		t.Fatalf("expected LAN security warning after enabling switch, got:\n%s", strings.Join(tt.Texts(), "\n"))
	}

	click(t, tt, "仅保存参数")
	tt.Frame()
	saved, ok := svc.ServicePresetFor("Qwen3-8B-Q4.gguf", "llama-cpp-default")
	if !ok || !saved.LANAccess {
		t.Fatalf("expected LANAccess=true persisted in preset, got ok=%v preset=%+v", ok, saved)
	}
}

func TestServicesP3SidebarPopoverAndMultiQuitDialog(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	id1 := core.ServiceIDFor("Qwen3-8B-Q4.gguf", "llama-cpp-default")
	id2 := core.ServiceIDFor("Llama-3-8B.gguf", "llama-cpp-default")
	s.managedServices = []core.EngineState{
		{
			ServiceID:  id1,
			State:      "running",
			Origin:     core.EngineOriginStandalone,
			EngineName: "llama-cpp-default",
			ModelName:  "Qwen3-8B-Q4.gguf",
			Port:       8080,
			Endpoint:   "http://127.0.0.1:8080",
		},
		{
			ServiceID:  id2,
			State:      "running",
			Origin:     core.EngineOriginStandalone,
			EngineName: "llama-cpp-default",
			ModelName:  "Llama-3-8B.gguf",
			Port:       8081,
			Endpoint:   "http://127.0.0.1:8081",
		},
	}
	s.engineState = s.managedServices[0]

	tt := newServicesTester(s)
	tt.Frame()
	// Sidebar footer should show "2 个服务运行中" and open multi-service popover on click.
	click(t, tt, "2 个服务运行中")
	tt.Frame()
	if !s.sidebarServiceOpen {
		t.Fatal("expected sidebar service popover open")
	}
	popTexts := strings.Join(tt.Texts(), "\n")
	for _, want := range []string{"运行中的服务（2）", ":8080", ":8081", "网页", "复制", "停止", "打开服务页"} {
		if !strings.Contains(popTexts, want) {
			t.Fatalf("expected sidebar popover to contain %q, got:\n%s", want, popTexts)
		}
	}
	s.sidebarServiceOpen = false

	// Multi-service quit dialog shows count and supports "同时停止所有服务并退出".
	s.minimizeToTray = false
	closed := false
	s.quitDialog.closeWindow = func() { closed = true }
	s.handleWindowClose(nil)
	tt.Frame()
	dialogTexts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(dialogTexts, "2 个本地模型服务正在运行") {
		t.Fatalf("expected multi-service count in quit dialog, got:\n%s", dialogTexts)
	}
	click(t, tt, "同时停止所有服务并退出")
	tt.Frame()
	if !closed || !s.quitDialog.bypassClose {
		t.Fatalf("expected window closed after Stop All Services & Quit, closed=%v dialog=%+v", closed, s.quitDialog)
	}
}

func TestServicesP3MultipleExitNoticesDismissIndividually(t *testing.T) {
	restore := l10n.SetTestLocale(l10n.LanguageZhHans)
	defer restore()

	s, _ := servicesP2State(t)
	s.syncExitedNotices([]core.ServiceExitNotice{
		{ID: "exit-a", Model: "Qwen3-8B-Q4.gguf", Engine: "llama-cpp-default", DetectedAt: "2026-11-25T05:10:00Z"},
		{ID: "exit-b", Model: "Llama-3-8B.gguf", Engine: "llama-cpp-default", DetectedAt: "2026-11-25T05:12:00Z"},
	})

	tt := newServicesTester(s)
	tt.Frame()
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, "Qwen3-8B-Q4.gguf") || !strings.Contains(texts, "Llama-3-8B.gguf") {
		t.Fatalf("expected both exit notices rendered, got:\n%s", texts)
	}

	click(t, tt, "知道了 · Qwen3-8B-Q4.gguf")
	tt.Frame()
	remaining := s.effectiveExitedNotices()
	if len(remaining) != 1 || remaining[0].ID != "exit-b" {
		t.Fatalf("expected only exit-b remaining after dismissing exit-a, got %+v", remaining)
	}
}

func TestHardwareMemoryBudgetCacheParsesOncePerSnapshot(t *testing.T) {
	s, _ := servicesP2State(t)
	modelSize := int64(4 * (1 << 30))
	s.registry.Models[0].SizeBytes = &modelSize
	s.registry.Models[1].SizeBytes = &modelSize
	// Discrete 16 GiB VRAM + 32 GiB host RAM.
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":false,"memory_total_bytes":34359738368,"gpu":{"devices":[{"memory_kind":"dedicated","memory_total_bytes":17179869184}]}}}`)

	zero := 0
	bDiscrete1 := s.checkMemoryBudget("Qwen3-8B-Q4.gguf", "llama-cpp-default", 8192, nil)
	bHost1 := s.checkMemoryBudget("Qwen3-8B-Q4.gguf", "llama-cpp-default", 8192, &zero)
	sum1 := s.currentMemorySummary()
	bDiscrete2 := s.checkMemoryBudget("Llama-3-8B.gguf", "llama-cpp-default", 8192, nil)

	if s.hwMemoryCache.parseCount != 1 {
		t.Fatalf("parseCount=%d want 1 for unchanged snapshot", s.hwMemoryCache.parseCount)
	}
	if !bDiscrete1.Known || bDiscrete1.TotalGB != 16 || !bDiscrete2.Known || bDiscrete2.TotalGB != 16 {
		t.Fatalf("expected 16 GB discrete budget, got %+v / %+v", bDiscrete1, bDiscrete2)
	}
	if !bHost1.Known || bHost1.TotalGB != 32 {
		t.Fatalf("expected 32 GB host budget for cpu-only gpuLayers=0, got %+v", bHost1)
	}
	if !sum1.Known || sum1.TotalGB != 16 {
		t.Fatalf("expected 16 GB summary, got %+v", sum1)
	}

	// Same byte length (11 digits -> 11 digits) change must still invalidate cache via SHA-256 fingerprint.
	s.device.Snapshot = json.RawMessage(`{"hardware":{"apple_silicon":false,"memory_total_bytes":68719476736,"gpu":{"devices":[{"memory_kind":"dedicated","memory_total_bytes":17179869184}]}}}`)
	bHost2 := s.checkMemoryBudget("Qwen3-8B-Q4.gguf", "llama-cpp-default", 8192, &zero)
	if s.hwMemoryCache.parseCount != 2 {
		t.Fatalf("parseCount=%d want 2 after snapshot update", s.hwMemoryCache.parseCount)
	}
	if !bHost2.Known || bHost2.TotalGB != 64 {
		t.Fatalf("expected updated 64 GB host budget, got %+v", bHost2)
	}

	// Repeated call after update hits cache again.
	_ = s.currentMemorySummary()
	if s.hwMemoryCache.parseCount != 2 {
		t.Fatalf("parseCount=%d want 2 on subsequent cache hit", s.hwMemoryCache.parseCount)
	}
}
