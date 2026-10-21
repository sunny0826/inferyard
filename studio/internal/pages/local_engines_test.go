package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"testing"
)

func TestLocalCandidateFormatAndSpeedAdmission(t *testing.T) {
	for _, family := range []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"} {
		t.Run(family, func(t *testing.T) {
			s := &viewState{engine: "local", registry: core.Registry{Engines: []core.EngineAsset{{Name: "local", Kind: "local_installation", Family: family}}}}
			gguf := family == "llama-cpp" || family == "lmstudio"
			if s.compatibleModel(core.ModelAsset{Format: "gguf"}) != gguf || s.compatibleModel(core.ModelAsset{Format: "directory"}) == gguf {
				t.Fatal("format admission changed")
			}
			if s.speedStartBlockReason() != "安装声明引擎暂不支持长上下文速度；可运行普通 benchmark。" {
				t.Fatal(s.speedStartBlockReason())
			}
		})
	}
}

func TestLocalEngineFieldsDoNotReplacePathSettings(t *testing.T) {
	s := testState()
	tt := ui.NewTester(s.settingsView, 1120, 1800)
	if !tt.HasText("安装声明 JSON 路径") || !tt.HasText("注册引擎") || !tt.HasText("CLI") {
		t.Fatal(tt.Texts())
	}
}

func TestSwitchingFromLMStudioDoesNotAttachItsServiceToDirectEngines(t *testing.T) {
	s := &viewState{model: "model", engine: "lmstudio", localPID: "1234", localEndpoint: "http://127.0.0.1:1234"}
	for _, family := range []string{"lmstudio", "llama-cpp", "mlx-lm", "vllm", "sglang"} {
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{Name: family, Kind: "local_installation", Family: family})
	}
	for _, family := range []string{"lmstudio", "llama-cpp", "mlx-lm", "vllm", "sglang"} {
		s.engine = family
		request := s.localCandidateRequest([]string{"zh-core", "zh-svg-pelican"})
		if request == nil || request.EngineName != family || request.ModelName != "model" || len(request.Bundles) != 2 {
			t.Fatalf("%s: %+v", family, request)
		}
		if family == "lmstudio" {
			if request.ServicePID != 1234 || request.Endpoint != s.localEndpoint {
				t.Fatal(request)
			}
		} else if request.ServicePID != 0 || request.Endpoint != "" {
			t.Fatalf("hidden LM service leaked into %s: %+v", family, request)
		}
	}
}
