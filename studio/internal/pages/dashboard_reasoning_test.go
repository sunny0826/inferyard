package pages

import (
	"strings"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestLaunchSummaryLineShowsThinkingStateAcrossEngines(t *testing.T) {
	t.Cleanup(l10n.SetTestLocale(l10n.LanguageEn))

	base := &viewState{
		model: "Qwen3-4B",
		registry: core.Registry{
			Models: []core.ModelAsset{{Name: "Qwen3-4B", Format: "gguf", Available: true}},
			Engines: []core.EngineAsset{
				{Name: "prism", Kind: "legacy"},
				{Name: "llama-local", Kind: "local_installation", Family: "llama-cpp"},
				{Name: "lms", Kind: "local_installation", Family: "lmstudio"},
				{Name: "ps", Kind: "local_installation", Family: "polystrata"},
			},
		},
	}

	base.engine = "prism"
	base.reasoningMode = core.ReasoningModeOff
	if got := launchSummaryLine(base); !strings.Contains(got, "Thinking: Off") {
		t.Fatalf("prism off summary = %q, want Thinking: Off", got)
	}
	base.reasoningMode = core.ReasoningModeOn
	if got := launchSummaryLine(base); !strings.Contains(got, "Thinking: On") {
		t.Fatalf("prism on summary = %q, want Thinking: On", got)
	}

	base.engine = "llama-local"
	if got := launchSummaryLine(base); !strings.Contains(got, "Thinking: On") {
		t.Fatalf("llama-cpp on summary = %q, want Thinking: On", got)
	}
	req := base.localCandidateRequest([]string{"zh-core"}, nil)
	if req == nil || req.ReasoningMode != core.ReasoningModeOn || req.ReasoningEffort != "" {
		t.Fatalf("llama-cpp request = %+v, want ReasoningMode=on and empty ReasoningEffort", req)
	}

	base.engine = "lms"
	base.reasoningMode = core.ReasoningModeOff
	if got := launchSummaryLine(base); !strings.Contains(got, "Thinking: Off") {
		t.Fatalf("lmstudio off summary = %q, want Thinking: Off", got)
	}

	base.engine = "ps"
	base.reasoningEffort = core.ReasoningEffortHigh
	if got := launchSummaryLine(base); !strings.Contains(got, "Thinking: High") {
		t.Fatalf("polystrata high summary = %q, want Thinking: High", got)
	}
	psReq := base.localCandidateRequest([]string{"zh-core"}, nil)
	if psReq == nil || psReq.ReasoningEffort != core.ReasoningEffortHigh || psReq.ReasoningMode != "" {
		t.Fatalf("polystrata request = %+v, want ReasoningEffort=high and empty ReasoningMode", psReq)
	}
}
