package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

type speedStage uint8

const (
	speedPreparing speedStage = iota
	speedBinding
	speedPlanning
	speedMeasuring
	speedReading
	speedDone
	speedCancelling
	speedCancelled
	speedFailed
	speedPartial
	speedQueued
)

type speedSample struct {
	context                  *int
	prompt                   *int
	seconds, prefill, decode *float64
	reason                   string
	missing                  string
}

type speedTask struct {
	selectionModel, selectionEngine string
	result                          *inferyard.ContextSpeedResult
	completed                       int
	budget                          float64
	id, model, engine               string
	startedAt                       string
	context                         int
	stage                           speedStage
	samples                         []speedSample
	summary                         *speedSample
	reason                          string
}

func (r *speedTask) active() bool {
	return r.stage <= speedReading || r.stage == speedCancelling || r.stage == speedQueued
}

var speedStageKeys = [...]string{
	"speed.stage.preparing",
	"speed.stage.binding",
	"speed.stage.planning",
	"speed.stage.measuring",
	"speed.stage.reading",
	"speed.stage.done",
	"speed.stage.cancelling",
	"speed.stage.cancelled",
	"speed.stage.failed",
	"speed.stage.partial",
	"speed.stage.queued",
}

var speedStageWireNames = [...]string{
	"preparing",
	"binding",
	"planning",
	"measuring",
	"reading",
	"done",
	"cancelling",
	"cancelled",
	"failed",
	"partial",
	"queued",
}

func (r *speedTask) toRunTask() core.RunTask {
	state := "running"
	queued := false
	switch r.stage {
	case speedPreparing, speedBinding, speedPlanning:
		state = "starting"
	case speedQueued:
		state, queued = "starting", true
	case speedCancelling:
		state = "cancelling"
	case speedDone, speedPartial:
		state = "done"
	case speedCancelled:
		state = "cancelled"
	case speedFailed:
		state = "failed"
	}
	var startedAt *string
	if r.startedAt != "" {
		s := r.startedAt
		startedAt = &s
	}
	stageName := ""
	if int(r.stage) >= 0 && int(r.stage) < len(speedStageWireNames) {
		stageName = speedStageWireNames[r.stage]
	}
	return core.RunTask{
		TaskId:    r.id,
		Kind:      "context_speed",
		State:     state,
		Queued:    queued,
		StartedAt: startedAt,
		ContextSpeed: &core.ContextSpeedTask{
			Model:     r.model,
			Engine:    r.engine,
			Context:   r.context,
			Stage:     stageName,
			Completed: r.completed,
		},
	}
}

func (r *speedTask) label() string {
	return l10n.TR(speedStageKeys[r.stage])
}

func (r *speedTask) validCount() int {
	if r.result != nil {
		return r.result.Measurement.ValidCount
	}
	n := 0
	for _, v := range r.samples {
		if v.prompt != nil && v.seconds != nil && v.prefill != nil && v.decode != nil && v.reason == "" {
			n++
		}
	}
	return n
}

type speedWorkspace struct {
	selected                     bool
	typeTab, typeTabSeen         int
	context                      int
	reading                      bool
	readSeq                      uint64
	current, opened              *speedTask
	history                      []*speedTask
	readError                    string
	table, samples, historyTable pageTableState
}

func (s *viewState) speedTypeSwitch(c *ui.Context) bool {
	if s.speed == nil {
		return false
	}
	w := s.speed
	if want := boolIndex(w.selected); w.typeTabSeen != want {
		w.typeTab, w.typeTabSeen = want, want
	}
	tabs := components.Tabs(c, &w.typeTab, components.TabsUnderline, tr(c, "speed.tab.standard"), tr(c, "speed.tab.context_speed"))
	if tabs.List.Changed() {
		w.selected, w.typeTabSeen = w.typeTab == 1, w.typeTab
	}
	return w.selected
}

func boolIndex(on bool) int {
	if on {
		return 1
	}
	return 0
}

func (s *viewState) speedSelection(c *ui.Context) bool {
	if s.speed == nil {
		return false
	}
	w := s.speed
	if !w.selected {
		return false
	}
	t := tokens(c)
	segmentedField(c, tr(c, "speed.select.context_tier"), "", &w.context, []components.NativeSelectOption[int]{
		{Value: inferyard.ContextSpeed64K, Label: "64k"},
		{Value: inferyard.ContextSpeed128K, Label: "128k"},
		{Value: inferyard.ContextSpeed256K, Label: "256k"},
	})
	applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).
		Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
		studioIcon(c, reicon.Gauge, t.Color.FgSecondary)
		ui.Text(c, trf(c, "speed.select.spec_summary", "bundle", inferyard.ContextSpeedBundleID, "samples", inferyard.ContextSpeedSamples)).
			FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgPrimary)
	})
	reason := s.speedStartBlockReason()
	if engineLocked(s.engineState.State) {
		if s.destructiveDialog(c, destructiveOption{
			kind: destructiveStopEngine, label: tr(c, "speed.stop_engine.label"), confirmLabel: tr(c, "speed.stop_engine.confirm"),
			prompt:   tr(c, "speed.stop_engine.prompt"),
			disabled: s.busy || s.engineState.State == "stopping",
		}) {
			s.stopEngine()
		}
	}
	if reason != "" {
		noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: reason})
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	if primaryButton(c, tr(c, "speed.action.start")).Disabled(reason != "").AlignSelf(ui.Start).Clicked() {
		s.startSpeed()
	}
	return true
}

func (s *viewState) speedStartBlockReason() string {
	if s.localEngine() != nil {
		return l10n.TR("speed.block.local_engine")
	}
	switch {
	case s.environmentMissing():
		return l10n.TR("speed.block.env_missing")
	case !s.preflightReady():
		return l10n.TR("speed.block.preflight")
	case s.newTaskPending:
		return l10n.TR("speed.block.prev_task")
	case s.busy:
		return l10n.TR("speed.block.busy")
	case s.running() != nil:
		return l10n.TR("speed.block.running")
	case s.model == "":
		return l10n.TR("speed.block.no_model")
	case s.engine == "":
		return l10n.TR("speed.block.no_engine")
	case s.engineState.State == "orphaned":
		return l10n.TR("speed.block.orphaned")
	case engineLocked(s.engineState.State) && !s.standaloneRunningForBenchmarkConflict():
		return l10n.TR("speed.block.engine_locked")
	}
	e := s.eligibility(s.engine)
	switch e.State {
	case core.EngineCheckEligible:
		for _, model := range s.registry.Models {
			if model.Name == s.model && model.ContextLength != nil && s.speed != nil && *model.ContextLength < int64(s.speed.context) {
				return l10n.TRf("speed.block.model_context_short", "max", fmt.Sprintf("%g", float64(*model.ContextLength)/1024), "target", s.speed.context/1024)
			}
		}
		return ""
	case core.EngineCheckChecking:
		return l10n.TR("speed.block.checking")
	case core.EngineCheckIneligible:
		return l10n.TRf("speed.block.ineligible", "reason", human(e.Reason))
	default:
		if e.Reason == "cli_outdated" {
			return l10n.TR("speed.block.cli_outdated")
		}
		return l10n.TR("speed.block.unverified")
	}
}

func speedPhaseCurrent(stage speedStage) int {
	switch stage {
	case speedPreparing, speedQueued:
		return 0
	case speedBinding:
		return 1
	case speedPlanning:
		return 2
	case speedMeasuring, speedCancelling:
		return 3
	case speedReading:
		return 4
	case speedDone, speedPartial:
		return 5
	default:
		return 3
	}
}

func speedStageStateKind(stage speedStage) stateKind {
	switch stage {
	case speedDone:
		return stateSuccess
	case speedPartial, speedCancelled, speedCancelling:
		return stateWarning
	case speedFailed:
		return stateDanger
	case speedQueued:
		return stateNeutral
	default:
		return stateActive
	}
}

func (s *viewState) speedTaskView(c *ui.Context) {
	t := tokens(c)
	r := s.speed.current
	s.flowHeader(c, tr(c, "speed.title"))

	current := speedPhaseCurrent(r.stage)
	steps, stepCount, stepKind := speedWorkflowSteps(c, current, r.stage == speedFailed, r)
	if r.stage == speedCancelled || r.stage == speedPartial {
		stepKind = stateWarning
	}
	pct := min(100, max(0, r.completed*20))
	barValue := float64(r.completed) / 5
	switch r.stage {
	case speedPreparing, speedBinding, speedPlanning, speedQueued, speedReading, speedCancelling:
		barValue = -1
	}
	pctLabel := ""
	if r.stage == speedMeasuring || r.stage == speedDone || r.stage == speedPartial {
		pctLabel = fmt.Sprintf("%d%%", pct)
	}
	prog := workflowProgressInfo{
		Active:       r.stage != speedDone,
		Value:        barValue,
		PercentLabel: pctLabel,
		AriaLabel:    tr(c, "speed.task.progress_bar_label"),
	}
	if r.stage == speedDone {
		prog.Active = true
		prog.Value = 1
	}

	pipelineCard(c, pipelineCardOptions{
		Key: "speed-task-card",
		Header: func() {
			ui.Row(c.Key("speed-target-card")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Stack).Children(func() {
				modelIconBox(c, s.modelIconHint(r.model), 36, t.Radius.Control)
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Tight).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						ui.Text(c, fmt.Sprintf("%s · %s · %s", r.model, r.engine, speedContext(r.context))).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
						inlineBadge(c, r.label(), speedStageStateKind(r.stage), badgeOptions{Size: sizeSM, Style: badgeDot})
					})
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if s.preview != nil {
							ui.Text(c, trf(c, "speed.task.meta_preview", "bundle", inferyard.ContextSpeedBundleID, "id", r.id)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
						} else {
							ui.Text(c, inferyard.ContextSpeedBundleID+" · "+r.id).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
						}
						if r.budget > 0 {
							ui.Text(c, "·").FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
							ui.Text(c, trf(c, "speed.task.time_budget", "seconds", fmt.Sprintf("%.0f", r.budget))).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
						}
					})
				})
				inlineBadge(c, stepCount, stepKind, badgeOptions{Size: sizeSM})
			})
		},
		Steps:     steps,
		Current:   current,
		Failed:    r.stage == speedFailed,
		RowLabel:  tr(c, "speed.task.progress_card"),
		StepCount: stepCount,
		StepKind:  stepKind,
		Progress:  prog,
		Body: func(embedBar bool) {
			elapsed := ""
			if r.stage == speedMeasuring {
				elapsed = s.formatTaskElapsed(c, r.startedAt)
			}
			ui.Row(c.Key("speed-elapsed-" + r.id)).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, trf(c, "speed.task.completed_count", "completed", r.completed)).Bold().FontFeatures("tnum")
				if elapsed != "" {
					ui.Text(c, "· "+elapsed).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
				}
				if embedBar {
					ui.Box(c).Grow(1)
					ui.Text(c, fmt.Sprintf("%d%%", pct)).Bold().FontSize(t.Type.Section).FontFeatures("tnum").TextColor(stateColor(c, speedStageStateKind(r.stage)))
				}
			})
			if embedBar {
				progressBar(c, barValue, tr(c, "speed.task.progress_bar_label"))
			}
			speedSampleSlots(c, r)

			if r.reason != "" {
				noticeBanner(c, noticeBannerOptions{Kind: stateWarning, Size: bannerCompact, Message: r.reason})
			}
			s.speedTaskActions(c, r)
		},
	})
}

func (s *viewState) speedTaskActions(c *ui.Context, r *speedTask) {
	if r.active() {
		if s.quietDestructiveAction(c, destructiveOption{
			kind: destructiveCancelRun, id: r.id, label: tr(c, "speed.cancel.label"), confirmLabel: tr(c, "speed.cancel.confirm"),
			prompt:   tr(c, "speed.cancel.prompt"),
			disabled: r.stage == speedCancelling,
		}) {
			s.cancelSpeed()
		}
		return
	}
	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(tokens(c).Density.Row).Children(func() {
		primaryButton(c, tr(c, "speed.action.view_result")).OnClick(func() { s.openSpeed(r.id) })
		outlineButton(c, tr(c, "speed.action.new_with_combo")).Disabled(s.busy).OnClick(func() { s.newSpeedTask(r) })
	})
}

func (s *viewState) newSpeedTask(r *speedTask) {
	if r.selectionModel == "" || r.selectionEngine == "" {
		s.failure = &core.ApiError{Code: "context_speed_assets_unavailable", Message: l10n.TR("speed.error.assets_unavailable")}
		return
	}
	s.newBenchmark()
	s.speed.selected, s.speed.context = true, r.context
	s.model, s.engine = r.model, r.engine
	if r.selectionModel != "" {
		s.model = r.selectionModel
	}
	if r.selectionEngine != "" {
		s.engine = r.selectionEngine
	}
}

func (s *viewState) openSpeed(id string) {
	s.closeOpenedReport()
	s.selected = "reports"
	if s.simulate(previewSpeedRead{id: id}) {
		return
	}
	var selected *speedTask
	for _, r := range s.speed.history {
		if r.id == id {
			copy := *r
			selected = &copy
			break
		}
	}
	if selected == nil {
		return
	}
	selected.samples, selected.summary, selected.result = nil, nil, nil
	if s.service == nil {
		return
	}
	apply := func(result *speedTask, e *core.ApiError) {
		s.speed.reading = false
		if e != nil {
			s.speed.readError = e.Message
			if s.speed.readError == "" {
				s.speed.readError = e.Code
			}
			if len(e.Limitations) > 0 {
				s.speed.readError += " · " + strings.Join(e.Limitations, l10n.TR("speed.list_sep"))
			}
			return
		}
		s.speed.opened = result
	}
	if s.win == nil && s.updateWindow == nil {
		s.speed.opened, s.speed.readError, s.speed.reading = selected, "", false
		result, e := readSpeedFlow(s.ctx, s.service, selected)
		apply(result, e)
		return
	}
	s.speed.readSeq++
	seq := s.speed.readSeq
	s.speed.opened, s.speed.readError, s.speed.reading = selected, "", true
	ctx, svc := s.ctx, s.service
	go func() {
		result, e := readSpeedFlow(ctx, svc, selected)
		s.updateView(func() {
			if s.speed.opened == nil || s.speed.opened.id != id || s.speed.readSeq != seq {
				return
			}
			apply(result, e)
		})
	}()
}
