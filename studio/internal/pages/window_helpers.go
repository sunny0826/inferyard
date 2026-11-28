package pages

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) loadingView(c *ui.Context) {
	page := canonicalPageID(s.selected)
	title := tr(c, "shell.loading.getting_started")
	if page == "benchmarks" {
		title = tr(c, "nav.benchmarks")
		if s.detailID != "" {
			title = tr(c, "shell.page.run_detail")
		}
	} else if page == "settings" {
		title = tr(c, "nav.settings")
	} else {
		for _, n := range navigation {
			if n.id == page {
				title = tr(c, n.labelKey)
			}
		}
	}
	if title != "" {
		s.header(c, title)
	}
	if s.failure != nil || s.refreshFailure != "" {
		button(c, tr(c, "shell.loading.retry_page")).AlignSelf(ui.Start).OnClick(s.refreshAction)
		return
	}
	host := ui.Column(c.Key("page-loading")).FillWidth().Gap(tokens(c).Density.Section)
	show := delayedVisible(c, host, "page-load", !s.loaded || s.hydrating)
	host.Children(func() {
		if show {
			pageLoadingSkeleton(c)
		}
	})
}

func (s *viewState) refreshAction() {
	if s.simulate(previewReload) {
		return
	}
	if !s.busy {
		go s.refresh(true)
	}
}

func (s *viewState) header(c *ui.Context, title string) {
	heading(c, title, 1)
}

func field(c *ui.Context, label string, value *string) ui.Element {
	return ui.Field(c, label, func() { studioTextBox(c, value) })
}

func text(p *string) string {
	if p == nil || *p == "" {
		return l10n.TR("common.missing")
	}
	return *p
}

func emptyText(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func terminal(state string) bool { return state == "done" || state == "failed" || state == "cancelled" }

func (s *viewState) pageScroll() *ui.ScrollState {
	if s.scrolls == nil {
		s.scrolls = map[string]*ui.ScrollState{}
	}
	key := canonicalPageID(s.selected)
	if key == "benchmarks" && s.detailID != "" {
		key = "detail"
	}
	if key == "reports" && s.nodes.compareOpen {
		key = "reports/compare"
	} else if key == "reports" && s.openedReport != nil {
		key = "report/" + s.openedReport.reference
	}
	if key == "settings" && s.settingsTab == settingsTabAbout {
		key = "settings/about"
	}
	if s.scrolls[key] == nil {
		s.scrolls[key] = &ui.ScrollState{}
	}
	return s.scrolls[key]
}

func (s *viewState) shouldVerifyEngines(full, visible bool, now time.Time) bool {
	if !visible || (!full && !s.lastEngineVerification.IsZero() && now.Sub(s.lastEngineVerification) < 5*time.Second) {
		return false
	}
	s.lastEngineVerification = now
	return true
}

func (s *viewState) openLocalEngineWizard(family string) {
	s.wizardOpenRequested = true
	s.wizardPresetFamily = family
	s.libraryTab = 1
	s.navigateTo("library", "")
	s.pageScroll().ScrollTo(0, 0, ui.Animation{})
}

func (s *viewState) contentView(c *ui.Context) {
	page := canonicalPageID(s.selected)
	if page != "reports" && s.openedReport != nil && s.openedReport.svgStage != nil {
		s.openedReport.releaseSVGStage()
	}
	s.consumeFeedback(c)
	s.dropStaleStopPrompt()
	s.clearPromptOnNavigation()
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		ui.Scroll(c.Key(page)).Grow(1).TrackScroll(s.pageScroll()).Padding(tokens(c).Density.Page).Children(func() {
			ui.Column(c).Gap(tokens(c).Density.Section).Children(func() {
				s.feedback(c)
				if !s.loaded || (s.hydrating && (page == "benchmarks" || page == "reports")) {
					s.loadingView(c)
					return
				}
				switch page {
				case "services":
					if s.remoteView() {
						s.remoteServicesView(c)
					} else {
						s.servicesView(c)
					}
				case "library":
					if s.remoteView() {
						s.remoteLibraryView(c)
					} else {
						s.libraryView(c)
					}
				case "benchmarks":
					if s.remoteView() {
						s.remoteBenchmarksView(c)
					} else if s.detailID != "" {
						s.detailView(c)
					} else {
						s.benchmarksView(c)
					}
				case "reports":
					s.reportsView(c)
				case "settings":
					s.settingsView(c)
				default:
					s.servicesView(c)
				}
			})
		})
	})
	q := s.queueActivity()
	if s.destructiveDialogModal(c, destructiveOption{
		kind: destructiveStopEngine, id: "header", label: tr(c, "shell.header.stop_engine.label"), confirmLabel: tr(c, "shell.header.stop_engine.confirm"),
		prompt:   tr(c, "common.confirm.stop_engine.prompt"),
		disabled: !s.stopEnabled(true, q.active()),
	}) {
		s.stopEngine()
	}
	if destructiveKind(s.pendingDestructive.kind) == destructiveStopService {
		targetID := s.pendingDestructive.id
		if svc, ok := s.findManagedServiceByID(targetID); ok {
			if s.destructiveDialogModal(c, destructiveOption{
				kind:         destructiveStopService,
				id:           targetID,
				label:        tr(c, "services.card.stop_confirm_title"),
				confirmLabel: tr(c, "services.card.stop_confirm_action"),
				prompt:       trf(c, "services.card.stop_confirm_prompt", "model", s.serviceModelTitle(c, svc)),
				disabled:     !s.serviceStopEnabled(svc, q.active()),
			}) {
				s.stopManagedServiceByID(svc.ServiceID)
			}
		}
	}
	s.resumeDialog(c)
	s.settingsDiscardDialog(c)
	s.cleanWorkspaceArtifactsDialog(c)
	s.standaloneDialogs(c)
	s.serviceDrawersAndDialogs(c)
	s.updateDialog(c)
	s.updateReadyPrompt(c)
	s.nodesDialogs(c)
	s.studioNotifications(c)
	studioToasts(c)
}
