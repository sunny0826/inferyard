package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"math"
)

func heading(c *ui.Context, title string, level int) ui.Element {
	scale := tokens(c).Type
	size := scale.Section
	if level == 1 {
		size = scale.PageTitle
	} else if level == 2 {
		size = scale.CardTitle
	}
	return ui.Text(c, title).Bold().FontSize(size).Role(ui.RoleHeading).Level(level)
}

func section(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(tokens(c).Density.CardGap).Children(func() { heading(c, title, 3); fn() })
}

func configuredValue(value string) string {
	if value == "" {
		return "未配置"
	}
	return value
}

func button(c *ui.Context, label string) ui.Element {
	return components.Button(c, label, components.ButtonSecondary).MaxWidthPercent(100)
}

func primaryButton(c *ui.Context, label string) ui.Element {
	return components.Button(c, label, components.ButtonPrimary).MaxWidthPercent(100)
}

func activitySpinner(c *ui.Context, label string) ui.Element {
	return components.Spinner(c, components.SpinnerRing).Label(label)
}

func compactLayout(c *ui.Context) bool {
	width, _ := c.Size()
	return width < 1050 || c.Preferences().TextScale > 1
}

func progressValue(done int64, total *int64) float64 {
	if total == nil || *total <= 0 || done < 0 || done > *total {
		return -1
	}
	return float64(done) / float64(*total)
}

func progressBar(c *ui.Context, value float64, label string) ui.Element {
	if math.IsNaN(value) || math.IsInf(value, 0) || value > 1 {
		value = -1
	}
	return components.Progress(c, value).Label(label)
}

func errorAnnouncement(e *core.ApiError) string {
	return e.Code + "：" + human(e.Code) + " " + e.Message
}

func (s *viewState) submissionFailed(e *core.ApiError) {
	s.failure = e
	delete(s.announcedErrors, errorAnnouncement(e))
}

// consumeFeedback shows the pending completion message as a toast and
// announces what changed to assistive technology. It runs where the page
// content is built, so that the production window and the preview window
// behave the same.
func (s *viewState) consumeFeedback(c *ui.Context) {
	if s.pendingToast != "" {
		message := s.pendingToast
		s.pendingToast = ""
		c.AddToast(ui.Toast{ID: message, Title: message})
	}
	s.announceChanges(c)
}

func (s *viewState) announceChanges(c *ui.Context) {
	page := s.selected
	if page == "dashboard" && s.detailID != "" {
		page += "/" + s.detailID
	}
	if page != s.announcedPage {
		title := "运行详情"
		if s.detailID == "" || s.selected != "dashboard" {
			for _, n := range navigation {
				if n.id == s.selected {
					title = n.label
				}
			}
			if s.selected == "dashboard" && s.detailID == "" {
				title = "任务"
			}
		}
		c.Announce(title)
		s.announcedPage = page
	}
	errors := []string{}
	if s.failure == nil {
		errors = append(errors, s.refreshFailure)
	}
	add := func(e *core.ApiError) {
		if e != nil {
			errors = append(errors, errorAnnouncement(e))
		}
	}
	add(s.failure)
	switch s.selected {
	case "device":
		add(s.device.LastError)
		if _, err := parseDeviceSnapshot(s.device.Snapshot); err != nil {
			errors = append(errors, "设备快照无法读取，请重新采集。")
		}
	case "models":
		add(s.engineState.Error)
		for _, d := range s.downloads {
			add(d.Error)
		}
	case "dashboard":
		if s.detailID != "" {
			add(s.detailError)
			if s.detail != nil {
				add(s.detail.Task.Error)
			}
		} else {
			add(s.historyError)
			for _, task := range s.tasks {
				add(task.Error)
			}
		}
	case "reports":
		add(s.historyError)
		add(s.reportsError)
	}
	if s.progress != nil && (s.selected == "dashboard" && s.detailID == "") {
		for _, bundle := range s.progress.Bundles {
			// Guidance reasons (why resume is not offered on an unfinished
			// run) are not announced as errors; see the dashboard banner.
			if bundle.BlockedReason != nil && bundle.Phase != "run_incomplete" {
				errors = append(errors, bundle.Bundle+"："+human(*bundle.BlockedReason))
			}
		}
	}
	current := map[string]bool{}
	for _, message := range errors {
		if message == "" || current[message] {
			continue
		}
		current[message] = true
		if !s.announcedErrors[message] {
			c.Announce(message)
		}
	}
	s.announcedErrors = current
}

func runColor(c *ui.Context, task core.RunTask) ui.Color {
	if task.Exit != nil {
		switch task.Exit.ExitCode {
		case 0:
			return stateColor(c, stateSuccess)
		case 2, 3:
			return stateColor(c, stateWarning)
		case 4:
			return stateColor(c, stateDanger)
		case 130:
			return stateColor(c, stateNeutral)
		}
	}
	switch task.State {
	case "starting", "running", "cancelling":
		return stateColor(c, stateActive)
	case "failed":
		return stateColor(c, stateDanger)
	}
	return stateColor(c, stateNeutral)
}

func progressTotal(total *int64) string {
	if total == nil || *total <= 0 {
		return "总量未知"
	}
	return fmt.Sprint(*total)
}
