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
		card: "Preview GPU · 24 GiB 共享", stage: speedPreparing}
	s.speed.current, s.speed.opened = r, nil
	s.busy = true
	fail, missing := p.takeFailure(), p.speedMissingTiming
	p.after("speed", 400*time.Millisecond, func() {
		r.stage = speedBinding
		s.engineState.State = "starting"
	})
	p.after("speed", 1100*time.Millisecond, func() {
		if fail {
			r.stage, r.reason = speedFailed, "模拟启动失败；可用此组合新开任务。"
			s.engineState.State = "failed"
			p.finishSpeed(r)
			return
		}
		s.engineState.State = "running"
		r.stage = speedPlanning
		p.after("speed", 400*time.Millisecond, func() {
			r.stage = speedMeasuring
			p.speedMissingTiming = false
			p.tickSpeed(r, missing)
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
		if len(r.samples) < 5 {
			p.tickSpeed(r, missing)
			return
		}
		r.stage = speedReading
		p.after("speed", 600*time.Millisecond, func() {
			if !missing {
				summary := r.samples[2]
				r.summary = &summary
			} else {
				r.reason = "执行已完成，但部分指标缺测，不显示完整汇总。"
			}
			r.stage = speedDone
			p.finishSpeed(r)
		})
	})
}

func previewSpeedSample(context, index int) speedSample {
	prompt := map[int]int{65536: 52625, 131072: 106294, 262144: 213612}[context]
	seconds := float64(prompt)/2000 + float64(index-2)*0.2
	return speedSample{prompt: previewValue(prompt), seconds: previewValue(seconds),
		prefill: previewValue(float64(prompt) / seconds), decode: previewValue(42.0)}
}

func previewSpeedHistory(s *viewState) {
	r := &speedTask{id: "preview-speed-history", model: s.model, engine: s.engine,
		context: 131072, card: "Preview GPU · 24 GiB 共享", stage: speedDone}
	for i := range 5 {
		r.samples = append(r.samples, previewSpeedSample(r.context, i))
	}
	summary := r.samples[2]
	r.summary = &summary
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
		w.opened, w.readError = r, ""
		w.table, w.samples = pageTableState{}, pageTableState{}
		if p.takeFailure() {
			w.readError = "模拟结果读取失败；重新读取不会再次运行模型。"
		}
		return
	}
	p.showNotice()
}
