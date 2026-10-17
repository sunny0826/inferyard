//go:build ui_debug && !release

package pages

import (
	"fmt"
	"slices"
	"time"
)

type previewEvent struct {
	at         time.Duration
	generation int
	group      string
	apply      func()
}

func (p *previewSession) after(group string, delay time.Duration, apply func()) {
	p.jobs = append(p.jobs, previewEvent{p.clock + delay, p.generation, group, apply})
}

func (p *previewSession) cancelEvents(group string) {
	p.jobs = slices.DeleteFunc(p.jobs, func(event previewEvent) bool { return event.group == group })
}

func (p *previewSession) advance(delta time.Duration) {
	if p.ctx.Err() != nil || delta < 0 {
		return
	}
	target := p.clock + delta
	for {
		if p.ctx.Err() != nil {
			return
		}
		next := -1
		for i, event := range p.jobs {
			if event.at <= target && (next == -1 || event.at < p.jobs[next].at) {
				next = i
			}
		}
		if next == -1 {
			break
		}
		event := p.jobs[next]
		p.jobs = slices.Delete(p.jobs, next, next+1)
		p.clock = event.at
		if event.generation == p.generation {
			event.apply()
		}
	}
	p.clock = target
	if p.state.detailID != "" {
		p.state.detail = p.detail(p.state.detailID)
	}
}

func (p *previewSession) runClock() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case now := <-ticker.C:
			p.win.Update(func() {
				if p.ctx.Err() != nil || !now.After(p.lastTick) {
					return
				}
				p.advance(now.Sub(p.lastTick))
				p.lastTick = now
			})
		}
	}
}

func (p *previewSession) nextID(kind string) string {
	p.serial++
	return fmt.Sprintf("preview-%s-%03d", kind, p.serial)
}

func (p *previewSession) timestamp() string {
	return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC).Add(p.clock).Format(time.RFC3339)
}

func (p *previewSession) takeFailure() bool {
	fail := p.failNext
	p.failNext = false
	return fail
}
