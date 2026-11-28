//go:build ui_debug && !release

package pages

const previewSnapshotRTX4070 = `{
	"hardware": {
		"platform": "Windows",
		"os_version": "11",
		"cpu_model": "Intel Core i7-14700H",
		"logical_cpus": 20,
		"architecture": "amd64",
		"cpu_features": {
			"avx2": true,
			"avx512f": false
		},
		"memory_total_bytes": 25769803776,
		"memory_available_bytes": 15032385536,
		"disk_free_bytes": 102005473280,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [
				{
					"name": "NVIDIA GeForce RTX 4070 Laptop GPU",
					"vendor": "nvidia",
					"driver_version": "580.42",
					"compute_capability": "8.9",
					"memory_total_bytes": 8585740288
				}
			]
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
		"recommended_context": 32768,
		"estimated_download_bytes": 22663387424,
		"estimated_ram_bytes": 26560846284,
		"tier": "best",
		"disk_free_bytes": 102005473280,
		"reason": "polystrata_hardware_fit",
		"gpu": {
			"vendor": "nvidia",
			"name": "NVIDIA GeForce RTX 4070 Laptop GPU",
			"vram_bytes": 8585740288
		},
		"notes": [
			"host_available_memory_tight"
		],
		"alternatives": [
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-flint-ud-q3-k-xl",
				"model_title": "Flint 40B-A2.5B (UD-Q3_K_XL)",
				"family": "flint",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 18494977472,
				"estimated_ram_bytes": 21994478643,
				"tier": "balanced"
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
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 15801852825,
				"tier": "lightweight"
			},
			{
				"engine": "llama-cpp",
				"backend": "cuda",
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
				"backend": "cuda",
				"model_id": "polystrata-qwen36-ud-q4-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q4_K_M)",
				"family": "qwen36",
				"size": "UD-Q4_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 26560846284,
				"tier": "best"
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen36-ud-q3-k-m",
				"model_title": "Qwen3.6-35B-A3B (UD-Q3_K_M)",
				"family": "qwen36",
				"size": "UD-Q3_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 17104402720,
				"estimated_ram_bytes": 21168220467
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-flint-ud-q3-k-xl",
				"model_title": "Flint 40B-A2.5B (UD-Q3_K_XL)",
				"family": "flint",
				"size": "UD-Q3_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 18494977472,
				"estimated_ram_bytes": 21994478643,
				"tier": "balanced"
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-gemma4-ud-q3-k-m",
				"model_title": "Gemma 4 26B-A4B (UD-Q3_K_M)",
				"family": "gemma4",
				"size": "UD-Q3_K_M",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 13190264704,
				"estimated_ram_bytes": 17941962291
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen36-ud-q2-k-xl",
				"model_title": "Qwen3.6-35B-A3B (UD-Q2_K_XL)",
				"family": "qwen36",
				"size": "UD-Q2_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 12574128416,
				"estimated_ram_bytes": 16723975372
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-lfm25-ud-q4-k-xl",
				"model_title": "LFM2.5-24B-A2B (UD-Q4_K_XL)",
				"family": "lfm25",
				"size": "UD-Q4_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 14876901632,
				"estimated_ram_bytes": 19816601190
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
				"fit_level": "tight",
				"recommended_context": 32768,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 15801852825,
				"tier": "lightweight"
			}
		]
	}
}`
