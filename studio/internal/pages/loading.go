package pages

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

const (
	loadingDelay         = 300 * time.Millisecond
	loadingMinShow       = 500 * time.Millisecond
	loadingSlowThreshold = 10 * time.Second
)

type delayedLoadingState struct {
	startedAt time.Time
	shownAt   time.Time
	showing   bool
}

func delayedElapsed(c *ui.Context, host ui.Element, key string, active bool) (bool, time.Duration) {
	state := ui.Local(host, "delayed-"+key, func() delayedLoadingState { return delayedLoadingState{} })
	now := c.Now()
	if active {
		if state.startedAt.IsZero() {
			state.startedAt = now
		}
		elapsed := now.Sub(state.startedAt)
		if elapsed >= loadingDelay {
			if !state.showing {
				state.showing = true
				state.shownAt = now
			}
		} else {
			c.After(loadingDelay - elapsed)
		}
		if elapsed < loadingSlowThreshold {
			c.After(loadingSlowThreshold - elapsed)
		}
		return state.showing, elapsed
	}
	state.startedAt = time.Time{}
	if state.showing {
		shownFor := now.Sub(state.shownAt)
		if shownFor < loadingMinShow {
			c.After(loadingMinShow - shownFor)
			return true, 0
		}
		state.showing = false
		state.shownAt = time.Time{}
	}
	return false, 0
}

func delayedVisible(c *ui.Context, host ui.Element, key string, active bool) bool {
	visible, _ := delayedElapsed(c, host, key, active)
	return visible
}

type spinIconState struct {
	spinning  bool
	startedAt time.Time
	angle     float64
	stopAt    float64
}

func refreshButton(c *ui.Context, label string, active bool) ui.Element {
	t := tokens(c)
	b := applySecondaryButtonPalette(c, styleButtonSize(c, components.Button(c, "", components.ButtonSecondary), sizeMD).Label(label))
	applyButtonOpticalPadding(b, t, sizeMD, buttonIconLeading)
	state := ui.Local(b, "refresh-spin", func() spinIconState { return spinIconState{} })
	now := c.Now()
	if active {
		if !state.spinning {
			state.spinning = true
			state.startedAt = now
			state.angle = 0
		}
		state.stopAt = 0
	} else if state.spinning && state.stopAt == 0 {
		turns := math.Max(1, math.Ceil(state.angle/360))
		state.stopAt = turns * 360
	}
	angle := float32(0)
	if state.spinning {
		if c.Preferences().ReduceMotion {
			if !active {
				state.spinning = false
				state.angle = 0
			}
		} else {
			elapsed := now.Sub(state.startedAt).Seconds()
			// First turn eases over 640ms; longer waits continue smoothly at 600 deg/s.
			deg := elapsed * 600
			if elapsed < 0.64 && state.stopAt == 360 {
				t := elapsed / 0.64
				deg = float64(ui.EaseInOut(float32(t))) * 360
			}
			state.angle = deg
			if state.stopAt > 0 && state.angle >= state.stopAt {
				state.spinning = false
				state.angle = 0
				state.stopAt = 0
			} else {
				angle = float32(math.Mod(state.angle, 360))
				c.AnimationFrame()
			}
		}
	}
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Box(c).Size(14, 14).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				icon := components.Icon(c, reicon.RefreshCw).Size(14, 14)
				if angle != 0 {
					icon.Rotate(angle)
				}
			})
			ui.Text(c, label).SingleLine()
		})
	})
	return b
}

func actionButton(c *ui.Context, variant components.ButtonVariant, label string, active bool) ui.Element {
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, "", variant), sizeMD).Label(label)
	if variant == components.ButtonSecondary {
		applySecondaryButtonPalette(c, b)
	}
	showSpinner := delayedVisible(c, b, "btn-spinner", active)
	side := buttonIconNone
	if showSpinner {
		side = buttonIconLeading
	}
	applyButtonOpticalPadding(b, t, sizeMD, side)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			if showSpinner {
				components.Spinner(c, components.SpinnerRing).Size(14, 14).Label(trf(c, "common.loading.action_busy", "label", label))
			}
			ui.Text(c, label).SingleLine()
		})
	})
	return b
}

func primaryActionButton(c *ui.Context, label string, active bool) ui.Element {
	return actionButton(c, components.ButtonPrimary, label, active)
}

func secondaryActionButton(c *ui.Context, label string, active bool) ui.Element {
	return actionButton(c, components.ButtonSecondary, label, active)
}

func skeletonCard(c *ui.Context, title string, lines int, label string) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).
		Children(func() {
			if title != "" {
				heading(c, title, 2)
			}
			components.Skeleton(c, components.SkeletonOptions{Lines: lines, Label: label})
		})
}

func pageLoadingSkeleton(c *ui.Context) {
	skeletonCard(c, "", 3, tr(c, "common.loading.primary_content"))
	skeletonCard(c, "", 4, tr(c, "common.loading.detail_content"))
}

func detailLoadingSkeleton(c *ui.Context) {
	skeletonCard(c, tr(c, "common.loading.task_summary_title"), 3, tr(c, "common.loading.task_summary_label"))
	skeletonCard(c, tr(c, "common.loading.live_progress_title"), 4, tr(c, "common.loading.case_progress_label"))
	skeletonCard(c, tr(c, "common.loading.resource_chart_title"), 3, tr(c, "common.loading.resource_samples_label"))
}

func (s *viewState) currentClock(c *ui.Context) time.Time {
	if s != nil && s.preview != nil {
		if pm, ok := s.preview.(previewClockMode); ok {
			return pm.now()
		}
	}
	return c.Now()
}

func (s *viewState) formatTaskElapsed(c *ui.Context, startedAt string) string {
	raw := strings.TrimSpace(startedAt)
	if raw == "" {
		return ""
	}
	started, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return ""
	}
	elapsed := max(time.Duration(0), s.currentClock(c).Sub(started))
	c.After(time.Second - (elapsed % time.Second))
	return trf(c, "common.elapsed.running", "duration", formatElapsedDurationFor(contextLocale(c), elapsed))
}

func formatElapsedDurationFor(locale string, d time.Duration) string {
	if d < time.Second {
		return l10n.T(locale, "common.duration.under_second")
	}
	secs := int64(d / time.Second)
	if secs < 60 {
		return l10n.Tf(locale, "common.duration.seconds", "seconds", secs)
	}
	mins := secs / 60
	remSecs := secs % 60
	if mins < 60 {
		if remSecs == 0 {
			return l10n.Tf(locale, "common.duration.minutes", "minutes", mins)
		}
		return l10n.Tf(locale, "common.duration.minutes_seconds", "minutes", mins, "seconds", remSecs)
	}
	hours := mins / 60
	remMins := mins % 60
	if remMins == 0 {
		return l10n.Tf(locale, "common.duration.hours", "hours", hours)
	}
	return l10n.Tf(locale, "common.duration.hours_minutes", "hours", hours, "minutes", remMins)
}
