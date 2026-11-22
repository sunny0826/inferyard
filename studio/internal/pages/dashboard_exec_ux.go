package pages

import (
	"fmt"

	"inferyard/studio/internal/core"
)

func localBackendLabel(family string) string {
	switch family {
	case "polystrata":
		return "PolyStrata"
	case "llama-cpp":
		return "llama.cpp（Metal 加速）"
	case "mlx-lm":
		return "MLX（Metal 加速）"
	case "vllm":
		return "vLLM"
	case "sglang":
		return "SGLang"
	case "lmstudio":
		return "LM Studio"
	default:
		if family != "" {
			return family
		}
		return "本地引擎"
	}
}

func (w *viewState) selectedModel() *core.ModelAsset {
	for i := range w.registry.Models {
		if w.registry.Models[i].Name == w.model {
			return &w.registry.Models[i]
		}
	}
	return nil
}

func prismContextSize(model *core.ModelAsset) int64 {
	const defaultContext int64 = 4096
	if model != nil && model.ContextLength != nil && *model.ContextLength > 0 && *model.ContextLength < defaultContext {
		return *model.ContextLength
	}
	return defaultContext
}

func prismBackendLabel(w *viewState) string {
	if el, ok := w.checks.Engines[w.engine]; ok {
		switch el.Mode {
		case "metal":
			return "Metal 加速"
		case "cuda":
			return "CUDA 加速"
		case "hip":
			return "HIP (ROCm) 加速"
		case "cpu":
			return "CPU"
		}
		if el.Modes != nil && !el.Modes["metal"] {
			return "CPU"
		}
	}
	if snapshot, err := parseDeviceSnapshot(w.device.Snapshot); err == nil && snapshot != nil {
		hw := snapshot.object("hardware")
		platform, _ := hw["platform"].(string)
		if appleSilicon, ok := hw["apple_silicon"].(bool); ok && !appleSilicon {
			return "CPU"
		}
		if platform == "" || platform == "Darwin" {
			for _, item := range hw.object("gpu").list("devices") {
				if deviceItem(item).flag("metal_supported") {
					return "Metal 加速"
				}
			}
		}
		if platform != "" && platform != "Darwin" {
			return "CPU"
		}
	}
	if el, ok := w.checks.Engines[w.engine]; ok && el.Modes != nil && el.Modes["metal"] {
		return "Metal 加速"
	}
	return "CPU"
}

func polystrataBackendLabel(w *viewState) string {
	if w != nil {
		if rec, ok := core.ParseStarterRecommendation(w.device.Snapshot); ok && rec.Engine == "polystrata" && rec.Backend != "" {
			return "PolyStrata（" + deviceLabel(rec.Backend) + "）"
		}
		if snapshot, err := parseDeviceSnapshot(w.device.Snapshot); err == nil && snapshot != nil {
			devices := snapshot.object("hardware").object("gpu").list("devices")
			for _, item := range devices {
				v := deviceItem(item)
				switch v.value("vendor") {
				case "amd":
					return "PolyStrata（HIP (ROCm) 加速）"
				case "nvidia":
					return "PolyStrata（CUDA 加速）"
				case "apple":
					return "PolyStrata（Metal 加速）"
				}
				if v.flag("metal_supported") {
					return "PolyStrata（Metal 加速）"
				}
			}
		}
	}
	return "PolyStrata"
}

func launchSummaryLine(w *viewState) string {
	if w == nil || w.engine == "" || w.model == "" {
		return ""
	}
	if e := w.localEngine(); e != nil {
		if e.Family == "lmstudio" {
			return fmt.Sprintf("运行条件：%s · 上下文 8192 · 使用外部服务地址", localBackendLabel(e.Family))
		}
		if e.Family == "polystrata" {
			return fmt.Sprintf("运行条件：%s · 端口自动（默认 %d）", polystrataBackendLabel(w), core.DefaultBenchmarkPort)
		}
		return fmt.Sprintf("运行条件：%s · 上下文 8192 · 端口自动（默认 %d）", localBackendLabel(e.Family), core.DefaultBenchmarkPort)
	}
	return fmt.Sprintf("运行条件：%s · 上下文 %d · 关闭思考模式 · 端口自动（默认 %d）", prismBackendLabel(w), prismContextSize(w.selectedModel()), core.DefaultBenchmarkPort)
}

func (w *viewState) incompatibleModelSuffix(model core.ModelAsset) string {
	if w == nil || w.engine == "" || w.compatibleModel(model) {
		return ""
	}
	if e := w.localEngine(); e != nil {
		switch e.Family {
		case "mlx-lm", "vllm", "sglang":
			return "（需目录模型）"
		case "polystrata":
			if model.Format == "gguf" && model.Architecture != "" {
				return "（PolyStrata 暂不支持此架构）"
			}
			return "（需 GGUF 或 PolyStrata 配置）"
		case "llama-cpp", "lmstudio":
			return "（需 GGUF）"
		default:
			return ""
		}
	}
	return "（需 GGUF）"
}

func lmStudioHandoffNotice(w *viewState) string {
	if w == nil {
		return ""
	}
	e := w.localEngine()
	if e == nil || e.Family != "lmstudio" || len(w.checkedBundles()) <= 1 {
		return ""
	}
	return "提示：选择多个题集时，LM Studio 会在第一套题集完成后暂停，需重启一次后台服务并重新绑定 PID 后继续。"
}
