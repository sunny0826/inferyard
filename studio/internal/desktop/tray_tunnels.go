package desktop

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/remote"
)

type TrayTunnelSummary struct {
	TunnelID  string
	NodeName  string
	ModelName string
	LocalURL  string
	OnCopy    func()
	OnClose   func()
}

func ProjectTrayTunnels(tunnels []remote.TunnelInfo, closeTunnel func(string) *core.ApiError) []TrayTunnelSummary {
	out := make([]TrayTunnelSummary, 0, len(tunnels))
	for _, tunnel := range tunnels {
		tunnel := tunnel
		item := TrayTunnelSummary{
			TunnelID: tunnel.TunnelID, NodeName: tunnel.NodeName, ModelName: tunnel.ModelName, LocalURL: tunnel.LocalURL,
			OnCopy: func() { mygo.Clipboard.WriteText(tunnel.LocalURL) },
		}
		if closeTunnel != nil {
			item.OnClose = func() { _ = closeTunnel(tunnel.TunnelID) }
		}
		out = append(out, item)
	}
	return out
}

func appendTrayTunnelItems(items []*mygo.MenuItem, tunnels []TrayTunnelSummary) []*mygo.MenuItem {
	if len(tunnels) == 0 {
		return items
	}
	items = append(items, mygo.Separator(), &mygo.MenuItem{Label: l10n.TRf("nodes.tray.tunnels", "count", len(tunnels)), Disabled: true})
	for _, tunnel := range tunnels {
		items = append(items, &mygo.MenuItem{
			Label:    l10n.TRf("nodes.tray.tunnel_item", "node", tunnel.NodeName, "model", tunnel.ModelName, "url", tunnel.LocalURL),
			Disabled: true,
		})
		if tunnel.OnCopy != nil {
			items = append(items, &mygo.MenuItem{Label: l10n.TRf("nodes.tray.copy", "url", tunnel.LocalURL), Click: func(*mygo.MenuItem, *mygo.Window) { tunnel.OnCopy() }})
		}
		if tunnel.OnClose != nil {
			items = append(items, &mygo.MenuItem{Label: l10n.TRf("nodes.tray.close", "model", tunnel.ModelName), Click: func(*mygo.MenuItem, *mygo.Window) { tunnel.OnClose() }})
		}
	}
	return items
}

func trayTunnelTooltip(tunnels []TrayTunnelSummary) string {
	if len(tunnels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tunnels))
	for _, tunnel := range tunnels {
		parts = append(parts, fmt.Sprintf("%s %s", tunnel.ModelName, tunnel.LocalURL))
	}
	return strings.Join(parts, "\n")
}
