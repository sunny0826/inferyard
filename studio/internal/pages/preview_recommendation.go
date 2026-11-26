//go:build ui_debug && !release

package pages

import (
	"encoding/json"

	"inferyard/studio/internal/core"
)

func previewRecEmptyHost(s *viewState, page, hostPlatform string, snapshot string) {
	s.selected = canonicalPageID(page)
	if s.selected == "benchmarks" {
		s.benchFlowOpen = true
	}
	s.registry = core.Registry{}
	s.model, s.engine = "", ""
	s.checks = core.EngineCheckOverview{Supported: true}
	s.downloads = nil
	s.history = core.RunHistoryResponse{}
	s.reports = nil
	s.device.Snapshot = json.RawMessage(snapshot)
	st := s.ensureDownloadsUI()
	st.hostPlatform = hostPlatform
	st.starterExpandedOverride = nil
	st.starterChoice = 0
}

func previewRecMac48FirstUse(s *viewState) {
	previewRecEmptyHost(s, "benchmarks", "darwin/arm64", previewSnapshotMac48)
}

func previewRecRTX5090FirstUse(s *viewState) {
	previewRecEmptyHost(s, "benchmarks", "windows/amd64", previewSnapshotRTX5090)
}

func previewRecRTX4070Tight(s *viewState) {
	s.libraryTab = 0
	previewRecEmptyHost(s, "library", "windows/amd64", previewSnapshotRTX4070)
}

func previewRecExistingAssetsCollapsed(s *viewState) {
	s.selected = "library"
	s.libraryTab = 1
	s.downloads = nil
	s.device.Snapshot = json.RawMessage(previewSnapshotMac48)
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	st.starterExpandedOverride = nil
	st.starterChoice = 0
}

func previewRecCPUFallback(s *viewState) {
	previewRecEmptyHost(s, "benchmarks", "windows/amd64", previewSnapshotCPU16)
}

func previewRecBlockedDisk(s *viewState) {
	s.libraryTab = 0
	previewRecEmptyHost(s, "library", "darwin/arm64", previewSnapshotBlockedDisk)
}

const previewSnapshotMac48 = `{
	"hardware": {
		"platform": "Darwin",
		"os_version": "15.4",
		"cpu_model": "Apple M4 Pro",
		"logical_cpus": 14,
		"architecture": "arm64",
		"apple_silicon": true,
		"xcode_clt_installed": true,
		"cpu_features": {
			"avx2": false,
			"avx512f": false
		},
		"memory_total_bytes": 51539607552,
		"memory_available_bytes": 36721975296,
		"disk_free_bytes": 171798691840,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [
				{
					"name": "Apple M4 Pro",
					"vendor": "apple",
					"arch_family": "Apple Silicon",
					"cores": 20,
					"metal_supported": true,
					"missing": {
						"memory_total_bytes": "shared_host_memory",
						"memory_free_bytes": "not_measured"
					}
				}
			]
		},
		"missing": {
			"temperature": "unsupported"
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "polystrata",
		"backend": "metal",
		"model_id": "polystrata-qwen38-coder-iq1-m",
		"model_title": "Qwen3.8-Flash-Next Coder (IQ1_M)",
		"family": "coder",
		"size": "IQ1_M",
		"engine_core": "strata",
		"model_format": "strata_pack",
		"fit_level": "tight",
		"recommended_context": 32768,
		"estimated_download_bytes": 58400000000,
		"estimated_ram_bytes": 37000000000,
		"tier": "best",
		"disk_free_bytes": 171798691840,
		"reason": "polystrata_hardware_fit",
		"gpu": {
			"vendor": "apple",
			"name": "Apple M4 Pro",
			"vram_bytes": null,
			"unified_memory_bytes": 51539607552
		},
		"notes": [],
		"alternatives": [
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q4-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
				"family": "qwen36",
				"size": "UD-Q4_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 28700000000,
				"tier": "balanced"
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 17000000000,
				"tier": "lightweight"
			},
			{
				"engine": "llama-cpp",
				"backend": "metal",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M (GGUF)",
				"family": "llama-cpp",
				"size": null,
				"engine_core": null,
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944,
				"tier": "fallback"
			}
		],
		"polystrata_models": [
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen38-coder-iq1-m",
				"model_title": "Qwen3.8-Flash-Next Coder (IQ1_M)",
				"family": "coder",
				"size": "IQ1_M",
				"engine_core": "strata",
				"model_format": "strata_pack",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 58400000000,
				"estimated_ram_bytes": 37000000000,
				"tier": "best"
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q4-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
				"family": "qwen36",
				"size": "UD-Q4_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 28700000000,
				"tier": "balanced"
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q3-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)",
				"family": "qwen36",
				"size": "UD-Q3_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 17104402720,
				"estimated_ram_bytes": 23100000000
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-qwen36-ud-q2-k-xl",
				"model_title": "Qwen3.6-35B-A3B (UD-Q2_K_XL)",
				"family": "qwen36",
				"size": "UD-Q2_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 12574128416,
				"estimated_ram_bytes": 18600000000
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q4-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q4_K_XL)",
				"family": "lfm25",
				"size": "UD-Q4_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 14876901632,
				"estimated_ram_bytes": 20900000000
			},
			{
				"engine": "polystrata",
				"backend": "metal",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 17000000000,
				"tier": "lightweight"
			}
		]
	}
}`

const previewSnapshotCPU16 = `{
	"hardware": {
		"platform": "Windows",
		"os_version": "11",
		"cpu_model": "Intel Core Ultra 7 155H",
		"logical_cpus": 16,
		"architecture": "amd64",
		"cpu_features": {
			"avx2": true,
			"avx512f": false
		},
		"memory_total_bytes": 17179869184,
		"memory_available_bytes": 11166914969,
		"disk_free_bytes": 88046829568,
		"ac_online": false,
		"gpu": {
			"status": "unavailable",
			"reason": "nvidia_smi_not_found",
			"devices": []
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "llama-cpp",
		"backend": "cpu",
		"model_id": "model-qwen3-4b-q4km",
		"model_title": "Qwen3-4B-Q4_K_M (GGUF)",
		"family": "llama-cpp",
		"size": null,
		"engine_core": null,
		"model_format": "gguf",
		"fit_level": "fits",
		"recommended_context": 8192,
		"estimated_download_bytes": 2497280256,
		"estimated_ram_bytes": 6442450944,
		"tier": "best",
		"disk_free_bytes": 88046829568,
		"reason": "fallback_llama_cpp_no_compatible_polystrata_gpu",
		"gpu": null,
		"notes": [],
		"alternatives": [
			{
				"engine": "llama-cpp",
				"backend": "cpu",
				"model_id": "model-qwen3-0.6b-q8",
				"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
				"family": "llama-cpp",
				"size": null,
				"engine_core": null,
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 8192,
				"estimated_download_bytes": 639446688,
				"estimated_ram_bytes": 2147483648,
				"tier": "lightweight"
			},
			{
				"engine": "llama-cpp",
				"backend": "cpu",
				"model_id": "model-smollm2-135m",
				"model_title": "SmolLM2-135M-Instruct-Q4_K_M (GGUF)",
				"family": "llama-cpp",
				"size": null,
				"engine_core": null,
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 4096,
				"estimated_download_bytes": 105454144,
				"estimated_ram_bytes": 1073741824,
				"tier": "fallback"
			}
		]
	}
}`

const previewSnapshotBlockedDisk = `{
	"hardware": {
		"platform": "Darwin",
		"os_version": "15.4",
		"cpu_model": "Apple M3",
		"logical_cpus": 8,
		"architecture": "arm64",
		"apple_silicon": true,
		"memory_total_bytes": 17179869184,
		"memory_available_bytes": 9771050598,
		"disk_free_bytes": 536870912,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [
				{
					"name": "Apple M3",
					"vendor": "apple",
					"arch_family": "Apple Silicon",
					"cores": 10,
					"metal_supported": true
				}
			]
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "blocked",
		"engine": null,
		"backend": null,
		"model_id": null,
		"model_title": null,
		"family": null,
		"size": null,
		"engine_core": null,
		"model_format": null,
		"fit_level": null,
		"recommended_context": null,
		"estimated_download_bytes": null,
		"estimated_ram_bytes": null,
		"disk_free_bytes": 536870912,
		"reason": "insufficient_disk_space",
		"gpu": null,
		"notes": [],
		"alternatives": []
	}
}`
