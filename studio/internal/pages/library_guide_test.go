package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestLibraryHidesReadyTaskGuideBanner(t *testing.T) {
	s := readyState("library")
	s.task.stage = "prepare_candidate"
	s.registry = core.Registry{
		Models:  []core.ModelAsset{{Name: "qwen", Format: "gguf", Available: true}},
		Engines: []core.EngineAsset{{Name: "llama-cpp"}},
	}

	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	if got := s.downloadGuideStatus(); got != "" {
		t.Fatalf("downloadGuideStatus() = %q, want empty when model and engine are ready", got)
	}
	if _, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("onboarding.guide.back_dashboard")); ok {
		t.Fatalf("expected library view to omit back-to-tasks guide banner when assets are ready")
	}
}

func TestLibraryShowsTaskGuideWhenMissingOrDownloading(t *testing.T) {
	t.Run("missing assets", func(t *testing.T) {
		s := readyState("library")
		s.task.stage = "prepare_candidate"

		tt := ui.NewTester(s.view, 1120, 900)
		tt.Frame()

		if got := s.downloadGuideStatus(); got != l10n.TR("onboarding.guide.prompt") {
			t.Fatalf("downloadGuideStatus() = %q, want prompt", got)
		}
		if _, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("onboarding.guide.back_dashboard")); !ok {
			t.Fatalf("expected library view to show back-to-tasks button when assets are missing")
		}
	})

	t.Run("active download with ready assets", func(t *testing.T) {
		s := readyState("library")
		s.task.stage = "prepare_candidate"
		s.registry = core.Registry{
			Models:  []core.ModelAsset{{Name: "qwen", Format: "gguf", Available: true}},
			Engines: []core.EngineAsset{{Name: "llama-cpp"}},
		}
		s.downloads = []core.DownloadTask{{TaskId: "dl-1", Kind: "model", State: "downloading"}}

		tt := ui.NewTester(s.view, 1120, 900)
		tt.Frame()

		if got := s.downloadGuideStatus(); got == "" {
			t.Fatalf("expected non-empty downloadGuideStatus while download is active")
		}
		if _, ok := tt.AccessByRole(ui.RoleButton, l10n.TR("onboarding.guide.back_dashboard")); !ok {
			t.Fatalf("expected library view to show back-to-tasks button while downloading")
		}
	})
}
