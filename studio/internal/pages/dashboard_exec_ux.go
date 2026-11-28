package pages

import (
	"encoding/json"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func macOSBatteryWarning(raw json.RawMessage) bool {
	snapshot, err := parseDeviceSnapshot(raw)
	if err != nil || snapshot == nil {
		return false
	}
	hw := snapshot.object("hardware")
	if hw == nil {
		return false
	}
	platform, _ := hw["platform"].(string)
	if platform != "Darwin" {
		return false
	}
	online, ok := hw["ac_online"].(bool)
	return ok && !online
}

func failureView(c *ui.Context, title string, e *core.ApiError, next string) {
	ui.Text(c, title).Bold().TextColor(stateColor(c, stateDanger))
	if e != nil {
		errorView(c, e)
	} else {
		noticeBanner(c, noticeBannerOptions{Kind: stateDanger, Size: bannerCompact, Message: tr(c, "dashboard.failure.no_detail")})
	}
	if next != "" {
		ui.Text(c, next).TextColor(tokens(c).Color.FgSecondary)
	}
}

func localBackendLabel(family string) string {
	switch family {
	case "polystrata":
		return "PolyStrata"
	case "llama-cpp":
		return l10n.TR("dashboard.exec.backend_llamacpp_metal")
	case "mlx-lm":
		return l10n.TR("dashboard.exec.backend_mlx_metal")
	case "vllm":
		return "vLLM"
	case "lmstudio":
		return "LM Studio"
	default:
		if family != "" {
			return family
		}
		return l10n.TR("dashboard.exec.backend_local_default")
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
			return l10n.TR("dashboard.exec.accel_metal")
		case "cuda":
			return l10n.TR("dashboard.exec.accel_cuda")
		case "hip":
			return l10n.TR("dashboard.exec.accel_hip")
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
					return l10n.TR("dashboard.exec.accel_metal")
				}
			}
		}
		if platform != "" && platform != "Darwin" {
			return "CPU"
		}
	}
	if el, ok := w.checks.Engines[w.engine]; ok && el.Modes != nil && el.Modes["metal"] {
		return l10n.TR("dashboard.exec.accel_metal")
	}
	return "CPU"
}

func polystrataBackendLabel(w *viewState) string {
	if w != nil {
		if el, ok := w.checks.Engines[w.engine]; ok {
			switch el.Mode {
			case "hip":
				return l10n.TR("dashboard.exec.polystrata_hip")
			case "cuda":
				return l10n.TR("dashboard.exec.polystrata_cuda")
			case "metal":
				return l10n.TR("dashboard.exec.polystrata_metal")
			case "cpu":
				return l10n.TR("dashboard.exec.polystrata_cpu")
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
			return l10n.TRf("dashboard.exec.summary_lmstudio", "backend", localBackendLabel(e.Family))
		}
		if e.Family == "polystrata" {
			return l10n.TRf(
				"dashboard.exec.summary_polystrata_thinking",
				"backend", polystrataBackendLabel(w),
				"thinking", reasoningSummaryLabel(w.effectiveReasoningEffort()),
			)
		}
		return l10n.TRf("dashboard.exec.summary_local", "backend", localBackendLabel(e.Family))
	}
	return l10n.TRf("dashboard.exec.summary_prism", "backend", prismBackendLabel(w), "context", prismContextSize(w.selectedModel()))
}

func (w *viewState) incompatibleModelSuffix(model core.ModelAsset) string {
	if w == nil || w.engine == "" || w.compatibleModel(model) {
		return ""
	}
	if e := w.localEngine(); e != nil {
		switch e.Family {
		case "mlx-lm", "vllm":
			return l10n.TR("dashboard.exec.compat_dir")
		case "polystrata":
			if model.Format == "gguf" && model.Architecture != "" {
				return l10n.TR("dashboard.exec.compat_polystrata_arch")
			}
			return l10n.TR("dashboard.exec.compat_polystrata_format")
		case "llama-cpp", "lmstudio":
			return l10n.TR("dashboard.exec.compat_gguf")
		default:
			return ""
		}
	}
	return l10n.TR("dashboard.exec.compat_gguf")
}

func lmStudioHandoffNotice(w *viewState) string {
	if w == nil {
		return ""
	}
	e := w.localEngine()
	if e == nil || e.Family != "lmstudio" || len(w.checkedBundles()) <= 1 {
		return ""
	}
	return l10n.TR("dashboard.exec.lmstudio_handoff")
}
