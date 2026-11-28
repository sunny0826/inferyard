package desktop

import (
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/remote"
)

func TestTrayTunnelProjectionAndActions(t *testing.T) {
	closed := ""
	tunnels := ProjectTrayTunnels([]remote.TunnelInfo{{
		TunnelID: "tunnel-1", NodeName: "Win-RTX4090", ModelName: "Qwen3", LocalURL: "http://127.0.0.1:18001/v1",
	}}, func(id string) *core.ApiError { closed = id; return nil })
	if len(tunnels) != 1 || tunnels[0].OnCopy == nil || tunnels[0].OnClose == nil {
		t.Fatalf("unexpected tunnel projection: %+v", tunnels)
	}
	tunnels[0].OnClose()
	if closed != "tunnel-1" {
		t.Fatalf("closed %q", closed)
	}
	copied := false
	tunnels[0].OnCopy = func() { copied = true }
	status := TrayEngineStatus{Tunnels: tunnels}
	if !status.HasActive() {
		t.Fatal("active tunnel should keep tray resident")
	}
	menu := buildTrayMenu(status, func() {}, func() {})
	foundCopy := false
	for _, item := range menu.Items() {
		if strings.Contains(item.Label, "http://127.0.0.1:18001/v1") && item.Click != nil {
			foundCopy = true
			item.Click(item, (*mygo.Window)(nil))
		}
	}
	if !foundCopy || !copied {
		t.Fatal("tray copy action did not invoke callback")
	}
}

func TestTrayTunnelTooltipDoesNotMutateEngineSummary(t *testing.T) {
	status := TrayEngineStatus{Tunnels: []TrayTunnelSummary{{ModelName: "Qwen3", LocalURL: "http://127.0.0.1:18001/v1"}}}
	if !strings.Contains(trayToolTip(status), status.Tunnels[0].LocalURL) || trayTitle(status) == "" {
		t.Fatalf("missing tunnel status: title=%q tooltip=%q", trayTitle(status), trayToolTip(status))
	}
}
