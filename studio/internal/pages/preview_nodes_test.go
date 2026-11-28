//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

func TestPreviewNodeScenesStayInMemory(t *testing.T) {
	home := isolatePreviewHome(t)
	p := newPreviewSession(context.Background())
	tt := previewTester(p, 1200, 2200)

	p.loadScene(previewSceneIndex(t, "节点 · 一行命令配对"))
	tt.Frame()
	if len(p.state.nodes.snapshots) != 3 || !tt.HasText("839-204") || p.state.service != nil {
		t.Fatalf("pairing scene missing nodes or code: %+v", p.state.nodes)
	}

	p.loadScene(previewSceneIndex(t, "节点 · 配对确认卡"))
	tt.Frame()
	click(t, tt, "确认接入并进入该机器")
	if p.state.nodes.activeID != previewWinNodeID || p.state.nodes.addOpen {
		t.Fatalf("pairing approval did not open approved node: active=%q open=%v", p.state.nodes.activeID, p.state.nodes.addOpen)
	}

	p.loadScene(previewSceneIndex(t, "节点 · SSH HostKey 三选一"))
	tt.Frame()
	if !tt.HasText("SHA256:7m2K9previewFingerprint") {
		t.Fatalf("host key fingerprint missing: %v", tt.Texts())
	}
	click(t, tt, "仅本次信任")
	if len(p.state.nodes.hostKeyRequests) != 0 {
		t.Fatal("host key decision did not resolve pending request")
	}

	p.loadScene(previewSceneIndex(t, "节点 · 远端服务与本地映射"))
	tt.Frame()
	if !tt.HasText("http://127.0.0.1:18001/v1") || len(p.state.nodes.tunnels) != 2 {
		t.Fatalf("remote tunnels missing: %v", tt.Texts())
	}
	localServices := append([]core.EngineState(nil), p.state.managedServices...)
	p.state.selectViewNode(core.LocalNodeID)
	p.state.selectViewNode(previewWinNodeID)
	if len(p.state.nodes.tunnels) != 2 || len(localServices) != len(p.state.managedServices) {
		t.Fatal("preview node switching interrupted services or tunnels")
	}
	p.state.closeTunnel("tunnel-cloud")
	if len(p.state.nodes.tunnels) != 1 {
		t.Fatal("preview close tunnel did not stay in memory")
	}

	p.loadScene(previewSceneIndex(t, "报告 · 跨机同屏并排对比"))
	tt.Frame()
	comparison := p.state.nodes.comparison
	if comparison == nil || comparison.Runs[0].Right.PeakTemperatureCelsius != nil {
		t.Fatalf("expected missing Windows temperature: %+v", comparison)
	}
	if !tt.HasText(compareMissing) {
		t.Fatalf("missing metric was not rendered as em dash: %v", tt.Texts())
	}
	assertPreviewHomeClean(t, home)
}

func TestPreviewNodeDecisionHooksReceiveExplicitOptions(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "节点 · 配对确认卡"))
	var pairing remote.PairingDecision
	p.state.nodes.decidePairingFn = func(decision remote.PairingDecision) (core.NodeRecord, *core.ApiError) {
		pairing = decision
		return core.NodeRecord{NodeID: previewWinNodeID}, nil
	}
	p.state.submitPairingDecision(previewPairingRequest(), remote.PairingApprove)
	if pairing.Option != remote.PairingApprove {
		t.Fatalf("unexpected preview pairing option: %+v", pairing)
	}

	p.loadScene(previewSceneIndex(t, "节点 · SSH HostKey 三选一"))
	var hostKey remote.HostKeyDecision
	p.state.nodes.decideHostKeyFn = func(decision remote.HostKeyDecision) *core.ApiError {
		hostKey = decision
		return nil
	}
	p.state.submitHostKeyDecision(previewHostKeyRequest(), remote.HostKeyTrustAndSave)
	if hostKey.Option != remote.HostKeyTrustAndSave {
		t.Fatalf("unexpected preview host key option: %+v", hostKey)
	}
}
