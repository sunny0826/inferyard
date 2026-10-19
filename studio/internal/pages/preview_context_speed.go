//go:build ui_debug && !release

package pages

import (
	"time"

	"inferyard/studio/internal/core"
)

func (p *previewSession) startSpeed(op previewSpeedStart) {
	s := p.state
	validContext := op.context == 65536 || op.context == 131072 || op.context == 262144
	if s.speed == nil || s.busy || s.running() != nil || s.environmentMissing() || !s.preflightReady() ||
		op.model == "" || op.engine == "" || op.model != s.model || op.engine != s.engine ||
		s.eligibility(op.engine).State != core.EngineCheckEligible || !validContext ||
		engineLocked(s.engineState.State) || s.engineState.State == "orphaned" {
		p.showNotice()
		return
	}
	r := &speedTask{id: p.nextID("speed"), model: op.model, engine: op.engine, context: op.context,
		stage: speedPreparing, selectionModel: op.model, selectionEngine: op.engine}
	s.speed.current, s.speed.opened = r, nil
	s.busy = true
	fail, missing := p.takeFailure(), p.speedMissingTiming
	failureStage, failure := p.speedFailureStage, p.speedFailure
	p.speedFailureStage, p.speedFailure = "", nil
	reject := func(stage string) bool {
		if failureStage != stage || failure == nil {
			return false
		}
		projected := speedFromTask(core.RunTask{TaskId: r.id, State: "failed", Error: failure, ContextSpeed: &core.ContextSpeedTask{Stage: "failed", Model: r.model, Engine: r.engine, Context: r.context}})
		r.stage, r.reason = speedFailed, projected.reason
		if stage == "binding" {
			s.engineState.State = "failed"
		}
		p.finishSpeed(r)
		return true
	}
	p.after("speed", 400*time.Millisecond, func() {
		if reject("preparing") {
			p.cancelEvents("speed")
			return
		}
		r.stage = speedBinding
		s.engineState.State = "starting"
	})
	p.after("speed", 1100*time.Millisecond, func() {
		if reject("binding") {
			return
		}
		if fail {
			r.stage, r.reason = speedFailed, core.Err("engine_start_failed").Message
			if r.reason == "" {
				r.reason = "engine_start_failed"
			}
			s.engineState.State = "failed"
			p.finishSpeed(r)
			return
		}
		s.engineState.State = "running"
		r.stage = speedPlanning
		p.after("speed", 400*time.Millisecond, func() {
			if reject("planning") {
				return
			}
			r.stage = speedQueued
			p.after("speed", 400*time.Millisecond, func() {
				r.stage = speedMeasuring
				p.speedMissingTiming = false
				p.tickSpeed(r, missing)
			})
		})
	})
}

func (p *previewSession) tickSpeed(r *speedTask, missing bool) {
	p.after("speed", time.Second, func() {
		sample := previewSpeedSample(r.context, len(r.samples))
		if missing && len(r.samples) == 4 {
			sample.seconds, sample.prefill, sample.decode = nil, nil, nil
			sample.reason = "引擎阶段计时缺失（模拟）；速度与耗时缺测。"
		}
		r.samples = append(r.samples, sample)
		r.completed = len(r.samples)
		if len(r.samples) < 5 {
			p.tickSpeed(r, missing)
			return
		}
		r.stage = speedReading
		p.after("speed", 600*time.Millisecond, func() {
			r.stage = speedDone
			applySpeedResult(r, previewSpeedResult(r, 5, missing))
			p.finishSpeed(r)
		})
	})
}

func previewSpeedSample(context, index int) speedSample {
	prompt := map[int]int{65536: 52625, 131072: 106294, 262144: 213612}[context]
	seconds := float64(prompt)/2000 + float64(index-2)*0.2
	return speedSample{context: previewValue(context), prompt: previewValue(prompt), seconds: previewValue(seconds),
		prefill: previewValue(float64(prompt) / seconds), decode: previewValue(42.0)}
}

func previewSpeedHistory(s *viewState) {
	r := &speedTask{id: "preview-speed-history", model: s.model, engine: s.engine,
		context: 131072, stage: speedDone}
	r.selectionModel, r.selectionEngine = s.model, s.engine
	applySpeedResult(r, previewSpeedResult(r, 5, false))
	s.speed.history = []*speedTask{r}
	s.selected = "reports"
}

func (p *previewSession) finishSpeed(r *speedTask) {
	p.state.busy = false
	p.state.speed.history = append(p.state.speed.history, r)
}

func (p *previewSession) cancelSpeed(after func()) {
	s := p.state
	if s.speed == nil || s.speed.current == nil {
		return
	}
	r := s.speed.current
	if !r.active() {
		return
	}
	if r.stage == speedCancelling {
		if after != nil {
			p.after("speed", 500*time.Millisecond, after)
		}
		return
	}
	p.cancelEvents("speed")
	r.stage = speedCancelling
	p.after("speed", 500*time.Millisecond, func() {
		r.stage, r.reason = speedCancelled, "任务已取消；保留已经完成的测量，不补跑。"
		if s.engineState.State == "starting" {
			s.engineState.State = "stopped"
		}
		p.finishSpeed(r)
		if after != nil {
			after()
		}
	})
}

func (p *previewSession) readSpeed(id string) {
	w := p.state.speed
	if w == nil {
		p.showNotice()
		return
	}
	for _, r := range w.history {
		if r.id != id {
			continue
		}
		if r.stage == speedCancelled && r.result == nil {
			applySpeedResult(r, previewSpeedResult(r, r.completed, false))
		}
		w.opened, w.readError = r, ""
		w.table, w.samples = pageTableState{}, pageTableState{}
		if p.takeFailure() {
			copy := *r
			copy.samples, copy.summary, copy.result = nil, nil, nil
			w.opened = &copy
			w.readError = "模拟结果读取失败；重新读取不会再次运行模型。"
		}
		return
	}
	p.showNotice()
}
