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
	return components.Button(c, label, components.ButtonSecondary).MaxWidthPercent(100).AlignSelf(ui.Start)
}

func primaryButton(c *ui.Context, label string) ui.Element {
	return components.Button(c, label, components.ButtonPrimary).MaxWidthPercent(100).AlignSelf(ui.Start)
}

func dangerButton(c *ui.Context, label string) ui.Element {
	theme := c.Theme()
	danger := tokens(c).State.Danger
	b := components.Button(c, label, components.ButtonOutline).MaxWidthPercent(100).AlignSelf(ui.Start)
	background := theme.Surface.Mix(danger, .10)
	if !b.IsDisabled() {
		switch {
		case b.Pressed():
			background = theme.Surface.Mix(danger, .28)
		case b.Hovered():
			background = theme.Surface.Mix(danger, .18)
		}
	}
	return b.Background(background).TextColor(danger.Mix(theme.Text, .2)).Border(1, danger.Alpha(.7))
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
	if math.IsNaN(value) || math.IsInf(value, 0) {
		value = -1
	} else if value > 1 {
		value = -1
	}
	t := tokens(c)
	reduced := c.Preferences().ReduceMotion
	trackColor := t.Color.BorderSubtle
	fillColor := t.Color.Active.Solid
	if value >= 1 {
		fillColor = t.Color.Success.Solid
	}
	e := ui.Box(c).Height(c.Theme().Space(1.5)).FillWidth().Role(ui.RoleProgress).Range(0, 1, value).Label(label)
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		rad := r.H / 2
		p.Fill(r, trackColor, rad)
		x, w := r.X, r.W*float32(value)
		if value < 0 {
			phase := 0.25
			if !reduced {
				p.AnimationFrame()
				phase = float64(p.Now().UnixMilli()%1400) / 1400
			}
			w = r.W * 0.3
			x = r.X + (r.W-w)*float32((1-math.Cos(phase*2*math.Pi))/2)
		}
		if w > 0 {
			p.Clip(r, rad, func() { p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, fillColor, rad) })
		}
	})
	return e
}

func errorAnnouncement(e *core.ApiError) string {
	if e.Message != "" {
		return e.Message
	}
	return human(e.Code)
}

func (s *viewState) submissionFailed(e *core.ApiError) {
	s.failure = e
	s.failurePage = s.selected
	delete(s.announcedErrors, errorAnnouncement(e))
}

func (s *viewState) consumeFeedback(c *ui.Context) {
	s.flushToasts(c)
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
	if task.Queued && task.State == "starting" {
		return stateColor(c, stateNeutral)
	}
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
		return "—"
	}
	return fmt.Sprint(*total)
}
