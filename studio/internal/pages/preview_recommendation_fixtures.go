//go:build ui_debug && !release

package pages

const previewSnapshotRTX5090 = `{
	"hardware": {
		"platform": "Windows",
		"os_version": "11",
		"cpu_model": "AMD Ryzen 9 9950X",
		"logical_cpus": 32,
		"architecture": "amd64",
		"cpu_features": {
			"avx2": true,
			"avx512f": true
		},
		"memory_total_bytes": 128849018880,
		"memory_available_bytes": 105763569664,
		"disk_free_bytes": 450971566080,
		"ac_online": true,
		"gpu": {
			"status": "observed",
			"devices": [
				{
					"name": "NVIDIA GeForce RTX 5090",
					"vendor": "nvidia",
					"driver_version": "580.42",
					"compute_capability": "12.0",
					"memory_total_bytes": 34190983168
				}
			]
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
		"recommended_context": 131072,
		"estimated_download_bytes": 83600000000,
		"estimated_ram_bytes": 66571993088,
		"tier": "best",
		"disk_free_bytes": 450971566080,
		"reason": "polystrata_hardware_fit",
		"gpu": {
			"vendor": "nvidia",
			"name": "NVIDIA GeForce RTX 5090",
			"vram_bytes": 34190983168
		},
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
				"recommended_context": 131072,
				"estimated_download_bytes": 58400000000,
				"estimated_ram_bytes": 34359738368,
				"tier": "balanced"
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
				"recommended_context": 131072,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 10000000000,
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
				"model_id": "polystrata-qwen38-iq3-s",
				"model_title": "Qwen3.8-Flash-Next (IQ3_S)",
				"family": "qwen",
				"size": "IQ3_S",
				"engine_core": "strata",
				"model_format": "strata_pack",
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 83600000000,
				"estimated_ram_bytes": 66571993088,
				"tier": "best"
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen38-iq3-xxs",
				"model_title": "Qwen3.8-Flash-Next (IQ3_XXS)",
				"family": "qwen",
				"size": "IQ3_XXS",
				"engine_core": "strata",
				"model_format": "strata_pack",
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 75800000000,
				"estimated_ram_bytes": 64424509440
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-qwen38-q2-0",
				"model_title": "Qwen3.8-Flash-Next (Q2_0)",
				"family": "qwen",
				"size": "Q2_0",
				"engine_core": "strata",
				"model_format": "strata_pack",
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 66400000000,
				"estimated_ram_bytes": 51539607552
			},
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
				"recommended_context": 131072,
				"estimated_download_bytes": 58400000000,
				"estimated_ram_bytes": 34359738368,
				"tier": "balanced"
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
				"recommended_context": 131072,
				"estimated_download_bytes": 22663387424,
				"estimated_ram_bytes": 10000000000,
				"tier": "lightweight"
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
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 17104402720,
				"estimated_ram_bytes": 10000000000
			},
			{
				"engine": "polystrata",
				"backend": "cuda",
				"model_id": "polystrata-flint-ud-q4-k-xl",
				"model_title": "Flint 40B-A2.5B (UD-Q4_K_XL)",
				"family": "flint",
				"size": "UD-Q4_K_XL",
				"engine_core": "strata-poly",
				"model_format": "gguf",
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 25054688704,
				"estimated_ram_bytes": 10000000000
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
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 18494977472,
				"estimated_ram_bytes": 10000000000
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
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 13190264704,
				"estimated_ram_bytes": 10000000000
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
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 12574128416,
				"estimated_ram_bytes": 10000000000
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
				"fit_level": "fits",
				"recommended_context": 131072,
				"estimated_download_bytes": 14876901632,
				"estimated_ram_bytes": 10000000000
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
				"recommended_context": 131072,
				"estimated_download_bytes": 10947100928,
				"estimated_ram_bytes": 10000000000
			}
		]
	}
}`

