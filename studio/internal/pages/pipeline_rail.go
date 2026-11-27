package pages

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/icons/reicon"
)

type workflowStepItem struct {
	Label   string
	Summary string
}

type workflowProgressInfo struct {
	Active       bool
	Value        float64 // 0..1 or <0 for indeterminate
	PercentLabel string
	AriaLabel    string
}

type pipelineCardOptions struct {
	Key        string
	ModeSwitch func()
	Header     func()
	Steps      []workflowStepItem
	Current    int
	Failed     bool
	RowLabel   string
	StepCount  string
	StepKind   stateKind
	Progress   workflowProgressInfo
	SubHeader  func()
	Body       func(embedBar bool)
}

type pipelineAnimState struct {
	initialized bool
	lastN       int
	weights     []float32
	fillValue   float32
	pointerX    float32
	hasPointer  bool
	lastAt      time.Time
}

func pipelineTargetWeights(n, current int) []float32 {
	weights := make([]float32, n)
	for i := range n {
		if i == current {
			weights[i] = 2.6
		} else {
			weights[i] = 1.0
		}
	}
	return weights
}

func pipelineStepScale(c *ui.Context, failed bool) stateColorScale {
	if failed {
		return tokens(c).Color.Danger
	}
	return tokens(c).Color.Active
}

// pipelineCard renders an integrated single-card workflow container whose top
// segmented PipelineRail merges macro step progression and live active-stage
// progress into a single physical track.
func pipelineCard(c *ui.Context, spec pipelineCardOptions) {
	t := tokens(c)
	col := ui.Column(c)
	if spec.Key != "" {
		col = ui.Column(c.Key(spec.Key))
	}
	applyMaterial(c, col.FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		if spec.Header != nil {
			spec.Header()
		}
		if spec.ModeSwitch != nil || (spec.StepCount != "" && spec.Header == nil) {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if spec.ModeSwitch != nil {
					ui.Box(c).Grow(1).MinWidth(0).Children(spec.ModeSwitch)
				} else {
					ui.Text(c, spec.RowLabel).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Grow(1).SingleLine()
				}
				if spec.StepCount != "" {
					inlineBadge(c, spec.StepCount, spec.StepKind, badgeOptions{Size: sizeSM})
				}
			})
		}
		if len(spec.Steps) > 0 {
			pipelineRail(c, spec)
		}
		if spec.SubHeader != nil {
			// Concentric radius (design-1): Card surface (20) - padding (12) = Inset (8).
			applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Section).
				Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(spec.SubHeader)
		}
		if spec.Body != nil {
			embedBar := !spec.Progress.Active || len(spec.Steps) == 0
			ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Children(func() {
				spec.Body(embedBar)
			})
		}
	})
}

// ambientLifecycleDot renders a single persistent 10x10 lifecycle indicator
// (motion-22): softly pulsing halo while active, steady solid dot while idle or
// failed, and respecting ReduceMotion (accessibility-2).
func ambientLifecycleDot(c *ui.Context, scale stateColorScale, pulsing bool) {
	reduced := c.Preferences().ReduceMotion
	ui.Box(c).Size(10, 10).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		if pulsing && !reduced {
			p.AnimationFrame()
			phase := float64(p.Now().UnixMilli()%1800) / 1800
			wave := float32((1 - math.Cos(phase*2*math.Pi)) / 2) // 0 -> 1 -> 0
			haloR := float32(3.6 + 1.6*wave)
			haloAlpha := float32(0.32 - 0.18*wave)
			p.Fill(ui.Rect{X: cx - haloR, Y: cy - haloR, W: haloR * 2, H: haloR * 2}, scale.Solid.Alpha(haloAlpha), haloR)
		} else if pulsing {
			haloR := float32(4.6)
			p.Fill(ui.Rect{X: cx - haloR, Y: cy - haloR, W: haloR * 2, H: haloR * 2}, scale.Solid.Alpha(.22), haloR)
		}
		coreR := float32(3.0)
		p.Fill(ui.Rect{X: cx - coreR, Y: cy - coreR, W: coreR * 2, H: coreR * 2}, scale.Solid, coreR)
	})
}

// pipelineRail renders optically aligned weighted labels directly above a
// Painter-drawn segmented live progress track.
func pipelineRail(c *ui.Context, spec pipelineCardOptions) {
	t := tokens(c)
	n := len(spec.Steps)
	if n == 0 {
		return
	}
	targetWeights := pipelineTargetWeights(n, spec.Current)
	activeScale := pipelineStepScale(c, spec.Failed)
	reduced := c.Preferences().ReduceMotion
	dark := c.Theme().Dark
	highContrast := c.Preferences().HighContrast

	hostKey := spec.Key
	if hostKey == "" {
		hostKey = spec.RowLabel
	}
	host := ui.Column(c.Key("pipeline-rail-" + hostKey)).FillWidth().MinWidth(0).Gap(t.Density.Tight).Label(spec.RowLabel)
	anim := ui.Local(host, "anim", func() pipelineAnimState {
		return pipelineAnimState{
			initialized: true,
			lastN:       n,
			weights:     append([]float32(nil), targetWeights...),
			fillValue:   float32(max(0, min(1, spec.Progress.Value))),
		}
	})
	if !anim.initialized || anim.lastN != n || len(anim.weights) != n {
		anim.initialized = true
		anim.lastN = n
		anim.weights = append([]float32(nil), targetWeights...)
		anim.fillValue = float32(max(0, min(1, spec.Progress.Value)))
		anim.hasPointer = false
	}

	// Step weights smoothly toward target (motion-11 / motion-12 / accessibility-2).
	if reduced {
		copy(anim.weights, targetWeights)
	}

	host.Children(func() {
		// Optically aligned label row (design-2): exact same weights and 4px gap as segments below.
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
			for i, st := range spec.Steps {
				ui.Row(c).Grow(targetWeights[i]).MinWidth(0).AlignItems(ui.Center).Gap(4).Children(func() {
					switch {
					case i < spec.Current:
						studioIcon(c, reicon.Check, t.Color.Success.Fg)
						ui.Text(c, st.Label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
					case i == spec.Current:
						ambientLifecycleDot(c, activeScale, spec.Progress.Active && !spec.Failed)
						ui.Text(c, st.Label).Bold().FontSize(t.Type.Caption).TextColor(activeScale.Fg).SingleLine()
						if spec.Progress.PercentLabel != "" {
							ui.Text(c, spec.Progress.PercentLabel).Bold().FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(activeScale.Fg).SingleLine()
						}
					default:
						ui.Text(c, fmt.Sprintf("%d", i+1)).FontSize(t.Type.ShellDetail).FontFeatures("tnum").TextColor(t.Color.FgTertiary).SingleLine()
						ui.Text(c, st.Label).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).SingleLine()
					}
				})
			}
		})

		barVal := float64(min(n, max(0, spec.Current))) / float64(max(1, n))
		barLabel := spec.RowLabel
		if spec.Progress.Active {
			barVal = spec.Progress.Value
			if spec.Progress.AriaLabel != "" {
				barLabel = spec.Progress.AriaLabel
			}
		}
		rail := ui.Box(c).FillWidth().Height(24)
		if spec.Progress.Active {
			rail.Role(ui.RoleProgress).Range(0, 1, barVal).Label(barLabel)
		}

		// Inset ring specular highlight (design-3 / design-10).
		insetRing := ui.RGBA(255, 255, 255, .18)
		if dark {
			insetRing = ui.RGBA(255, 255, 255, .12)
		}
		sheenColor := activeScale.Solid.Mix(ui.RGB(255, 255, 255), .26)
		if dark {
			sheenColor = activeScale.Solid.Mix(ui.RGB(255, 255, 255), .20)
		}

		rail.Draw(func(p *ui.Painter, r ui.Rect) {
			if r.W <= 0 || r.H <= 0 || n == 0 {
				return
			}
			now := p.Now()
			dt := float32(0.016)
			if !anim.lastAt.IsZero() {
				if elapsed := float32(now.Sub(anim.lastAt).Seconds()); elapsed > 0 && elapsed < 0.2 {
					dt = elapsed
				}
			}
			anim.lastAt = now
			// Interruptible exponential damping ~220ms settle (motion-11 / motion-12).
			alpha := float32(1 - math.Exp(float64(-12*dt)))
			if reduced {
				alpha = 1
			}
			stillMoving := false
			for i := range n {
				diff := targetWeights[i] - anim.weights[i]
				if float32(math.Abs(float64(diff))) > 0.005 {
					anim.weights[i] += diff * alpha
					stillMoving = true
				} else {
					anim.weights[i] = targetWeights[i]
				}
			}

			val := spec.Progress.Value
			if math.IsNaN(val) || math.IsInf(val, 0) || val > 1 {
				val = -1
			}
			if spec.Progress.Active && val >= 0 {
				targetFill := float32(val)
				diff := targetFill - anim.fillValue
				if float32(math.Abs(float64(diff))) > 0.002 {
					anim.fillValue += diff * alpha
					stillMoving = true
				} else {
					anim.fillValue = targetFill
				}
			} else if !spec.Progress.Active {
				anim.fillValue = 0
			}

			const gap float32 = 4
			const barH float32 = 12
			barY := r.Y + 1
			rad := barH / 2
			totalW := float32(0)
			for _, w := range anim.weights {
				totalW += w
			}
			avail := max(0, r.W-gap*float32(n-1))
			x := r.X
			targetPtrX := float32(-1)

			for i := range n {
				segW := avail * (anim.weights[i] / totalW)
				seg := ui.Rect{X: x, Y: barY, W: max(6, segW), H: barH}
				inner := ui.Rect{X: seg.X + 1, Y: seg.Y + 1, W: max(0, seg.W-2), H: max(0, seg.H-2)}
				innerR := inner.H / 2

				switch {
				case i < spec.Current:
					p.Fill(seg, t.Color.Success.Solid, rad)
					if !highContrast {
						p.Stroke(inner, insetRing, innerR, 1)
					}
				case i == spec.Current:
					targetPtrX = seg.X + seg.W/2
					borderCol := activeScale.Border
					if highContrast {
						borderCol = activeScale.Solid
					}
					p.Fill(seg, borderCol, rad)
					p.Fill(inner, t.Color.Sunken, innerR)

					if spec.Failed {
						p.Fill(inner, t.Color.Danger.Solid, innerR)
					} else if spec.Progress.Active {
						if val < 0 {
							phase := 0.25
							if !reduced {
								p.AnimationFrame()
								phase = float64(now.UnixMilli()%1400) / 1400
							}
							fillW := inner.W * 0.34
							fx := inner.X + (inner.W-fillW)*float32((1-math.Cos(phase*2*math.Pi))/2)
							p.Clip(inner, innerR, func() {
								p.Fill(ui.Rect{X: fx, Y: inner.Y, W: fillW, H: inner.H}, activeScale.Solid, innerR)
							})
						} else if anim.fillValue > 0 {
							fillW := min(inner.W, max(inner.H, inner.W*anim.fillValue))
							fillRect := ui.Rect{X: inner.X, Y: inner.Y, W: fillW, H: inner.H}
							p.Clip(inner, innerR, func() {
								p.Fill(fillRect, activeScale.Solid, innerR)
								if !highContrast {
									p.Stroke(fillRect, insetRing, innerR, 1)
								}
								if anim.fillValue < 0.995 && !reduced && fillW >= 24 {
									p.AnimationFrame()
									phase := float32(now.UnixMilli()%1800) / 1800
									bandW := max(18, fillW*0.35)
									bandX := inner.X - bandW + (fillW+bandW*2)*phase
									p.Clip(fillRect, innerR, func() {
										p.Fill(ui.Rect{X: bandX, Y: inner.Y, W: bandW, H: inner.H}, sheenColor, innerR)
									})
								}
							})
						}
					} else {
						// Waiting / configuring stage: soft fill + leading progress anchor.
						p.Fill(inner, activeScale.SubtleBg, innerR)
						anchorW := min(inner.W*0.26, 40)
						p.Fill(ui.Rect{X: inner.X, Y: inner.Y, W: max(inner.H, anchorW), H: inner.H}, activeScale.Solid, innerR)
					}
				default:
					trackBorder := t.Color.BorderSubtle
					if highContrast {
						trackBorder = t.Color.BorderDefault
					}
					p.Fill(seg, trackBorder, rad)
					p.Fill(inner, t.Color.Sunken, innerR)
				}
				x += segW + gap
			}

			// Bottom divider hairline unifying rail and workspace.
			lineY := r.Y + r.H - 1
			p.Fill(ui.Rect{X: r.X, Y: lineY, W: r.W, H: 1}, t.Color.BorderSubtle, 0)

			// Sliding optical connector latch tying active segment to workspace (design-2 / motion-17).
			if targetPtrX >= 0 {
				if !anim.hasPointer || reduced {
					anim.pointerX = targetPtrX
					anim.hasPointer = true
				} else {
					diff := targetPtrX - anim.pointerX
					if float32(math.Abs(float64(diff))) > 0.5 {
						anim.pointerX += diff * alpha
						stillMoving = true
					} else {
						anim.pointerX = targetPtrX
					}
				}
				cx := anim.pointerX
				neckTop := barY + barH + 1
				neckH := max(2, lineY-neckTop)
				// Subtle vertical stem + grounded accent foot resting cleanly on the divider.
				p.Fill(ui.Rect{X: cx - 1, Y: neckTop, W: 2, H: neckH}, activeScale.Solid.Alpha(.45), 1)
				footW := float32(28)
				if highContrast {
					footW = 36
				}
				p.Fill(ui.Rect{X: cx - footW/2, Y: lineY - 1, W: footW, H: 2.5}, activeScale.Solid, 1.25)
			}

			if stillMoving && !reduced {
				p.AnimationFrame()
			}
		})
	})
}
