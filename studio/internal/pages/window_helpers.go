package pages

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
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
	show := delayedVisible(c, host, "page-load", !s.loaded)
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
	return ui.Field(c, label, func() { components.TextBox(c, value, components.TextBoxOptions{}) })
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
	if key == "reports" && s.openedReport != nil {
		key = "report/" + s.openedReport.reference
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
}
