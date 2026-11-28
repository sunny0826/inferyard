package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/shells/sidebar"
	"inferyard/studio/internal/icons/reicon"
)

func pageShellOptions(title, stateKey string, left, content func(*ui.Context)) sidebar.ShellOptions {
	return sidebar.ShellOptions{
		Title:              title,
		Width:              1120,
		Height:             800,
		MinWidth:           800,
		MinHeight:          600,
		StateKey:           stateKey,
		LeftWidth:          232,
		LeftToggleTrailing: true,
		Left:               left,
		Content:            content,
		Theme:              studioTheme,
		Toasts:             components.ToastOptions{Placement: components.ToastTop},
	}
}

func (s *viewState) activeBackAction(c *ui.Context) (string, func()) {
	page := canonicalPageID(s.selected)
	if !s.loaded || (s.hydrating && (page == "benchmarks" || page == "reports")) {
		return "", nil
	}
	switch page {
	case "benchmarks":
		if s.detailID != "" {
			return tr(c, "detail.back_dashboard"), func() {
				s.detailID = ""
				s.detailCaseID = ""
			}
		}
		if s.environmentMissing() || s.newTaskPending {
			return "", nil
		}
		if s.benchFlowOpen {
			return tr(c, "benchmarks.action.back_list"), func() {
				s.benchFlowOpen = false
				s.historyFromFlow = false
			}
		}
		if s.benchmarksTab == benchmarksTabHistory {
			if s.historyFromFlow {
				return tr(c, "onboarding.guide.back_dashboard"), func() {
					s.historyFromFlow = false
					s.benchFlowOpen = true
					s.benchmarksTab = benchmarksTabTasks
				}
			}
			return tr(c, "benchmarks.action.back_list"), func() {
				s.benchmarksTab = benchmarksTabTasks
			}
		}
	case "reports":
		if s.nodes.compareOpen {
			return tr(c, "reports.detail.back"), func() { s.nodes.compareOpen = false }
		}
		if s.speed != nil && s.speed.opened != nil {
			return tr(c, "speed.action.back_history"), func() {
				s.speed.opened, s.speed.readError, s.speed.reading = nil, "", false
				s.reportsTab = reportsTabSpeed
			}
		}
		if s.openedReport != nil {
			return tr(c, "reports.detail.back"), func() {
				s.closeOpenedReport()
				s.reportsTab = reportsTabList
			}
		}
	}
	return "", nil
}

func (s *viewState) shellLeftHeader(c *ui.Context) {
	label, onBack := s.activeBackAction(c)
	if label == "" || onBack == nil {
		return
	}
	if sidebar.IconButton(c, reicon.ArrowLeft(), label).Clicked() {
		onBack()
	}
}
