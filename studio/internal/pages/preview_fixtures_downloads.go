//go:build ui_debug && !release

package pages

import (
	"path/filepath"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func previewDownloadsPolystrata(s *viewState) {
	s.selected = "library"
	s.libraryTab = 1
	s.registry.Engines = nil
	s.registry.Models = nil
	blockerErr := core.Err("polystrata_preflight_blocked")
	blockerErr.Details = map[string]any{
		"blockers": []map[string]any{{
			"code":        "polystrata_build_tools_missing",
			"message":     l10n.TR("preview.polystrata.blocker.build_tools"),
			"fix_command": "brew install cmake ninja",
		}},
	}
	netErr := core.Err("polystrata_setup_failed")
	netErr.Limitations = []string{"engine_acquire_incomplete"}
	netErr.Details = map[string]any{
		"failure_stage": "downloading_runtime",
		"failure_cause": "network_timeout",
	}
	s.downloads = []core.DownloadTask{
		{
			TaskId:      "preview-polystrata-active",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen36 · UD-Q4_K_M",
			State:       "downloading",
			Stage:       "setup_engine",
			Step:        previewValue(3),
			StepTotal:   previewValue(4),
			Percent:     previewValue(62.0),
			Detail:      previewValue(l10n.TR("preview.polystrata.stage.build_metal_inference")),
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/d62dfacbe185dfbb447e79c5fb1091ae189afc64.tar.gz",
			OutName:     "polystrata-preview",
			OutDir:      "/preview/engines/polystrata-preview",
			LogPath:     previewValue("/preview/engines/polystrata-preview/.inferyard-setup.log"),
			StartedAt:   "2026-10-17T00:00:00Z",
		},
		{
			TaskId:      "preview-polystrata-blocked",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · coder · IQ1_M",
			State:       "failed",
			Stage:       "preflight",
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/d62dfacbe185dfbb447e79c5fb1091ae189afc64.tar.gz",
			OutName:     "polystrata-blocked",
			OutDir:      "/preview/engines/polystrata-blocked",
			Error:       blockerErr,
			StartedAt:   "2026-10-16T23:55:00Z",
		},
		{
			TaskId:      "preview-polystrata-net-timeout",
			Kind:        "polystrata",
			DisplayName: "PolyStrata · qwen · Q2_0",
			State:       "failed",
			Stage:       "downloading_runtime",
			SourceUrl:   "https://github.com/simplexai-labs/PolyStrata/archive/d62dfacbe185dfbb447e79c5fb1091ae189afc64.tar.gz",
			OutName:     "polystrata-net-timeout",
			OutDir:      "/preview/engines/polystrata-net-timeout",
			Error:       netErr,
			StartedAt:   "2026-10-16T23:52:00Z",
		},
	}
}

func previewDownloadsPolystrataModel(s *viewState) {
	s.selected = "library"
	s.libraryTab = 1
	s.registry.Engines = nil
	s.registry.Models = nil
	totalBytes := int64(66400000000) // 61.84 GiB strata_pack
	s.downloads = []core.DownloadTask{
		{
			TaskId:          "preview-polystrata-model-live",
			Kind:            "polystrata",
			DisplayName:     "PolyStrata · qwen · Q2_0",
			State:           "downloading",
			Stage:           "downloading_model",
			Step:            previewValue(4),
			StepTotal:       previewValue(4),
			DownloadedBytes: 7856028569, // 7.32 GiB
			TotalBytes:      previewValue(totalBytes),
			BytesPerSecond:  previewValue(22502441.0), // 21.46 MiB/s
			Detail:          previewValue("Qwen3.8-Flash-Next-Q2_0-00001-of-00002.strata"),
			SourceUrl:       "https://github.com/simplexai-labs/PolyStrata/archive/d62dfacbe185dfbb447e79c5fb1091ae189afc64.tar.gz",
			OutName:         "polystrata-qwen-q2",
			OutDir:          "/preview/engines/polystrata-qwen-q2",
			LogPath:         previewValue("/preview/engines/polystrata-qwen-q2/.inferyard-setup.log"),
			StartedAt:       "2026-10-17T00:00:00Z",
		},
	}
}

func previewDownloadsActiveAndFailed(s *viewState) {
	s.selected = "library"
	s.libraryTab = 0
	ws := emptyText(s.settings.Effective.WorkspaceRoot)
	modelsRoot := emptyText(s.settings.Effective.ModelsRoot)
	engineOutDir := "/preview/engines/preview-engine"
	if ws != "" {
		engineOutDir = filepath.Join(ws, "engines", "preview-engine")
	}
	modelOutDir := "/preview/models/preview-model"
	if modelsRoot != "" {
		modelOutDir = filepath.Join(modelsRoot, "preview-model")
	}
	engineTotal := int64(11517506)
	s.downloads = []core.DownloadTask{
		{
			TaskId:          "preview-engine",
			Kind:            "engine",
			DisplayName:     "llama.cpp · prism-b10743-adfffbe",
			State:           "downloading",
			Stage:           "downloading",
			SourceUrl:       core.DefaultEngineURL(),
			OutName:         "preview-engine",
			OutDir:          engineOutDir,
			DownloadedBytes: engineTotal * 3 / 5,
			TotalBytes:      previewValue(engineTotal),
			BytesPerSecond:  previewValue(18.5 * 1024 * 1024),
			StartedAt:       "2026-10-17T00:00:00Z",
		},
		{
			TaskId:    "preview-model",
			Kind:      "model",
			State:     "failed",
			OutName:   "preview-model",
			OutDir:    modelOutDir,
			SourceUrl: "https://example.invalid/preview.gguf",
			Error:     core.Err("download_failed"),
		},
	}
}
