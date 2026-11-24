package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func stateBanner(c *ui.Context, kind stateKind, icon func() *ui.SVG, fn func(foreground ui.Color)) {
	t := tokens(c)
	scale := stateScale(c, kind)
	banner := ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Stack).Radius(t.Radius.Inset)
	if c.Preferences().HighContrast {
		banner.Background(scale.SubtleBg).Border(1, scale.Border)
	} else {
		banner.Background(scale.SubtleBg).BorderWidth(0, 0, 0, 3).BorderColor(scale.Solid)
	}
	banner.Children(func() {
		ui.Row(c).AlignItems(ui.Start).Gap(t.Density.Row).Children(func() {
			ui.Box(c).Padding(2, 0, 0, 0).Children(func() {
				components.Icon(c, icon).Size(t.Type.Body, t.Type.Body).TextColor(scale.Fg)
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() { fn(scale.Fg) })
		})
	})
}

func errorView(c *ui.Context, e *core.ApiError) {
	errorRecoveryBanner(c, e, nil)
}

func conflictReasonLabel(reason string) string {
	switch reason {
	case "not_resumable":
		return l10n.TR("common.conflict.not_resumable")
	case "run_active", "engine_active":
		return l10n.TR("common.conflict.run_or_engine_active")
	case "engine_stopping":
		return l10n.TR("common.conflict.engine_stopping")
	case "engine_orphaned":
		return l10n.TR("common.conflict.engine_orphaned")
	case "settings_incomplete":
		return l10n.TR("common.conflict.settings_incomplete")
	case "output_exists":
		return l10n.TR("common.conflict.output_exists")
	case "invalid_state":
		return l10n.TR("common.conflict.invalid_state")
	case "polystrata_setup_active":
		return l10n.TR("common.conflict.polystrata_setup_active")
	case "update_applying":
		return l10n.TR("common.conflict.update_applying")
	default:
		return ""
	}
}

func warningBanner(c *ui.Context, message string) {
	if message == "" {
		return
	}
	stateBanner(c, stateWarning, lucide.TriangleAlert, func(foreground ui.Color) {
		ui.Text(c, message).Role(ui.RoleStatus).TextColor(foreground)
	})
}

var taskPrepStageKeys = map[string]string{
	"awaiting_service":  "common.task.prep.awaiting_service",
	"switching_service": "common.task.prep.switching_service",
	"starting_engine":   "common.task.prep.starting_engine",
	"binding":           "common.task.prep.binding",
}

func taskLabel(t core.RunTask) string {
	return taskLabelFor("", t)
}

func taskLabelFor(locale string, t core.RunTask) string {
	if !terminal(t.State) && t.PreparationStage != "" {
		if key, ok := taskPrepStageKeys[t.PreparationStage]; ok {
			return l10n.T(locale, key)
		}
	}
	if t.Exit != nil {
		switch t.Exit.ExitCode {
		case inferyard.ExitComplete:
			return l10n.T(locale, "common.task.exit.complete")
		case inferyard.ExitBlocked:
			return l10n.T(locale, "common.task.exit.blocked")
		case inferyard.ExitIncomplete:
			return l10n.T(locale, "common.task.exit.incomplete")
		case inferyard.ExitToolError:
			return l10n.T(locale, "common.task.exit.tool_error")
		case inferyard.ExitCancelled:
			return l10n.T(locale, "common.task.exit.cancelled")
		}
	}
	if t.State == "starting" && t.Queued {
		return l10n.T(locale, "common.task.queued")
	}
	return stateLabelFor(locale, t.State)
}

var stateLabelKeys = map[string]string{
	"orphaned":          "common.state.orphaned",
	"":                  "common.missing",
	"stopped":           "common.state.stopped",
	"stopping":          "common.state.stopping",
	"starting":          "common.state.starting",
	"running":           "common.state.running",
	"cancelling":        "common.state.cancelling",
	"done":              "common.state.done",
	"failed":            "common.state.failed",
	"cancelled":         "common.state.cancelled",
	"downloading":       "common.state.downloading",
	"complete":          "common.state.complete",
	"partial":           "common.state.partial",
	"dirty":             "common.state.dirty",
	"pending":           "common.state.pending",
	"completed":         "common.state.completed",
	"invalid":           "common.state.invalid",
	"probe":             "common.state.probe",
	"warmup":            "common.state.warmup",
	"baseline":          "common.state.baseline",
	"formal":            "common.state.formal",
	"scoring":           "common.state.scoring",
	"residual":          "common.state.residual",
	"finalizing":        "common.state.finalizing",
	"candidate_missing": "common.state.candidate_missing",
	"bound_missing":     "common.state.bound_missing",
	"run_missing":       "common.state.run_missing",
	"run_incomplete":    "common.state.run_incomplete",
	"run_complete":      "common.state.run_complete",
	"preflight_missing": "common.state.preflight_missing",
}

func stateLabel(v string) string {
	return stateLabelFor("", v)
}

func stateLabelFor(locale, v string) string {
	if key, ok := stateLabelKeys[v]; ok {
		return l10n.T(locale, key)
	}
	return v
}
func (s *viewState) historyRuns() []core.RunSummary {
	runs := []core.RunSummary{}
	seen := map[string]bool{}
	add := func(r core.RunSummary) {
		if !seen[r.Path] {
			seen[r.Path] = true
			runs = append(runs, r)
		}
	}
	for _, b := range s.history.Batches {
		for _, r := range b.Runs {
			add(r)
		}
	}
	for _, r := range s.history.StandaloneRuns {
		add(r)
	}
	return runs
}
func number[T ~int64 | ~float64](v *T) string {
	if v == nil {
		return l10n.TR("common.missing")
	}
	return fmt.Sprint(*v)
}

func formatRate(v float64) string {
	switch {
	case v >= 1048576:
		return fmt.Sprintf("%.2f MiB/s", v/1048576)
	case v >= 1024:
		return fmt.Sprintf("%.1f KiB/s", v/1024)
	default:
		return fmt.Sprintf("%d bytes/s", int64(v))
	}
}

func groupedInt(v int64) string {
	s := fmt.Sprintf("%d", v)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i := range len(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func componentFold(c *ui.Context, title string, fn func()) {
	componentFoldKeyed(c, title, title, fn)
}

func componentFoldKeyed(c *ui.Context, key, title string, fn func()) {
	container := ui.Column(c.Key(key))
	open := ui.Local(container, "open", func() bool { return false })
	container.Children(func() { components.Collapsible(c, title, open, components.CollapsibleOptions{}, fn) })
}

func componentFoldOpen(c *ui.Context, key, title string, fn func()) {
	container := ui.Column(c.Key(key))
	open := ui.Local(container, "open", func() bool { return true })
	container.Children(func() { components.Collapsible(c, title, open, components.CollapsibleOptions{}, fn) })
}
