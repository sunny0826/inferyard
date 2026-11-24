//go:build ui_debug && !release

package pages

import (
	"encoding/json"

	"inferyard/studio/internal/core"
)

func previewRecEmptyHost(s *viewState, page, hostPlatform string, snapshot string) {
	s.selected = page
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
	previewRecEmptyHost(s, "dashboard", "darwin/arm64", previewSnapshotMac48)
}

func previewRecRTX5090FirstUse(s *viewState) {
	previewRecEmptyHost(s, "dashboard", "windows/amd64", previewSnapshotRTX5090)
}

func previewRecRTX4070Tight(s *viewState) {
	previewRecEmptyHost(s, "downloads", "windows/amd64", previewSnapshotRTX4070)
}

func previewRecExistingAssetsCollapsed(s *viewState) {
	s.selected = "downloads"
	s.downloads = nil
	s.device.Snapshot = json.RawMessage(previewSnapshotMac48)
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	st.starterExpandedOverride = nil
	st.starterChoice = 0
}

func previewRecCPUFallback(s *viewState) {
	previewRecEmptyHost(s, "dashboard", "windows/amd64", previewSnapshotCPU16)
}

func previewRecBlockedDisk(s *viewState) {
	previewRecEmptyHost(s, "downloads", "darwin/arm64", previewSnapshotBlockedDisk)
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
		"cpu_features": {"avx2": false, "avx512f": false},
		"memory_total_bytes": 51539607552,
		"memory_available_bytes": 36721975296,
		"disk_free_bytes": 171798691840,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [{
				"name": "Apple M4 Pro",
				"vendor": "apple",
				"arch_family": "Apple Silicon",
				"cores": 20,
				"metal_supported": true,
				"missing": {"memory_total_bytes": "shared_host_memory", "memory_free_bytes": "not_measured"}
			}]
		},
		"missing": {"temperature": "unsupported"}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "polystrata",
		"backend": "metal",
		"model_id": "polystrata-qwen36-ud-q4-k-m",
		"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
		"family": "qwen36",
		"size": "UD-Q4_K_M",
		"engine_core": "strata-poly",
		"model_format": "gguf",
		"fit_level": "fits",
		"tier": "best",
		"recommended_context": 32768,
		"estimated_download_bytes": 22663387424,
		"estimated_ram_bytes": 28700000000,
		"disk_free_bytes": 171798691840,
		"reason": "polystrata_hardware_fit",
		"gpu": {"name": "Apple M4 Pro", "vendor": "apple", "arch_family": "Apple Silicon", "unified_memory_bytes": 51539607552},
		"notes": [],
		"alternatives": [
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
				"tier": "balanced",
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
				"tier": "lightweight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 17000000000
			},
			{
				"engine": "llama-cpp",
				"backend": "metal",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "fallback",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944
			}
		],
		"polystrata_models": [
			{"engine": "polystrata", "backend": "metal", "model_id": "polystrata-qwen36-ud-q4-k-m", "model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)", "family": "qwen36", "size": "UD-Q4_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "best", "recommended_context": 32768, "estimated_download_bytes": 22663387424, "estimated_ram_bytes": 28700000000},
			{"engine": "polystrata", "backend": "metal", "model_id": "polystrata-qwen36-ud-q3-k-m", "model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)", "family": "qwen36", "size": "UD-Q3_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 32768, "estimated_download_bytes": 17104402720, "estimated_ram_bytes": 23100000000},
			{"engine": "polystrata", "backend": "metal", "model_id": "polystrata-qwen36-ud-q2-k-xl", "model_title": "Qwen3.6-35B-A3B (UD-Q2_K_XL)", "family": "qwen36", "size": "UD-Q2_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 32768, "estimated_download_bytes": 12574128416, "estimated_ram_bytes": 18600000000},
			{"engine": "polystrata", "backend": "metal", "model_id": "polystrata-lfm25-ud-q4-k-xl", "model_title": "LFM2.5-24B-A2B (UD-Q4_K_XL)", "family": "lfm25", "size": "UD-Q4_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "balanced", "recommended_context": 32768, "estimated_download_bytes": 14876901632, "estimated_ram_bytes": 20900000000},
			{"engine": "polystrata", "backend": "metal", "model_id": "polystrata-lfm25-ud-q3-k-xl", "model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)", "family": "lfm25", "size": "UD-Q3_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "lightweight", "recommended_context": 32768, "estimated_download_bytes": 10947100928, "estimated_ram_bytes": 17000000000}
		]
	}
}`

const previewSnapshotRTX5090 = `{
	"hardware": {
		"platform": "Windows",
		"os_version": "11",
		"cpu_model": "AMD Ryzen 9 9950X",
		"logical_cpus": 32,
		"architecture": "amd64",
		"cpu_features": {"avx2": true, "avx512f": true},
		"memory_total_bytes": 128849018880,
		"memory_available_bytes": 105763569664,
		"disk_free_bytes": 450971566080,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [{
				"name": "NVIDIA GeForce RTX 5090",
				"vendor": "nvidia",
				"driver_version": "580.42",
				"compute_capability": "12.0",
				"memory_total_bytes": 34190983168
			}]
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "polystrata",
		"backend": "cuda",
		"model_id": "polystrata-qwen38-iq3-s",
		"model_title": "Qwen3.8-Flash-Next (IQ3_S)",
		"family": "qwen",
		"size": "IQ3_S",
		"engine_core": "strata",
		"model_format": "strata_pack",
		"fit_level": "fits",
		"tier": "best",
		"recommended_context": 131072,
		"estimated_download_bytes": 83600000000,
		"estimated_ram_bytes": 66571993088,
		"disk_free_bytes": 450971566080,
		"reason": "polystrata_hardware_fit",
		"gpu": {"name": "NVIDIA GeForce RTX 5090", "vendor": "nvidia", "vram_bytes": 34190983168},
		"notes": [],
		"alternatives": [
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen38-coder-iq1-m",
				"model_title": "Qwen3.8-Flash-Next Coder (IQ1_M)",
				"family": "coder",
				"size": "IQ1_M",
				"engine_core": "strata",
				"model_format": "strata_pack",
				"fit_level": "fits",
				"tier": "balanced",
				"recommended_context": 131072,
				"estimated_download_bytes": 58400000000,
				"estimated_ram_bytes": 34359738368
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen36-ud-q4-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
				"family": "qwen36",
				"size": "UD-Q4_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "lightweight",
				"recommended_context": 131072,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 10737418240
			},
			{
				"engine": "llama-cpp",
				"backend": "cuda",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "fallback",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944
			}
		],
		"polystrata_models": [
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen38-iq3-s", "model_title": "Qwen3.8-Flash-Next (IQ3_S)", "family": "qwen", "size": "IQ3_S", "engine_core": "strata", "model_format": "strata_pack", "fit_level": "fits", "tier": "best", "recommended_context": 131072, "estimated_download_bytes": 83600000000, "estimated_ram_bytes": 66571993088},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen38-iq3-xxs", "model_title": "Qwen3.8-Flash-Next (IQ3_XXS)", "family": "qwen", "size": "IQ3_XXS", "engine_core": "strata", "model_format": "strata_pack", "fit_level": "fits", "recommended_context": 131072, "estimated_download_bytes": 75800000000, "estimated_ram_bytes": 64424509440},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen38-q2-0", "model_title": "Qwen3.8-Flash-Next (Q2_0)", "family": "qwen", "size": "Q2_0", "engine_core": "strata", "model_format": "strata_pack", "fit_level": "fits", "recommended_context": 131072, "estimated_download_bytes": 66400000000, "estimated_ram_bytes": 51539607552},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen38-coder-iq1-m", "model_title": "Qwen3.8-Flash-Next Coder (IQ1_M)", "family": "coder", "size": "IQ1_M", "engine_core": "strata", "model_format": "strata_pack", "fit_level": "fits", "tier": "balanced", "recommended_context": 131072, "estimated_download_bytes": 58400000000, "estimated_ram_bytes": 34359738368},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen36-ud-q4-k-m", "model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)", "family": "qwen36", "size": "UD-Q4_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "lightweight", "recommended_context": 131072, "estimated_download_bytes": 22663387424, "estimated_ram_bytes": 10737418240},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-flint-ud-q4-k-xl", "model_title": "Flint 40B-A2.5B (UD-Q4_K_XL)", "family": "flint", "size": "UD-Q4_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 131072, "estimated_download_bytes": 25054688704, "estimated_ram_bytes": 10737418240},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-gemma4-ud-q3-k-m", "model_title": "Gemma 4 26B-A4B (UD-Q3_K_M)", "family": "gemma4", "size": "UD-Q3_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 131072, "estimated_download_bytes": 13190264704, "estimated_ram_bytes": 10737418240},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-lfm25-ud-q4-k-xl", "model_title": "LFM2.5-24B-A2B (UD-Q4_K_XL)", "family": "lfm25", "size": "UD-Q4_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 131072, "estimated_download_bytes": 14876901632, "estimated_ram_bytes": 10737418240}
		]
	}
}`

const previewSnapshotRTX4070 = `{
	"hardware": {
		"platform": "Windows",
		"os_version": "11",
		"cpu_model": "Intel Core i7-14700H",
		"logical_cpus": 20,
		"architecture": "amd64",
		"cpu_features": {"avx2": true, "avx512f": false},
		"memory_total_bytes": 25769803776,
		"memory_available_bytes": 15032385536,
		"disk_free_bytes": 102005473280,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [{
				"name": "NVIDIA GeForce RTX 4070 Laptop GPU",
				"vendor": "nvidia",
				"driver_version": "580.42",
				"compute_capability": "8.9",
				"memory_total_bytes": 8585740288
			}]
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "polystrata",
		"backend": "cuda",
		"model_id": "polystrata-qwen36-ud-q4-k-m",
		"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
		"family": "qwen36",
		"size": "UD-Q4_K_M",
		"engine_core": "strata-poly",
		"model_format": "gguf",
		"fit_level": "tight",
		"tier": "best",
		"recommended_context": 32768,
		"estimated_download_bytes": 22663387424,
		"estimated_ram_bytes": 26600000000,
		"disk_free_bytes": 102005473280,
		"reason": "polystrata_hardware_fit",
		"gpu": {"name": "NVIDIA GeForce RTX 4070 Laptop GPU", "vendor": "nvidia", "vram_bytes": 8585740288},
		"notes": ["host_available_memory_tight"],
		"alternatives": [
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen36-ud-q3-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)",
				"family": "qwen36",
				"size": "UD-Q3_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "balanced",
				"recommended_context": 32768,
				"estimated_download_bytes": 17104402720,
				"estimated_ram_bytes": 21100000000
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-lfm25-ud-q3-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)",
				"family": "lfm25",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "lightweight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 15800000000
			},
			{
				"engine": "llama-cpp",
				"backend": "cuda",
				"model_id": "model-qwen3-4b-q4km",
				"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "fallback",
				"recommended_context": 8192,
				"estimated_download_bytes": 2497280256,
				"estimated_ram_bytes": 6442450944
			}
		],
		"polystrata_models": [
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen36-ud-q4-k-m", "model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)", "family": "qwen36", "size": "UD-Q4_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "tight", "tier": "best", "recommended_context": 32768, "estimated_download_bytes": 22663387424, "estimated_ram_bytes": 26600000000},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-qwen36-ud-q3-k-m", "model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)", "family": "qwen36", "size": "UD-Q3_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "balanced", "recommended_context": 32768, "estimated_download_bytes": 17104402720, "estimated_ram_bytes": 21100000000},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-gemma4-ud-q3-k-m", "model_title": "Gemma 4 26B-A4B (UD-Q3_K_M)", "family": "gemma4", "size": "UD-Q3_K_M", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "recommended_context": 32768, "estimated_download_bytes": 13190264704, "estimated_ram_bytes": 17700000000},
			{"engine": "polystrata", "backend": "cuda", "model_id": "polystrata-lfm25-ud-q3-k-xl", "model_title": "LFM2.5-24B-A2B (UD-Q3_K_XL)", "family": "lfm25", "size": "UD-Q3_K_XL", "engine_core": "strata-poly", "model_format": "gguf", "fit_level": "fits", "tier": "lightweight", "recommended_context": 32768, "estimated_download_bytes": 10947100928, "estimated_ram_bytes": 15800000000}
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
		"cpu_features": {"avx2": true, "avx512f": false},
		"memory_total_bytes": 17179869184,
		"memory_available_bytes": 11166914969,
		"disk_free_bytes": 88046829568,
		"ac_online": false,
		"gpu": {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "recommended",
		"engine": "llama-cpp",
		"backend": "cpu",
		"model_id": "model-qwen3-4b-q4km",
		"model_title": "Qwen3-4B-Q4_K_M（GGUF）",
		"model_format": "gguf",
		"fit_level": "fits",
		"tier": "best",
		"recommended_context": 8192,
		"estimated_download_bytes": 2497280256,
		"estimated_ram_bytes": 6442450944,
		"disk_free_bytes": 88046829568,
		"reason": "fallback_llama_cpp_no_compatible_polystrata_gpu",
		"notes": [],
		"alternatives": [
			{
				"engine": "llama-cpp",
				"backend": "cpu",
				"model_id": "model-qwen3-0.6b-q8",
				"model_title": "Qwen3-0.6B-Q8_0（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "lightweight",
				"recommended_context": 8192,
				"estimated_download_bytes": 639446688,
				"estimated_ram_bytes": 2147483648
			},
			{
				"engine": "llama-cpp",
				"backend": "cpu",
				"model_id": "model-smollm2-135m",
				"model_title": "SmolLM2-135M-Instruct-Q4_K_M（GGUF）",
				"model_format": "gguf",
				"fit_level": "fits",
				"tier": "fallback",
				"recommended_context": 4096,
				"estimated_download_bytes": 105454144,
				"estimated_ram_bytes": 1073741824
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
		"disk_free_bytes": 1288490188,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [{"name": "Apple M3", "vendor": "apple", "arch_family": "Apple Silicon", "cores": 10, "metal_supported": true}]
		}
	},
	"starter_recommendation": {
		"definition": "starter-recommendation.v1",
		"status": "blocked",
		"disk_free_bytes": 1288490188,
		"reason": "insufficient_disk_space",
		"notes": []
	}
}`
