package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) remoteServiceStartCard(c *ui.Context, node core.NodeSnapshot) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("remote-service-start")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "nodes.remote.start_service")).Bold().Grow(1)
			if outlineIconButton(c, reicon.Plus, tr(c, "nodes.remote.start_service")).Disabled(s.busy || !node.Online).Clicked() {
				s.nodes.serviceFormOpen = !s.nodes.serviceFormOpen
				if s.nodes.serviceContext == "" {
					s.nodes.serviceContext = "8192"
				}
			}
		})
		if !node.Online {
			ui.Text(c, tr(c, "nodes.remote.online_required")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if !s.nodes.serviceFormOpen {
			return
		}
		monoField(c, tr(c, "nodes.remote.model_path"), "", "/models/model.gguf", "", &s.nodes.serviceModel)
		monoField(c, tr(c, "nodes.remote.engine_path"), "", "/opt/inferyard/engines/llama-server", "", &s.nodes.serviceEngine)
		ui.Grid(c).Columns(2).Gap(t.Density.Row).Children(func() {
			monoField(c, tr(c, "nodes.remote.context_size"), "", "8192", "", &s.nodes.serviceContext)
			monoField(c, tr(c, "nodes.remote.port_optional"), "", "48857", "", &s.nodes.servicePort)
		})
		_, valid := parseRemoteServiceRequest(s.nodes.serviceModel, s.nodes.serviceEngine, s.nodes.serviceContext, s.nodes.servicePort)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if primaryActionButton(c, tr(c, "nodes.remote.start_service_submit"), s.actionBusy("start-remote-service")).
				Disabled(s.busy || !node.Online || !valid).Clicked() {
				s.startRemoteService(node)
			}
		})
	})
}

func (s *viewState) remoteBenchmarkStartCard(c *ui.Context, node core.NodeSnapshot) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c.Key("remote-benchmark-start")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Text(c, tr(c, "nodes.remote.start_benchmark")).Bold().Grow(1)
			if outlineIconButton(c, reicon.Plus, tr(c, "nodes.remote.start_benchmark")).Disabled(s.busy || !node.Online).Clicked() {
				s.nodes.benchmarkFormOpen = !s.nodes.benchmarkFormOpen
			}
		})
		if !node.Online {
			ui.Text(c, tr(c, "nodes.remote.online_required")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if !s.nodes.benchmarkFormOpen {
			return
		}
		monoField(c, tr(c, "nodes.remote.config_paths"), tr(c, "nodes.remote.config_paths_hint"), "/workspace/configs/benchmark.json", "", &s.nodes.benchmarkConfigs)
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if primaryActionButton(c, tr(c, "nodes.remote.start_benchmark_submit"), s.actionBusy("start-remote-benchmark")).
				Disabled(s.busy || !node.Online || len(splitRemoteConfigPaths(s.nodes.benchmarkConfigs)) == 0).Clicked() {
				s.startRemoteBenchmark(node)
			}
		})
	})
}
