package pages

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type drawerPreviewStub struct {
	intents []previewIntent
}

func (p *drawerPreviewStub) showNotice()                                       {}
func (p *drawerPreviewStub) dispatch(i previewIntent)                          { p.intents = append(p.intents, i) }
func (p *drawerPreviewStub) resetTask(string, string)                          {}
func (p *drawerPreviewStub) report(string) (*core.ReportIndex, *core.ApiError) { return nil, nil }
func (p *drawerPreviewStub) detail(string) *core.RunTaskDetail                 { return nil }
func (p *drawerPreviewStub) detectedEngines() []inferyard.DetectedEngine       { return nil }
func (p *drawerPreviewStub) downloadLogTail(string) string                     { return "" }
func (p *drawerPreviewStub) resolveModelURL(string) inferyard.ModelResolve {
	return inferyard.ModelResolve{}
}

func TestPolystrataDrawerBlocksIncompatibleHardwareSnapshot(t *testing.T) {
	s := testState()
	s.device.Snapshot = json.RawMessage(`{
		"starter_recommendation": {
			"definition": "starter-recommendation.v1",
			"status": "recommended",
			"engine": "llama-cpp",
			"backend": "metal",
			"model_id": "model-qwen3-0.6b-q8",
			"model_title": "Qwen3-0.6B-Q8_0 (GGUF)",
			"fit_level": "fits",
			"recommended_context": 8192,
			"estimated_download_bytes": 639446688,
			"reason": "fallback_llama_cpp_insufficient_ram_for_polystrata"
		}
	}`)
	stub := &drawerPreviewStub{}
	s.preview = stub
	st := s.ensureDownloadsUI()
	st.hostPlatform = "darwin/arm64"
	tt := ui.NewTester(s.downloadsPageView, 1120, 2600)

	click(t, tt, "安装 · polystrata")
	if !st.engineDrawerOpen {
		t.Fatal("expected PolyStrata drawer to open")
	}
	if !tt.HasText("当前内存不足以运行 PolyStrata 模型") {
		t.Fatalf("expected localized fallback reason in drawer: %v", tt.Texts())
	}
	if tt.HasText("Qwen3.6-35B-A3B") || tt.HasText("Qwen3.8-Flash-Next") {
		t.Fatalf("drawer must not re-inject oversized PolyStrata models excluded by snapshot: %v", tt.Texts())
	}
	if tt.HasText("复制命令") || tt.HasText("inferyard engine prepare-polystrata") {
		t.Fatalf("drawer must hide fallback CLI command and copy button when no PolyStrata model fits: %v", tt.Texts())
	}
	info, ok := tt.AccessByRole(ui.RoleButton, "安装此版本 · polystrata")
	if !ok || !info.Disabled {
		t.Fatalf("confirm install button must be disabled when snapshot has no fitting PolyStrata model: ok=%v info=%+v", ok, info)
	}
	click(t, tt, "安装此版本 · polystrata")
	if len(stub.intents) != 0 || !st.engineDrawerOpen {
		t.Fatalf("disabled confirm button must not dispatch setup: intents=%#v open=%v", stub.intents, st.engineDrawerOpen)
	}
}
