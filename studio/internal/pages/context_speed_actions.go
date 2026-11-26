package pages

import (
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) startSpeed() {
	w := s.speed
	if w == nil || s.speedStartBlockReason() != "" || !inferyard.ValidContextSpeedContext(w.context) {
		return
	}
	if !s.guardStandaloneForBenchmark(func() { s.startSpeed() }) {
		return
	}
	if s.simulate(previewSpeedStart{model: s.model, engine: s.engine, context: w.context}) {
		return
	}
	request := core.StartContextSpeedRequest{Model: s.model, Engine: s.engine, Context: w.context}
	s.work(func() (string, *core.ApiError) {
		task, e := startSpeedFlow(s.service, request)
		if e == nil {
			s.win.Update(func() { s.speed.current = task; s.speed.opened = nil; s.benchFlowOpen = true })
		}
		return "", e
	})
}

func (s *viewState) cancelSpeed() {
	if s.speed == nil || s.speed.current == nil {
		return
	}
	if s.simulate(previewSpeedCancel{}) {
		return
	}
	id := s.speed.current.id
	s.work(func() (string, *core.ApiError) {
		return l10n.TR("speed.toast.cancel_requested"), cancelSpeedFlow(s.service, id)
	})
}

func speedFromTask(t core.RunTask) *speedTask {
	v := t.ContextSpeed
	r := &speedTask{id: t.TaskId, stage: speedFailed}
	if v == nil {
		r.reason = l10n.TR("speed.error.record_corrupted")
		return r
	}
	r.model, r.engine, r.context = v.Model, v.Engine, v.Context
	r.selectionModel, r.selectionEngine = v.Model, v.Engine
	if v.DisplayModel != "" {
		r.model = v.DisplayModel
	}
	if v.DisplayEngine != "" {
		r.engine = v.DisplayEngine
	}
	if r.model == "" {
		r.model = l10n.TR("speed.error.model_missing")
	}
	if r.engine == "" {
		r.engine = l10n.TR("speed.error.engine_missing")
	}
	stages := map[string]speedStage{"preparing": speedPreparing, "binding": speedBinding, "planning": speedPlanning, "queued": speedQueued, "measuring": speedMeasuring, "reading": speedReading, "done": speedDone, "failed": speedFailed, "cancelled": speedCancelled}
	if stage, ok := stages[v.Stage]; ok {
		r.stage = stage
	}
	if t.State == "cancelling" {
		r.stage = speedCancelling
	}
	if t.State == "failed" {
		r.stage = speedFailed
	}
	if t.State == "cancelled" {
		r.stage = speedCancelled
	}
	if v.Plan != nil {
		r.budget = v.Plan.MaxWallSeconds
	}
	if v.Batch != nil {
		r.completed = v.Completed
	}
	r.startedAt = emptyText(t.StartedAt)
	if t.Error != nil {
		r.reason = t.Error.Message
		if r.reason == "" {
			r.reason = t.Error.Code
		}
		if len(t.Error.Limitations) > 0 {
			r.reason += " · " + strings.Join(t.Error.Limitations, l10n.TR("speed.list_sep"))
		}
	}
	return r
}

func (s *viewState) syncSpeedTasks(tasks []core.RunTask) {
	if s.speed == nil {
		s.speed = &speedWorkspace{context: inferyard.ContextSpeed64K}
	}
	w := s.speed
	w.history = nil
	for _, task := range tasks {
		if task.ContextSpeed == nil {
			continue
		}
		r := speedFromTask(task)
		if w.current != nil && w.current.id == r.id {
			w.current = r
		}
		w.history = append(w.history, r)
	}
}

func applySpeedResult(task *speedTask, result inferyard.ContextSpeedResult) {
	task.result = &result
	task.model = result.Metadata.Model.DisplayName
	task.engine = result.Metadata.Engine.Release
	task.context = result.Metadata.SelectedContextTokens
	task.samples = nil
	task.summary = nil
	task.completed = result.Execution.Counts.Completed
	if task.stage != speedCancelled && result.Execution.Counts.NotExecuted+result.Execution.Counts.Cancelled+result.Execution.Counts.Invalid > 0 {
		task.stage = speedPartial
	}
	loc := l10n.CurrentLocale()
	task.reason = speedReasonsLabel(loc, result.Measurement.Reasons)
	for _, sample := range result.Samples {
		c := sample.Columns
		projected := speedSample{seconds: c.Time.Value, prefill: c.Prefill.Value, decode: c.Decode.Value, reason: speedReasonsLabel(loc, sample.Measurement.Reasons)}
		if c.Context.Value != nil {
			n := int(*c.Context.Value)
			projected.context = &n
		}
		if c.PromptLength.Value != nil {
			n := int(*c.PromptLength.Value)
			projected.prompt = &n
		}
		projected.missing = speedMissingLabels(loc, c)
		task.samples = append(task.samples, projected)
	}
	if result.Summary != nil {
		c := result.Summary.Columns
		v := speedSample{seconds: &c.Time.Median, prefill: &c.Prefill.Median, decode: &c.Decode.Median}
		if c.Context.Value != nil {
			n := int(*c.Context.Value)
			v.context = &n
		}
		if c.PromptLength.Value != nil {
			n := int(*c.PromptLength.Value)
			v.prompt = &n
		}
		task.summary = &v
	}
}

func humanSpeedReason(loc, code string) string {
	key := "speed.reason." + code
	if text := l10n.T(loc, key); text != key {
		return text
	}
	return code
}

func speedReasonsLabel(loc string, codes []string) string {
	var reasons []string
	for _, code := range codes {
		reasons = append(reasons, humanSpeedReason(loc, code))
	}
	return strings.Join(reasons, l10n.T(loc, "speed.list_sep"))
}

func speedMissingLabels(loc string, c inferyard.SpeedColumns) string {
	var missing []string
	for _, item := range []struct {
		metric string
		reason *string
	}{
		{"speed.metric.context", c.Context.MissingReason},
		{"speed.metric.prompt_length", c.PromptLength.MissingReason},
		{"speed.metric.input_time", c.Time.MissingReason},
		{"speed.metric.prefill", c.Prefill.MissingReason},
		{"speed.metric.decode", c.Decode.MissingReason},
	} {
		if item.reason != nil {
			missing = append(missing, l10n.Tf(loc, "speed.result.missing_item",
				"metric", l10n.T(loc, item.metric), "reason", humanSpeedReason(loc, *item.reason)))
		}
	}
	return strings.Join(missing, l10n.T(loc, "speed.missing_sep"))
}
