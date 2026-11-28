package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/remote"
)

func (s *viewState) tunnelRow(c *ui.Context, tunnel remote.TunnelInfo) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("tunnel-"+tunnel.TunnelID)).FillWidth().Gap(t.Density.Tight).
		Padding(t.Density.Row).Radius(t.Radius.Control), materialSunken).Children(func() {
		ui.Text(c, tunnel.NodeName+" · "+tunnel.ModelName).Bold().FontSize(t.Type.Caption).SingleLine()
		ui.Text(c, fmt.Sprintf(":%d → %s", tunnel.RemotePort, tunnel.LocalURL)).FontSize(t.Type.Caption).FontFeatures("tnum").Selectable()
		studioTooltipGroup(c, ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row), func() {
			if outlineIconButton(c, reicon.Copy, tr(c, "nodes.tunnels.copy")).Label(tr(c, "nodes.tunnels.copy") + " · " + tunnel.ModelName).Clicked() {
				s.copyTunnelEndpoint(c, tunnel.LocalURL)
			}
			if outlineIconButton(c, reicon.Unlink, tr(c, "nodes.tunnels.close")).Label(tr(c, "nodes.tunnels.close") + " · " + tunnel.ModelName).Clicked() {
				s.closeTunnel(tunnel.TunnelID)
			}
		})
	})
}

func (s *viewState) copyTunnelEndpoint(c *ui.Context, endpoint string) {
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "nodes.tunnels.copied_preview"))
		return
	}
	c.WriteClipboardThen(endpoint, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("nodes.tunnels.copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("nodes.tunnels.copied"))
	})
}

func (s *viewState) remoteTunnelBar(c *ui.Context, nodeID string, serviceID string, port int) {
	t := tokens(c)
	tunnel := s.tunnelForService(nodeID, serviceID)
	enabled := tunnel != nil
	before := enabled
	description := tr(c, "nodes.tunnels.disabled_desc")
	if tunnel != nil {
		description = tunnel.LocalURL
	}
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Density.Row).
		Radius(t.Radius.Control), materialSunken).Children(func() {
		labeledSwitchRow(c, tr(c, "nodes.tunnels.map_action"), description, tr(c, "nodes.tunnels.map_action"), &enabled)
		if tunnel != nil {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, tunnel.LocalURL).FontSize(t.Type.Caption).FontFeatures("tnum").Selectable().Grow(1)
				if outlineIconButton(c, reicon.Copy, tr(c, "nodes.tunnels.copy")).Clicked() {
					s.copyTunnelEndpoint(c, tunnel.LocalURL)
				}
			})
		}
	})
	if enabled == before {
		return
	}
	if enabled {
		for _, node := range s.nodes.snapshots {
			if node.Record.NodeID != nodeID {
				continue
			}
			for _, service := range node.Services {
				if service.ServiceID == serviceID {
					s.openServiceTunnel(node, service)
					return
				}
			}
		}
		return
	}
	if tunnel != nil {
		s.closeTunnel(tunnel.TunnelID)
	}
	_ = port
}
