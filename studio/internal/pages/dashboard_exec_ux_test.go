package pages

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

func TestLaunchSummaryLineLocalEngines(t *testing.T) {
	s := testState()
	if got := launchSummaryLine(&s); got != "" {
		t.Fatalf("empty selection produced summary %q", got)
	}
	s.registry.Engines = []core.EngineAsset{
		{Name: "mlx-lm · local", Family: "mlx-lm", Kind: "local_installation"},
		{Name: "llama-cpp · local", Family: "llama-cpp", Kind: "local_installation"},
		{Name: "vllm · local", Family: "vllm", Kind: "local_installation"},
		{Name: "sglang · local", Family: "sglang", Kind: "local_installation"},
		{Name: "lmstudio · local", Family: "lmstudio", Kind: "local_installation"},
	}
	s.model = "demo-model"

	for _, tc := range []struct {
		engine string
		wants  []string
		forbid string
	}{
		{"mlx-lm · local", []string{"运行条件：", "MLX", "Metal 加速", "上下文 8192", "端口自动（默认 48857）"}, ""},
		{"llama-cpp · local", []string{"运行条件：", "llama.cpp", "Metal 加速", "上下文 8192", "端口自动（默认 48857）"}, ""},
		{"vllm · local", []string{"运行条件：", "vLLM", "上下文 8192", "端口自动（默认 48857）"}, ""},
		{"sglang · local", []string{"运行条件：", "SGLang", "上下文 8192", "端口自动（默认 48857）"}, ""},
		{"lmstudio · local", []string{"运行条件：", "LM Studio", "上下文 8192", "使用外部服务地址"}, "端口自动"},
	} {
		s.engine = tc.engine
		got := launchSummaryLine(&s)
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Fatalf("%s summary %q missing %q", tc.engine, got, want)
			}
		}
		if tc.forbid != "" && strings.Contains(got, tc.forbid) {
			t.Fatalf("%s summary %q should not contain %q", tc.engine, got, tc.forbid)
		}
	}
}

func TestLaunchSummaryLinePrism(t *testing.T) {
	cases := []struct {
		name     string
		snapshot string
		check    core.EngineEligibility
		ctxLen   *int64
		wants    []string
	}{
		{
			name:     "darwin metal clamps 32k to 4096",
			snapshot: `{"hardware":{"platform":"Darwin","apple_silicon":true,"gpu":{"devices":[{"name":"Apple M3","metal_supported":true}]}}}`,
			check:    core.EngineEligibility{State: core.EngineCheckEligible, Modes: map[string]bool{"cpu": true, "metal": true}},
			ctxLen:   ptr(int64(32768)),
			wants:    []string{"运行条件：", "Metal 加速", "上下文 4096", "关闭思考模式", "端口自动（默认 48857）"},
		},
		{
			name:     "darwin metal respects smaller model context",
			snapshot: `{"hardware":{"platform":"Darwin","gpu":{"devices":[{"metal_supported":true}]}}}`,
			check:    core.EngineEligibility{State: core.EngineCheckEligible},
			ctxLen:   ptr(int64(2048)),
			wants:    []string{"Metal 加速", "上下文 2048", "关闭思考模式", "端口自动（默认 48857）"},
		},
		{
			name:     "cpu when engine check disables metal",
			snapshot: `{"hardware":{"platform":"Darwin","apple_silicon":true,"gpu":{"devices":[{"metal_supported":true}]}}}`,
			check:    core.EngineEligibility{State: core.EngineCheckEligible, Modes: map[string]bool{"cpu": true, "metal": false}},
			wants:    []string{"CPU", "上下文 4096", "关闭思考模式", "端口自动（默认 48857）"},
		},
		{
			name:     "cpu on intel mac",
			snapshot: `{"hardware":{"platform":"Darwin","apple_silicon":false,"gpu":{"devices":[{"metal_supported":true}]}}}`,
			check:    core.EngineEligibility{State: core.EngineCheckEligible},
			wants:    []string{"CPU", "上下文 4096"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := candidateState()
			s.device.Snapshot = json.RawMessage(tc.snapshot)
			s.checks = core.EngineCheckOverview{Supported: true, Engines: map[string]core.EngineEligibility{s.engine: tc.check}}
			s.registry.Models[0].ContextLength = tc.ctxLen
			got := launchSummaryLine(&s)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("got %q, missing %q", got, want)
				}
			}
		})
	}
}

func TestIncompatibleModelSuffix(t *testing.T) {
	ggufModel := core.ModelAsset{Name: "qwen.gguf", Format: "gguf", Available: true}
	dirModel := core.ModelAsset{Name: "qwen-dir", Format: "directory", Available: true}

	s := candidateState()
	if got := s.incompatibleModelSuffix(ggufModel); got != "" {
		t.Fatalf("prism + gguf got %q, want empty", got)
	}
	if got := s.incompatibleModelSuffix(dirModel); got != "（需 GGUF）" {
		t.Fatalf("prism + dir got %q, want （需 GGUF）", got)
	}

	for _, family := range []string{"mlx-lm", "vllm", "sglang"} {
		s.registry.Engines = []core.EngineAsset{{Name: family, Family: family, Kind: "local_installation"}}
		s.engine = family
		if got := s.incompatibleModelSuffix(ggufModel); got != "（需目录模型）" {
			t.Fatalf("%s + gguf got %q, want （需目录模型）", family, got)
		}
		if got := s.incompatibleModelSuffix(dirModel); got != "" {
			t.Fatalf("%s + dir got %q, want empty", family, got)
		}
	}

	for _, family := range []string{"llama-cpp", "lmstudio"} {
		s.registry.Engines = []core.EngineAsset{{Name: family, Family: family, Kind: "local_installation"}}
		s.engine = family
		if got := s.incompatibleModelSuffix(dirModel); got != "（需 GGUF）" {
			t.Fatalf("%s + dir got %q, want （需 GGUF）", family, got)
		}
		if got := s.incompatibleModelSuffix(ggufModel); got != "" {
			t.Fatalf("%s + gguf got %q, want empty", family, got)
		}
	}

	s.engine = ""
	if got := s.incompatibleModelSuffix(dirModel); got != "" {
		t.Fatalf("empty engine got %q, want empty", got)
	}
}

func TestLMStudioHandoffNotice(t *testing.T) {
	s := testState()
	s.registry.Engines = []core.EngineAsset{
		{Name: "lmstudio · local", Family: "lmstudio", Kind: "local_installation"},
		{Name: "mlx-lm · local", Family: "mlx-lm", Kind: "local_installation"},
	}
	s.engine = "lmstudio · local"
	const want = "提示：选择多个题集时，LM Studio 会在第一套题集完成后暂停，需重启一次后台服务并重新绑定 PID 后继续。"
	if got := lmStudioHandoffNotice(&s); got != want {
		t.Fatalf("2 checked bundles got %q, want %q", got, want)
	}

	s.selectedBundles = map[string]bool{"zh-core": true, "zh-svg-pelican": false}
	if got := lmStudioHandoffNotice(&s); got != "" {
		t.Fatalf("1 checked bundle got %q, want empty", got)
	}

	s.selectedBundles = map[string]bool{"zh-core": true, "zh-svg-pelican": true}
	s.engine = "mlx-lm · local"
	if got := lmStudioHandoffNotice(&s); got != "" {
		t.Fatalf("non-lmstudio engine got %q, want empty", got)
	}
}

func TestCandidateFieldsRendersExecutionUX(t *testing.T) {
	s := testState()
	s.preview = &stubPreviewMode{}
	s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	s.progress.NextAction = "prepare_candidate"
	s.progress.DevicePreflight = inferyard.ProgressPreflight{Status: "ok", Path: ptr("/w/preflight")}
	s.registry = core.Registry{
		Models: []core.ModelAsset{
			{Name: "local.gguf", Format: "gguf", Available: true},
			{Name: "local-weights", Format: "directory", Available: true},
		},
		Engines: []core.EngineAsset{
			{Name: "lmstudio · 模拟安装", Family: "lmstudio", Kind: "local_installation"},
		},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines:   map[string]core.EngineEligibility{"lmstudio · 模拟安装": {State: core.EngineCheckEligible, Family: "lmstudio"}},
	}
	s.engine, s.model = "lmstudio · 模拟安装", "local-weights"
	s.localPID, s.localEndpoint = "4001", "http://127.0.0.1:1234"

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() { s.candidateFields(c) })
	}, 1120, 1800)

	if !tt.HasText("local-weights · 目录模型（需 GGUF）") {
		t.Fatalf("incompatible model suffix not rendered on selected option: %v", tt.Texts())
	}
	wantSummary := "运行条件：LM Studio · 上下文 8192 · 使用外部服务地址"
	if !tt.HasText(wantSummary) {
		t.Fatalf("launch summary %q not rendered: %v", wantSummary, tt.Texts())
	}
	wantNotice := "提示：选择多个题集时，LM Studio 会在第一套题集完成后暂停，需重启一次后台服务并重新绑定 PID 后继续。"
	if !tt.HasText(wantNotice) {
		t.Fatalf("LM Studio handoff notice not rendered: %v", tt.Texts())
	}

	click(t, tt, "zh-svg-pelican")
	if tt.HasText(wantNotice) {
		t.Fatalf("LM Studio handoff notice stayed visible for single bundle: %v", tt.Texts())
	}
}

func TestStartChainAppendsPortWhenDefaultOccupied(t *testing.T) {
	orig := chooseFreePortFunc
	t.Cleanup(func() { chooseFreePortFunc = orig })
	chooseFreePortFunc = func(int) (int, error) {
		return 49555, nil
	}

	h := newChainHarness()
	h.progress.Bundles[0].Phase = "run_complete"
	h.progress.Bundles[0].CandidatePath = ptr("/sealed/old")
	if _, e := h.run(t, "model.gguf", "engine"); e != nil {
		t.Fatal(e)
	}
	if len(h.calls) != 1 || !strings.Contains(h.calls[0].command, "--port 49555") {
		t.Fatalf("expected --port 49555 in command, got %+v", h.calls)
	}

	h2 := newChainHarness()
	withPort := *h2.progress.Bundles[0].NextCommand + " --port 49000"
	h2.progress.Bundles[0].NextCommand = &withPort
	h2.progress.Bundles[1].Phase = "run_complete"
	h2.progress.Bundles[1].CandidatePath = ptr("/sealed/old")
	if _, e := h2.run(t, "model.gguf", "engine"); e != nil {
		t.Fatal(e)
	}
	if len(h2.calls) != 1 || strings.Count(h2.calls[0].command, "--port") != 1 {
		t.Fatalf("duplicate --port appended: %+v", h2.calls)
	}

	chooseFreePortFunc = orig
	holder, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.DefaultBenchmarkPort)))
	if err == nil {
		defer holder.Close()
		h3 := newChainHarness()
		h3.progress.Bundles[0].Phase = "run_complete"
		h3.progress.Bundles[0].CandidatePath = ptr("/sealed/old")
		if _, e := h3.run(t, "model.gguf", "engine"); e != nil {
			t.Fatal(e)
		}
		if len(h3.calls) != 1 || !strings.Contains(h3.calls[0].command, "--port ") {
			t.Fatalf("real occupied 48857 did not trigger fallback --port: %+v", h3.calls)
		}
	}
}
