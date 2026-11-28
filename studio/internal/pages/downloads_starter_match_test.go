package pages

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestStarterPolystrataNormalizedInstalledMatch(t *testing.T) {
	s := &viewState{
		registry: core.Registry{
			Engines: []core.EngineAsset{
				{Name: "polystrata-server", Family: "polystrata"},
			},
			Models: []core.ModelAsset{
				{
					Name:      "Qwen3.6-35B-A3B-UD-Q4_K_M.json",
					Path:      "/models/Qwen3.6-35B-A3B-UD-Q4_K_M.json",
					Format:    "strata_pack",
					Available: true,
				},
			},
		},
	}

	opt := starterOption{
		Engine:  "polystrata",
		Backend: "metal",
		ModelID: "polystrata-qwen36-ud-q4-k-m",
		Family:  "qwen36",
		Size:    "UD-Q4_K_M",
	}
	if !s.isStarterOptionInstalled(opt) {
		t.Fatalf("expected qwen36 UD-Q4_K_M to match normalized Qwen3.6-35B-A3B-UD-Q4_K_M asset")
	}

	other := starterOption{
		Engine:  "polystrata",
		Backend: "metal",
		ModelID: "polystrata-qwen36-ud-q2-k-xl",
		Family:  "qwen36",
		Size:    "UD-Q2_K_XL",
	}
	if s.isStarterOptionInstalled(other) {
		t.Fatalf("did not expect UD-Q2_K_XL to match UD-Q4_K_M asset")
	}
}

func TestStarterMlxInstalledMatch(t *testing.T) {
	s := &viewState{
		registry: core.Registry{
			Engines: []core.EngineAsset{
				{Name: "mlx-lm", Family: "mlx-lm"},
			},
			Models: []core.ModelAsset{
				{
					Name:      "Qwen3-0.6B-4bit",
					Path:      "/models/mlx-community/Qwen3-0.6B-4bit",
					Format:    "directory",
					Available: true,
				},
			},
		},
	}
	opt := starterOption{
		Engine:  "mlx-lm",
		Backend: "metal",
		ModelID: "model-qwen3-0.6b-mlx",
	}
	if !s.isStarterEngineInstalled(opt) {
		t.Fatalf("expected mlx-lm engine to be detected as installed")
	}
	if !s.isStarterModelInstalled(opt) {
		t.Fatalf("expected model-qwen3-0.6b-mlx directory model to be detected as installed")
	}
	if !s.isStarterOptionInstalled(opt) {
		t.Fatalf("expected mlx-lm starter option to be detected as installed")
	}
}

func TestOpenDownloadsWithStarterExpandedSelectsRecommendedTab(t *testing.T) {
	s := &viewState{}
	s.openDownloadsWithStarterExpanded()
	if s.selected != "library" || s.libraryTab != libraryTabRecommended {
		t.Fatalf("got selected=%q libraryTab=%d, want library / %d", s.selected, s.libraryTab, libraryTabRecommended)
	}
}

func TestDashboardStarterGoDownloadsNavigatesToRecommendedTab(t *testing.T) {
	s := &viewState{selected: "benchmarks"}
	s.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "llama-cpp",
			"backend": "metal",
			"model_id": "model-qwen3-4b-q4km",
			"model_title": "Qwen3-4B-Q4_K_M (GGUF)",
			"family": "llama-cpp",
			"model_format": "gguf",
			"fit_level": "fits",
			"tier": "best"
		}
	}`)
	tt := ui.NewTester(func(c *ui.Context) {
		s.renderStarterRecommendationPanel(c, "dashboard")
	}, 1200, 900)
	tt.SetLocale(l10n.LanguageZhHans)
	tt.Frame()
	click(t, tt, l10n.T(l10n.LanguageZhHans, "downloads.starter.go_downloads"))
	if s.selected != "library" || s.libraryTab != libraryTabRecommended {
		t.Fatalf("dashboard go_downloads navigated to selected=%q libraryTab=%d, want library / %d", s.selected, s.libraryTab, libraryTabRecommended)
	}
}
