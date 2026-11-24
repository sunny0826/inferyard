package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/l10n"
)

var deviceLabelKeys = map[string]string{
	"metal":                       "device.label.metal",
	"cuda":                        "device.label.cuda",
	"hip":                         "device.label.hip",
	"cpu":                         "device.label.cpu",
	"polystrata":                  "device.label.polystrata",
	"llama-cpp":                   "device.label.llama_cpp",
	"fits":                        "device.label.fits",
	"tight":                       "device.label.tight",
	"fallback":                    "device.label.fallback",
	"tier_best":                   "device.label.tier_best",
	"tier_balanced":               "device.label.tier_balanced",
	"tier_lightweight":            "device.label.tier_lightweight",
	"tier_fallback":               "device.label.tier_fallback",
	"blocked":                     "device.label.blocked",
	"recommended":                 "device.label.recommended",
	"device_snapshot_unavailable": "device.label.device_snapshot_unavailable",
	"rocm_smi_not_found":          "device.label.rocm_smi_not_found",
	"unsupported_gpu":             "device.label.unsupported_gpu",
	"insufficient_memory":         "device.label.insufficient_memory",
	"insufficient_disk":           "device.label.insufficient_disk",
	"insufficient_disk_space":     "device.label.insufficient_disk",
	"disk_space_unavailable":      "device.label.disk_space_unavailable",
	"disk_usage_unavailable":      "device.label.disk_space_unavailable",
	"unsupported_platform":        "device.label.unsupported_platform",
	"unified_memory_fit_estimate": "device.label.unified_memory_fit_estimate",
	"gpu_and_memory_fit_estimate": "device.label.gpu_and_memory_fit_estimate",
	"host_memory_fit_estimate":    "device.label.host_memory_fit_estimate",
	"host_memory_unavailable":     "device.label.host_memory_unavailable",
	"insufficient_host_memory":    "device.label.insufficient_host_memory",
	"no_local_model":              "device.label.no_local_model",
	"invalid_or_unreadable_gguf":  "device.label.invalid_or_unreadable_gguf",
	"chat_template_unavailable":   "device.label.chat_template_unavailable",
	"insufficient_output_disk":    "device.label.insufficient_output_disk",
	"output_disk_unavailable":     "device.label.output_disk_unavailable",
	"nvidia_smi_not_found":        "device.label.nvidia_smi_not_found",
	"nvidia_driver_query_failed":  "device.label.nvidia_driver_query_failed",
	"probe_timeout":               "device.label.probe_timeout",
	"memory_estimates_require_live_preflight":                             "device.label.memory_estimates_require_live_preflight",
	"runtime_backend_and_model_compatibility_require_verification":        "device.label.runtime_backend_and_model_compatibility_require_verification",
	"accelerator_execution_requires_verified_backend_and_service_binding": "device.label.accelerator_execution_requires_verified_backend_and_service_binding",
	"shared_host_memory":                  "device.label.shared_host_memory",
	"not_measured":                        "device.label.not_measured",
	"not_reported":                        "device.label.not_reported",
	"live_preflight_required":             "device.label.live_preflight_required",
	"provide_local_gguf":                  "device.label.provide_local_gguf",
	"resolve_blocker":                     "device.label.resolve_blocker",
	"configure_and_bind_existing_service": "device.label.configure_and_bind_existing_service",
	"unavailable":                         "device.label.unavailable",
	"polystrata_hardware_fit":             "device.label.polystrata_hardware_fit",
	"fallback_llama_cpp_no_compatible_polystrata_gpu":     "device.label.fallback_llama_cpp_no_compatible_polystrata_gpu",
	"fallback_llama_cpp_insufficient_ram_for_polystrata":  "device.label.fallback_llama_cpp_insufficient_ram_for_polystrata",
	"fallback_llama_cpp_insufficient_disk_for_polystrata": "device.label.fallback_llama_cpp_insufficient_disk_for_polystrata",
	"fallback_llama_cpp_insufficient_vram_for_polystrata": "device.label.fallback_llama_cpp_insufficient_vram_for_polystrata",
	"apple_silicon_required":                              "device.label.apple_silicon_required",
	"macos_version_below_15":                              "device.label.macos_version_below_15",
	"macos_version_unverified":                            "device.label.macos_version_unverified",
	"metal_not_supported":                                 "device.label.metal_not_supported",
	"nvidia_driver_too_old":                               "device.label.nvidia_driver_too_old",
	"nvidia_driver_version_unverified":                    "device.label.nvidia_driver_version_unverified",
	"nvidia_compute_capability_unsupported":               "device.label.nvidia_compute_capability_unsupported",
	"nvidia_compute_capability_unverified":                "device.label.nvidia_compute_capability_unverified",
	"nvidia_vram_unverified":                              "device.label.nvidia_vram_unverified",
	"amd_vram_unverified":                                 "device.label.amd_vram_unverified",
	"amd_arch_unsupported":                                "device.label.amd_arch_unsupported",
	"host_available_memory_tight":                         "device.label.host_available_memory_tight",
	"windows_page_file_low":                               "device.label.windows_page_file_low",
	"cpu_missing_avx2_experimental_build":                 "device.label.cpu_missing_avx2_experimental_build",
	"total_memory_unverified":                             "device.label.total_memory_unverified",
	"multiple_models_roots_unselected":                    "device.label.multiple_models_roots_unselected",
	"xcode_clt_missing":                                   "device.label.xcode_clt_missing",
	"polystrata_config_model_missing":                     "device.label.polystrata_config_model_missing",
}

func deviceLabelKnown(value string) (string, bool) {
	if key, ok := deviceLabelKeys[value]; ok {
		return l10n.TR(key), true
	}
	return "", false
}

func deviceLabel(value string) string {
	if key, ok := deviceLabelKeys[value]; ok {
		return l10n.TR(key)
	}
	return value
}

func deviceLabelFor(c *ui.Context, value string) string {
	if key, ok := deviceLabelKeys[value]; ok {
		return tr(c, key)
	}
	return value
}

func localizedDeviceNotes(notes []string) []string {
	return localizedDeviceNotesWith(notes, deviceLabel)
}

func localizedDeviceNotesFor(c *ui.Context, notes []string) []string {
	return localizedDeviceNotesWith(notes, func(value string) string {
		return deviceLabelFor(c, value)
	})
}

func localizedDeviceNotesWith(notes []string, lookup func(string) string) []string {
	if len(notes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(notes))
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		key := strings.TrimSpace(note)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		label := strings.TrimSpace(lookup(key))
		if label != "" {
			out = append(out, label)
		}
	}
	return out
}
