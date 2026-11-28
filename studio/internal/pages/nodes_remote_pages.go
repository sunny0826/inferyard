package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
)

func (s *viewState) remoteNodePage(c *ui.Context, page string) {
	switch page {
	case "services":
		s.remoteServicesView(c)
	case "library":
		s.remoteLibraryView(c)
	case "benchmarks":
		s.remoteBenchmarksView(c)
	}
}

func (s *viewState) remoteLibraryView(c *ui.Context) {
	node := s.activeNode()
	s.header(c, tr(c, "library.page.title"))
	platform := nodeTargetPlatform(node)
	if platform == "" {
		infoBanner(c, tr(c, "nodes.remote.platform_unknown"))
		return
	}
	heading(c, trf(c, "nodes.remote.library_title", "platform", nodePlatformLabel(node)), 2)
	if len(s.nodes.catalog) == 0 {
		emptyGuideCard(c, emptyGuideOptions{Icon: reicon.Layers, Title: tr(c, "nodes.remote.library_empty"), Surface: emptyCard})
		return
	}
	for _, family := range s.nodes.catalog {
		card(c, family.Name, func() {
			ui.Text(c, family.Description).FontSize(tokens(c).Type.Caption).TextColor(tokens(c).Color.FgSecondary)
			for _, release := range family.Releases {
				label := release.Version
				if release.Recommended {
					label += " · " + tr(c, "device.recommendation.recommended")
				}
				ui.Text(c, label).FontFeatures("tnum")
				for _, build := range release.Builds {
					if build.Platform != platform && build.Platform != "any" {
						continue
					}
					s.remoteEngineBuildRow(c, node, family, release, build, platform)
				}
			}
		})
	}
}

func (s *viewState) remoteEngineBuildRow(c *ui.Context, node core.NodeSnapshot, family core.EngineFamilyCatalog, release core.EngineRelease, build core.EnginePlatformBuild, platform string) {
	t := tokens(c)
	ui.Row(c.Key("remote-engine-build-" + build.ID)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Text(c, build.Platform+" · "+build.Variant).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1)
		installable := build.CanManagedVenvInstallOn(platform) || build.CanDirectDownloadOn(platform)
		if installable && outlineIconButton(c, reicon.Download, tr(c, "nodes.remote.install_engine")).Disabled(s.busy || !node.Online).Clicked() {
			s.installRemoteEngine(node, family, release, build)
		}
	})
}

func (s *viewState) remoteBenchmarksView(c *ui.Context) {
	t := tokens(c)
	node := s.activeNode()
	s.header(c, tr(c, "benchmarks.page.title"))
	s.remoteBenchmarkStartCard(c, node)
	if len(node.Jobs) == 0 {
		emptyGuideCard(c, emptyGuideOptions{Icon: reicon.Play, Title: tr(c, "nodes.remote.jobs_empty"), Surface: emptyCard})
		return
	}
	for _, job := range node.Jobs {
		task := job.Task
		applyMaterial(c, ui.Row(c.Key("remote-job-"+task.TaskId)).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, task.TaskId).Bold().SingleLine()
				ui.Text(c, taskLabelFor(contextLocale(c), task)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			})
			if job.Progress != nil {
				ui.Text(c, fmt.Sprintf("%d / %d", job.Progress.Done, valueOrZero(job.Progress.Total))).FontFeatures("tnum")
			}
			if !terminal(task.State) && node.Online {
				if dangerIconButton(c, reicon.Square, tr(c, "nodes.remote.cancel_job")).Clicked() {
					s.cancelRemoteBenchmark(node.Record.NodeID, task.TaskId)
				}
			}
		})
	}
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *viewState) cancelRemoteBenchmark(nodeID, jobID string) {
	s.workAs("cancel-remote-job", func() (string, *core.ApiError) {
		_, err := s.service.CancelNodeBenchmark(s.nodeContext(), nodeID, jobID)
		if err != nil {
			return "", err
		}
		s.updateView(s.syncNodeState)
		return "", nil
	})
}

func (s *viewState) sidebarRemoteServiceRow(c *ui.Context) {
	node := s.activeNode()
	running := 0
	for _, service := range node.Services {
		if service.State == "running" || service.State == "starting" {
			running++
		}
	}
	label := tr(c, "sidebar.status.service_idle")
	kind := stateNeutral
	if running > 0 {
		label = trf(c, "sidebar.status.services_running_count", "count", running)
		kind = stateSuccess
	}
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key: "sidebar-remote-engine-status", label: label, ariaLabel: label, kind: kind, progress: -1,
		onClick: func() { s.navigateTo("services", "") },
		leading: func() { components.Icon(c, reicon.Server).Size(14, 14).TextColor(stateColor(c, kind)) },
		open:    new(bool), pop: func(func()) {},
	})
}

func (s *viewState) sidebarRemoteQueueRow(c *ui.Context) {
	node := s.activeNode()
	var running, waiting int
	var activeJobs []core.RemoteNodeJob
	for _, job := range node.Jobs {
		if terminal(job.Task.State) {
			continue
		}
		activeJobs = append(activeJobs, job)
		if job.Task.Queued || job.Task.State == "queued" {
			waiting++
		} else {
			running++
		}
	}
	if len(activeJobs) == 0 {
		return
	}
	label := tr(c, "sidebar.status.queue_preparing")
	if len(activeJobs) > 0 {
		if waiting > 0 {
			label = trf(c, "sidebar.status.queue_running_waiting", "running", running, "waiting", waiting)
		} else {
			label = trf(c, "sidebar.status.queue_running", "running", running)
		}
	}
	s.sidebarStatusRow(c, sidebarStatusSpec{
		key: "sidebar-remote-queue-status", label: label, ariaLabel: label, kind: stateActive, progress: -1,
		leading: func() { components.Icon(c, reicon.Activity).Size(14, 14).TextColor(stateColor(c, stateActive)) },
		open:    &s.sidebarQueueOpen,
		pop: func(close func()) {
			t := tokens(c)
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, tr(c, "shell.header.task_queue"), 3).Grow(1)
					inlineBadge(c, label, stateActive, badgeOptions{Size: sizeSM})
				})
				for _, job := range activeJobs {
					dotKind := stateActive
					if job.Task.Queued || job.Task.State == "queued" {
						dotKind = stateNeutral
					}
					applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
						Padding(t.Density.Inset, t.Density.Row), materialSunken).Children(func() {
						ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Shrink(0).Background(stateColor(c, dotKind))
						ui.Text(c, job.Task.TaskId+" · "+taskLabelFor(contextLocale(c), job.Task)).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).SingleLine()
					})
				}
				outlineButton(c, tr(c, "sidebar.status.open_benchmarks")).AlignSelf(ui.Start).OnClick(func() {
					s.navigateTo("benchmarks", "")
					close()
				})
			})
		},
	})
}
