package pages

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestBrandSVGsHaveNoUnsupportedFeaturesAndRenderPixels(t *testing.T) {
	icons := []struct {
		name string
		svg  *ui.SVG
	}{
		{"llamacpp", svgBrandLlamaCpp},
		{"polystrata", svgBrandPolyStrata},
		{"mlx", svgBrandMLX},
		{"vllm", svgBrandVLLM},
		{"ollama", svgBrandOllama},
		{"lmstudio", svgBrandLMStudio},
		{"qwen", svgBrandQwen},
		{"gemma", svgBrandGemma},
		{"meta", svgBrandMeta},
		{"deepseek", svgBrandDeepSeek},
		{"huggingface", svgBrandHuggingFace},
		{"zai", svgBrandZai},
		{"liquid", svgBrandLiquid},
		{"mistral", svgBrandMistral},
		{"openai", svgBrandOpenAI},
		{"microsoft", svgBrandMicrosoft},
		{"kimi", svgBrandKimi},
		{"minimax", svgBrandMiniMax},
		{"hunyuan", svgBrandHunyuan},
		{"nvidia", svgBrandNvidia},
		{"yi", svgBrandYi},
		{"openbmb", svgBrandOpenBMB},
		{"internlm", svgBrandInternLM},
		{"cohere", svgBrandCohere},
		{"doubao", svgBrandDoubao},
		{"baidu", svgBrandBaidu},
		{"stepfun", svgBrandStepFun},
		{"baichuan", svgBrandBaichuan},
		{"ibm", svgBrandIBM},
		{"tii", svgBrandTII},
		{"xiaomimimo", svgBrandXiaomiMiMo},
		{"ling", svgBrandLing},
		{"longcat", svgBrandLongCat},
	}

	for _, tc := range icons {
		if tc.svg == nil {
			t.Fatalf("%s SVG is nil", tc.name)
		}
		if unsupported := tc.svg.Unsupported(); len(unsupported) > 0 {
			t.Errorf("%s has unsupported SVG features: %v", tc.name, unsupported)
		}
		w, h := tc.svg.Size()
		if w <= 0 || h <= 0 {
			t.Errorf("%s has invalid SVG size: %.1fx%.1f", tc.name, w, h)
		}
		for _, dark := range []bool{false, true} {
			fg := color.RGBA{24, 24, 27, 255}
			if dark {
				fg = color.RGBA{244, 244, 245, 255}
			}
			img := tc.svg.WithDark(dark).Image(32, 32, fg)
			nonZero := 0
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] > 0 {
					nonZero++
				}
			}
			if nonZero == 0 {
				t.Errorf("%s rendered 0 visible pixels (dark=%v)", tc.name, dark)
			}
		}
	}
}

func TestResolveBrandEngineIcon(t *testing.T) {
	cases := []struct {
		input     string
		wantSVG   *ui.SVG
		wantColor bool
		wantKind  stateKind
	}{
		{"llama.cpp", svgBrandLlamaCpp, false, stateInfo},
		{"llama-cpp", svgBrandLlamaCpp, false, stateInfo},
		{"preview-engine · llama-server", svgBrandLlamaCpp, false, stateInfo},
		{"prism_llama_server_v1", svgBrandLlamaCpp, false, stateInfo},
		{"PolyStrata", svgBrandPolyStrata, true, stateSuccess},
		{"polystrata v0.2.20-poly.5", svgBrandPolyStrata, true, stateSuccess},
		{"strata-poly", svgBrandPolyStrata, true, stateSuccess},
		{"mlx-lm", svgBrandMLX, false, stateActive},
		{"mlx_lm.server", svgBrandMLX, false, stateActive},
		{"vLLM", svgBrandVLLM, true, stateNeutral},
		{"vllm-metal", svgBrandVLLM, true, stateNeutral},
		{"Ollama", svgBrandOllama, false, stateNeutral},
		{"LM Studio", svgBrandLMStudio, false, stateNeutral},
		{"lmstudio", svgBrandLMStudio, false, stateNeutral},
		{"lms", svgBrandLMStudio, false, stateNeutral},
		{"custom-unknown-server", nil, false, stateNeutral},
	}

	for _, tc := range cases {
		gotSVG, gotColor := resolveBrandEngineIcon(tc.input)
		if gotSVG != tc.wantSVG || gotColor != tc.wantColor {
			t.Errorf("resolveBrandEngineIcon(%q) = (%p, %v), want (%p, %v)", tc.input, gotSVG, gotColor, tc.wantSVG, tc.wantColor)
		}
		if gotKind := engineIconKind(tc.input); gotKind != tc.wantKind {
			t.Errorf("engineIconKind(%q) = %v, want %v", tc.input, gotKind, tc.wantKind)
		}
	}
}

func TestResolveBrandModelIcon(t *testing.T) {
	cases := []struct {
		input    string
		wantSVG  *ui.SVG
		wantMono bool
	}{
		{"Qwen3.8-Flash-Next (IQ3_S)", svgBrandQwen, false},
		{"QwQ-32B-Q4_K_M.gguf", svgBrandQwen, false},
		{"Clover-35B-A3B-Preview qwen35moe", svgBrandQwen, false},
		{"Gemma 4 26B-A4B (UD-Q3_K_M)", svgBrandGemma, false},
		{"Meta-Llama-3.3-70B-Instruct.gguf", svgBrandMeta, false},
		{"DeepSeek V4 Flash", svgBrandDeepSeek, false},
		{"DeepSeek-R1-Distill-Qwen-7B.gguf qwen2", svgBrandDeepSeek, false},
		{"DeepSeek-R1-Distill-Llama-8B.gguf llama", svgBrandDeepSeek, false},
		{"Llama-3.1-Nemotron-70B-Instruct llama", svgBrandNvidia, false},
		{"LFM2.5-24B-A2B (UD-Q4_K_XL) lfm2moe", svgBrandLiquid, true},
		{"GLM 5.3 Flash glm5-next", svgBrandZai, true},
		{"zai-org/GLM-4.5", svgBrandZai, true},
		{"Hy4-Flash", svgBrandHunyuan, false},
		{"Tencent-Hunyuan-A13B", svgBrandHunyuan, false},
		{"MiniCPM-V 4.7 35B-A3B qwen35moe", svgBrandOpenBMB, false},
		{"Mistral-Small-24B-Instruct.gguf llama", svgBrandMistral, false},
		{"Devstral-Small-2505", svgBrandMistral, false},
		{"gpt-oss-120b", svgBrandOpenAI, true},
		{"Phi-4-mini-instruct", svgBrandMicrosoft, false},
		{"Kimi-K2-Instruct", svgBrandKimi, false},
		{"MiniMax-M1-80k", svgBrandMiniMax, false},
		{"Yi-1.5-9B-Chat.gguf llama", svgBrandYi, false},
		{"TinyLlama-1.1B.gguf llama", svgBrandMeta, false},
		{"Tongyi-DeepResearch", svgBrandQwen, false},
		{"InternLM3-8B-Instruct", svgBrandInternLM, false},
		{"c4ai-command-r-v01", svgBrandCohere, false},
		{"Seed-OSS-36B-Instruct", svgBrandDoubao, false},
		{"ERNIE-4.5-21B-A3B", svgBrandBaidu, false},
		{"Step-3-Reasoning", svgBrandStepFun, false},
		{"Baichuan-M1-14B", svgBrandBaichuan, false},
		{"granite-3.3-8b-instruct", svgBrandIBM, true},
		{"Falcon3-10B-Instruct", svgBrandTII, false},
		{"Xiaomi-MiMo-7B-RL", svgBrandXiaomiMiMo, true},
		{"LongCat-Flash-Chat", svgBrandLongCat, false},
		{"inclusionAI/Ling-plus", svgBrandLing, false},
		{"SmolLM2-135M-Instruct-Q4_K_M.gguf llama", svgBrandHuggingFace, false},
		{"custom-unknown-weights.bin", svgBrandHuggingFace, false},
	}

	for _, tc := range cases {
		gotSVG, gotMono := resolveBrandModelIcon(tc.input)
		if gotSVG != tc.wantSVG || gotMono != tc.wantMono {
			t.Errorf("resolveBrandModelIcon(%q) = (%p, %v), want (%p, %v)", tc.input, gotSVG, gotMono, tc.wantSVG, tc.wantMono)
		}
	}
}

func TestViewStateBrandIconHints(t *testing.T) {
	s := &viewState{
		registry: core.Registry{
			Models: []core.ModelAsset{
				{Name: "Preview-Model-Q4.gguf", Format: "gguf", Architecture: "llama", Path: "/tmp/models/Preview-Model-Q4.gguf"},
				{Name: "Preview-Small.gguf", Format: "gguf", Architecture: "qwen3", Path: "/tmp/models/Preview-Small.gguf"},
			},
			Engines: []core.EngineAsset{
				{Name: "my-local-vllm", Family: "vllm"},
				{Name: "my-local-mlx", Family: "mlx-lm"},
				{Name: "my-local-poly", Family: "polystrata"},
			},
		},
	}

	if svg, _ := resolveBrandModelIcon(s.modelIconHint("Preview-Model-Q4.gguf")); svg != svgBrandMeta {
		t.Fatalf("expected Preview-Model-Q4.gguf with llama arch to resolve to Meta icon")
	}
	if svg, _ := resolveBrandModelIcon(s.modelIconHint("Preview-Small.gguf · GGUF")); svg != svgBrandQwen {
		t.Fatalf("expected Preview-Small.gguf · GGUF with qwen3 arch to resolve to Qwen icon")
	}
	if svg, _ := resolveBrandEngineIcon(s.engineIconHint("my-local-vllm")); svg != svgBrandVLLM {
		t.Fatalf("expected my-local-vllm with vllm family to resolve to vLLM icon")
	}
	if svg, _ := resolveBrandEngineIcon(s.engineIconHint("my-local-mlx")); svg != svgBrandMLX {
		t.Fatalf("expected my-local-mlx with mlx-lm family to resolve to MLX icon")
	}
	if svg, _ := resolveBrandEngineIcon(s.engineIconHint("my-local-poly")); svg != svgBrandPolyStrata {
		t.Fatalf("expected my-local-poly with polystrata family to resolve to PolyStrata icon")
	}
}
