package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"math"
)

func heading(c *ui.Context, title string, level int) ui.Element {
	size := float32(1.1)
	if level == 1 {
		size = 1.7
	} else if level == 2 {
		size = 1.3
	}
	return ui.Text(c, title).Bold().FontSize(c.Theme().Rem(size)).Role(ui.RoleHeading).Level(level)
}

func section(c *ui.Context, title string, fn func()) {
	ui.Column(c).Gap(c.Theme().Space(2.5)).Children(func() { heading(c, title, 3); fn() })
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
			if bundle.BlockedReason != nil {
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
	t := c.Theme()
	if task.Exit != nil {
		switch task.Exit.ExitCode {
		case 0:
			return t.Success
		case 2, 3:
			return t.Warning
		case 4:
			return t.Danger
		case 130:
			return t.TextMuted
		}
	}
	switch task.State {
	case "starting", "running", "cancelling":
		return t.Accent
	case "failed":
		return t.Danger
	}
	return t.TextMuted
}

func progressTotal(total *int64) string {
	if total == nil || *total <= 0 {
		return "总量未知"
	}
	return fmt.Sprint(*total)
}
