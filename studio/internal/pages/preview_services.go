//go:build ui_debug && !release

package pages

import (
	"time"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func seedPreviewRecentServices(model, engine string) []core.ServicePreset {
	gpuAll := -1
	return []core.ServicePreset{
		{
			Model:         model,
			Engine:        engine,
			Port:          8080,
			ContextSize:   32768,
			GPULayers:     &gpuAll,
			LastStartedAt: "2026-11-24T10:15:00Z",
		},
		{
			Model:         "Preview-Small.gguf",
			Engine:        engine,
			Port:          0,
			ContextSize:   8192,
			LastStartedAt: "2026-11-23T08:00:00Z",
		},
	}
}

func previewServiceExitedScene(s *viewState) {
	s.selected = "services"
	code := 137
	gpuAll := -1
	s.exitedNotice = &core.ServiceExitNotice{
		ID:          "preview-exit-1",
		Model:       s.model,
		Engine:      s.engine,
		Port:        8080,
		ContextSize: 32768,
		GPULayers:   &gpuAll,
		LogPath:     "/preview/logs/standalone.log",
		ExitedAt:    "2026-11-25T05:42:10Z",
		ExitCode:    &code,
		WhileAway:   true,
		LogTail: []string{
			"llama_new_context_with_model: kv_size = 32768",
			"ggml_metal_buffer_init: failed to allocate buffer (out of memory)",
			"process terminated with signal SIGKILL",
		},
		Hint: l10n.TR("core.service.exit_hint.oom"),
	}
}

func (p *previewSession) savePreviewServicePreset(preset core.ServicePreset) {
	if preset.LastStartedAt == "" {
		preset.LastStartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	p.state.upsertRecentServiceLocal(preset)
}

func (p *previewSession) dismissPreviewExitNotice() {
	p.state.exitedNotice = nil
}

func (p *previewSession) testPreviewServiceEndpoint(op previewTestServiceEndpoint) {
	s := p.state
	fail := p.takeFailure()
	p.after("service-test", 250*time.Millisecond, func() {
		if s.serviceUsage.testSeq != op.seq {
			return
		}
		s.serviceUsage.testing = false
		if fail {
			s.serviceUsage.testResult = &core.ServiceTestResult{
				OK:         false,
				StatusCode: 503,
				LatencyMs:  312,
				Error:      "service unavailable",
			}
			return
		}
		s.serviceUsage.testResult = &core.ServiceTestResult{
			OK:         true,
			StatusCode: 200,
			LatencyMs:  128,
			Reply:      "OK",
		}
	})
}
