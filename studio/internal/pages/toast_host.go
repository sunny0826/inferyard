package pages

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

type toastKind int

const (
	toastKindStatus toastKind = iota
	toastKindAction
	toastKindProgress
)

type toastSpec struct {
	ID            string
	Kind          toastKind
	Title         string
	Action        string
	OnAction      func()
	RestoredTitle string
	Duration      time.Duration
	Sheen         bool
	Phase         int
	CloseOnly     bool
}

type toastRenderData struct {
	Kind      toastKind
	Phase     int
	Sheen     bool
	SheenAt   time.Time
	CreatedAt time.Time
	Duration  time.Duration
	ShowClose bool
}

type toastSheenState struct {
	phase int
	start time.Time
}

type toastCountdownState struct {
	phase     int
	startedAt time.Time
	elapsed   time.Duration
	lastFrame time.Time
}

var (
	toastEnterEase  = ui.CubicBezier(0.25, 0.1, 0.25, 1)
	toastResizeEase = ui.CubicBezier(0.32, 0.72, 0, 1)
)

func statusToastDuration(title string) time.Duration {
	n := utf8.RuneCountInString(strings.TrimSpace(title))
	switch {
	case n <= 10:
		return 3200 * time.Millisecond
	case n <= 20:
		return 4000 * time.Millisecond
	default:
		return 5000 * time.Millisecond
	}
}

func (s *viewState) enqueueToast(spec toastSpec) {
	if strings.TrimSpace(spec.Title) == "" && !spec.CloseOnly {
		return
	}
	s.pendingToasts = append(s.pendingToasts, spec)
}

func (s *viewState) enqueueStatusToast(title string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	s.enqueueToast(toastSpec{
		ID:       "studio-status-toast",
		Kind:     toastKindStatus,
		Title:    title,
		Duration: statusToastDuration(title),
		Sheen:    true,
	})
}

func (s *viewState) enqueueActionToast(id, title, actionLabel, restoredTitle string, duration time.Duration, onAction func()) {
	if id == "" {
		id = "studio-action-toast"
	}
	if duration < 5*time.Second {
		duration = 6 * time.Second
	}
	s.enqueueToast(toastSpec{
		ID:            id,
		Kind:          toastKindAction,
		Title:         strings.TrimSpace(title),
		Action:        actionLabel,
		OnAction:      onAction,
		RestoredTitle: strings.TrimSpace(restoredTitle),
		Duration:      duration,
		Sheen:         false,
	})
}

func (s *viewState) enqueueProgressToast(id, title string) {
	if id == "" {
		id = "studio-progress-toast"
	}
	s.enqueueToast(toastSpec{
		ID:       id,
		Kind:     toastKindProgress,
		Title:    strings.TrimSpace(title),
		Duration: -1,
		Sheen:    false,
	})
}

func (s *viewState) resolveProgressToast(id, title, actionLabel string, onAction func()) {
	if id == "" {
		id = "studio-progress-toast"
	}
	duration := 3200 * time.Millisecond
	if actionLabel != "" {
		duration = 5 * time.Second
	}
	s.enqueueToast(toastSpec{
		ID:       id,
		Kind:     toastKindStatus,
		Title:    strings.TrimSpace(title),
		Action:   actionLabel,
		OnAction: onAction,
		Duration: duration,
		Sheen:    true,
		Phase:    1,
	})
}

func (s *viewState) dismissToast(id string) {
	s.enqueueToast(toastSpec{ID: id, CloseOnly: true})
}

func (s *viewState) flushToasts(c *ui.Context) {
	if s.pendingToast != "" {
		message := s.pendingToast
		s.pendingToast = ""
		s.enqueueStatusToast(message)
	}
	if len(s.pendingToasts) == 0 {
		return
	}
	queue := s.pendingToasts
	s.pendingToasts = nil
	now := time.Now()
	for _, spec := range queue {
		if spec.CloseOnly {
			c.CloseToast(spec.ID)
			if s.activeToastID == spec.ID {
				s.activeToastID = ""
			}
			continue
		}
		if s.activeToastID != "" && s.activeToastID != spec.ID {
			c.CloseToast(s.activeToastID)
		}
		s.activeToastID = spec.ID
		s.toastPhase++
		phase := s.toastPhase
		if spec.Phase > 0 {
			phase = s.toastPhase
		}
		timeout := spec.Duration
		if timeout == 0 {
			timeout = statusToastDuration(spec.Title)
		}
		showClose := spec.Kind == toastKindAction || spec.Action != ""
		var onAction func()
		if spec.OnAction != nil {
			specCopy := spec
			onAction = func() {
				specCopy.OnAction()
				if specCopy.RestoredTitle != "" {
					s.enqueueToast(toastSpec{
						ID:       specCopy.ID,
						Kind:     toastKindStatus,
						Title:    specCopy.RestoredTitle,
						Duration: 1500 * time.Millisecond,
						Sheen:    true,
						Phase:    1,
					})
				}
			}
		}
		c.AddToast(ui.Toast{
			ID:       spec.ID,
			Title:    spec.Title,
			Timeout:  timeout,
			Action:   spec.Action,
			OnAction: onAction,
			Data: toastRenderData{
				Kind:      spec.Kind,
				Phase:     phase,
				Sheen:     spec.Sheen,
				SheenAt:   now,
				CreatedAt: now,
				Duration:  timeout,
				ShowClose: showClose,
			},
		})
	}
}

func studioToasts(c *ui.Context) {
	t := tokens(c)
	dark := c.Theme().Dark
	reduced := c.Preferences().ReduceMotion
	topInset := sidebar.HeaderHeight(c) + 12

	enterMotion := &ui.Motion{Y: -10, Scale: .96, Blur: 8, Opacity: 0}
	exitMotion := &ui.Motion{Y: -6, Blur: 6, Opacity: 0}
	enterDuration := 500 * time.Millisecond
	exitDuration := 220 * time.Millisecond
	resizeDuration := 380 * time.Millisecond
	if reduced {
		enterMotion = &ui.Motion{Opacity: 0}
		exitMotion = &ui.Motion{Opacity: 0}
		enterDuration = 150 * time.Millisecond
		exitDuration = 150 * time.Millisecond
		resizeDuration = 150 * time.Millisecond
	}

	surfaceFrom, surfaceTo := ui.Hex("#ffffff"), ui.Hex("#f7f9fc")
	borderColor := ui.RGBA(214, 219, 227, .95)
	shadowPrimary := ui.RGBA(17, 24, 39, .08)
	shadowSecondary := ui.RGBA(17, 24, 39, .05)
	if dark {
		surfaceFrom, surfaceTo = t.Color.Elevated, ui.Hex("#282828")
		borderColor = t.Color.BorderDefault
		shadowPrimary = ui.RGBA(0, 0, 0, .30)
		shadowSecondary = ui.RGBA(0, 0, 0, .18)
	}

	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.AlignItems(ui.Center).Justify(ui.Start).Padding(topInset, 16, 16, 16)
		if len(toasts) == 0 {
			return
		}
		// Keep one toast visible at a time: always render the newest one.
		toast := toasts[len(toasts)-1]
		data, ok := toast.Data.(toastRenderData)
		if !ok {
			data = toastRenderData{
				Kind:      toastKindStatus,
				Phase:     1,
				Sheen:     true,
				Duration:  statusToastDuration(toast.Title),
				ShowClose: toast.Action != "",
			}
		}

		parts := ui.ToastBase(c, toast)
		paused := viewport.Hovered() || viewport.FocusWithin()

		padRight := float32(12)
		if data.ShowClose || toast.Action != "" {
			padRight = 6
		}
		pill := parts.Root.MinHeight(40).Padding(6, padRight, 6, 8).Radius(16).Clip().
			LinearGradient(ui.LinearGradient{
				From:  surfaceFrom,
				To:    surfaceTo,
				Angle: 180,
			}).
			Border(1, borderColor).
			Shadow(0, 16, 36, 0, shadowPrimary).
			Shadow(0, 3, 10, 0, shadowSecondary).
			Transition(ui.ElementTransition{
				Duration:     enterDuration,
				Ease:         toastEnterEase,
				ExitDuration: exitDuration,
				ExitEase:     ui.EaseIn,
				Width:        true,
				Enter:        enterMotion,
				Exit:         exitMotion,
			})
		if !reduced {
			pill.Transition(ui.ElementTransition{
				Duration:     resizeDuration,
				Ease:         toastResizeEase,
				ExitDuration: exitDuration,
				ExitEase:     ui.EaseIn,
				Width:        true,
				Enter:        enterMotion,
				Exit:         exitMotion,
			})
		}

		sheenState := ui.Local(pill, "sheen", func() toastSheenState { return toastSheenState{} })
		if data.Sheen && sheenState.phase != data.Phase {
			sheenState.phase = data.Phase
			sheenState.start = time.Now()
		}

		pill.Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				studioToastIcon(c, pill, data, paused, reduced)
				studioToastBody(c, parts, toast, data, reduced)
			})
			if data.Sheen && !reduced {
				studioToastSheenOverlay(c, sheenState, dark)
			}
		})
	})
}

func studioToastIcon(c *ui.Context, host ui.Element, data toastRenderData, paused, reduced bool) {
	t := tokens(c)
	iconTransition := ui.ElementTransition{
		Duration:     300 * time.Millisecond,
		Ease:         toastResizeEase,
		ExitDuration: 180 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Scale: .5, Rotate: -40, Blur: 4, Opacity: 0},
		Exit:         &ui.Motion{Scale: .5, Blur: 4, Opacity: 0},
	}
	if reduced {
		iconTransition = ui.ElementTransition{
			Duration:     150 * time.Millisecond,
			ExitDuration: 150 * time.Millisecond,
			Enter:        &ui.Motion{Opacity: 0},
			Exit:         &ui.Motion{Opacity: 0},
		}
	}

	ui.Box(c).Size(24, 24).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		slot := ui.Box(c.Key(fmt.Sprintf("toast-icon-%d-%d", data.Kind, data.Phase))).
			Size(24, 24).Radius(8).AlignItems(ui.Center).Justify(ui.Center).
			Transition(iconTransition)

		switch data.Kind {
		case toastKindProgress:
			slot.Background(t.Color.Active.SubtleBg).Children(func() {
				studioRingSpinner(c, t.Color.Active.Fg, reduced)
			})
		case toastKindAction:
			slot.Background(t.Color.AccentSubtleBg).Children(func() {
				studioCountdownRing(c, host, data, paused, reduced)
			})
		default:
			slot.Background(t.Color.Success.SubtleBg).Children(func() {
				components.Icon(c, lucide.Check).Size(14, 14).TextColor(t.Color.Success.Fg).Role(ui.RoleNone)
			})
		}
	})
}

func studioToastBody(c *ui.Context, parts ui.ToastParts, toast ui.Toast, data toastRenderData, reduced bool) {
	t := tokens(c)
	contentTransition := ui.ElementTransition{
		Animation:    &ui.Animation{Duration: 300 * time.Millisecond, Delay: 100 * time.Millisecond, Ease: toastResizeEase},
		ExitDuration: 180 * time.Millisecond,
		ExitEase:     ui.EaseIn,
		Enter:        &ui.Motion{Y: 5, Blur: 4, Opacity: 0},
		Exit:         &ui.Motion{Y: -5, Blur: 4, Opacity: 0},
	}
	if reduced {
		contentTransition = ui.ElementTransition{
			Duration:     150 * time.Millisecond,
			ExitDuration: 150 * time.Millisecond,
			Enter:        &ui.Motion{Opacity: 0},
			Exit:         &ui.Motion{Opacity: 0},
		}
	}

	ui.Row(c.Key(fmt.Sprintf("toast-body-%d", data.Phase))).
		AlignItems(ui.Center).Gap(6).Transition(contentTransition).Children(func() {
		ui.Text(c, toast.Title).FontSize(14).FixedLineHeight(20).TextColor(t.Color.FgPrimary).SingleLine()
		if toast.Action != "" || data.ShowClose {
			ui.Row(c).AlignItems(ui.Center).Gap(2).Children(func() {
				if toast.Action != "" {
					btn := parts.ActionButton().Height(28).Padding(0, 10).Radius(10).
						AlignItems(ui.Center).Justify(ui.Center).Label(toast.Action)
					if btn.Pressed() {
						btn.Background(t.Color.AccentSelection)
					} else if btn.Hovered() {
						btn.Background(t.Color.AccentSubtleBg)
					}
					btn.Children(func() {
						ui.Text(c, toast.Action).FontSize(13).Bold().TextColor(t.Color.Active.Fg).SingleLine()
					})
				}
				if data.ShowClose {
					closeBtn := parts.CloseButton().Size(28, 28).Radius(10).
						AlignItems(ui.Center).Justify(ui.Center).Label("关闭提示")
					if closeBtn.Hovered() {
						closeBtn.Background(t.Color.SurfaceHover)
					}
					closeBtn.Children(func() {
						components.Icon(c, lucide.X).Size(14, 14).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
					})
				}
			})
		}
	})
}

func studioRingSpinner(c *ui.Context, color ui.Color, reduced bool) {
	ui.Box(c).Size(16, 16).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := float32(6)
		p.Stroke(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, color.Alpha(.22), rad, 1.75)
		angle := float64(0)
		if !reduced {
			p.AnimationFrame()
			angle = float64(p.Now().UnixMilli()%900) / 900 * 2 * math.Pi
		}
		pts := arcPoints(cx, cy, rad, angle, angle+math.Pi*1.15, 18)
		p.StrokePolyline(pts, 1.75, color)
	})
}

func studioCountdownRing(c *ui.Context, host ui.Element, data toastRenderData, paused, reduced bool) {
	t := tokens(c)
	cd := ui.Local(host, "countdown", func() toastCountdownState {
		now := time.Now()
		return toastCountdownState{phase: data.Phase, startedAt: now, lastFrame: now}
	})
	now := time.Now()
	if cd.phase != data.Phase {
		cd.phase = data.Phase
		cd.startedAt = now
		cd.elapsed = 0
		cd.lastFrame = now
	} else {
		if !paused && !cd.lastFrame.IsZero() {
			if delta := now.Sub(cd.lastFrame); delta > 0 && delta < time.Second {
				cd.elapsed += delta
			}
		}
		cd.lastFrame = now
	}
	total := data.Duration
	if total <= 0 {
		total = 6 * time.Second
	}
	remaining := max(0, total-cd.elapsed)
	fraction := float64(remaining) / float64(total)
	secs := int(math.Ceil(remaining.Seconds()))
	if secs < 1 {
		secs = 1
	}

	ui.Box(c).Size(24, 24).AlignItems(ui.Center).Justify(ui.Center).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := float32(8.5)
		p.Stroke(ui.Rect{X: cx - rad, Y: cy - rad, W: rad * 2, H: rad * 2}, t.Color.Active.Solid.Alpha(.18), rad, 1.75)
		if fraction > 0.01 {
			start := -math.Pi / 2
			end := start + 2*math.Pi*fraction
			p.StrokePolyline(arcPoints(cx, cy, rad, start, end, 24), 1.75, t.Color.Active.Solid)
		}
		if !paused && remaining > 0 && !reduced {
			p.AnimationFrame()
		}
	}).Children(func() {
		ui.Text(c, fmt.Sprintf("%d", secs)).FontSize(10).Bold().FontFeatures("tnum").TextColor(t.Color.FgPrimary).SingleLine()
	})
}

func studioToastSheenOverlay(c *ui.Context, state *toastSheenState, dark bool) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		if state.start.IsZero() || r.W <= 0 || r.H <= 0 {
			return
		}
		elapsed := time.Since(state.start)
		const sheenDuration = 900 * time.Millisecond
		if elapsed >= sheenDuration {
			return
		}
		p.AnimationFrame()
		progress := ui.EaseOut(float32(elapsed) / float32(sheenDuration))
		bandW := max(48, r.W*0.38)
		x := r.X - bandW + (r.W+bandW*2)*progress
		alpha := float32(0.55)
		if dark {
			alpha = 0.14
		}
		p.Clip(r, 16, func() {
			p.FillGradient(ui.Rect{X: x, Y: r.Y, W: bandW, H: r.H}, ui.LinearGradient{
				Angle: 90,
				Stops: []ui.GradientStop{
					{Color: ui.RGBA(255, 255, 255, 0), At: 0},
					{Color: ui.RGBA(255, 255, 255, alpha), At: 0.5},
					{Color: ui.RGBA(255, 255, 255, 0), At: 1},
				},
			}, 0)
		})
	})
}

func arcPoints(cx, cy, radius float32, startAngle, endAngle float64, segments int) [][2]float32 {
	if segments < 2 {
		segments = 2
	}
	pts := make([][2]float32, 0, segments+1)
	for i := 0; i <= segments; i++ {
		t := float64(i) / float64(segments)
		a := startAngle + (endAngle-startAngle)*t
		pts = append(pts, [2]float32{
			cx + radius*float32(math.Cos(a)),
			cy + radius*float32(math.Sin(a)),
		})
	}
	return pts
}
