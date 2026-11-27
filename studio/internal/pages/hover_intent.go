package pages

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	labelOpenDelay     = 500 * time.Millisecond
	labelWarmWindow    = 400 * time.Millisecond
	labelEnterDuration = 140 * time.Millisecond
	labelExitDuration  = 60 * time.Millisecond

	cardOpenDelay     = 700 * time.Millisecond
	cardCloseGrace    = 300 * time.Millisecond
	cardEnterDuration = 200 * time.Millisecond
	cardExitDuration  = 120 * time.Millisecond

	previewOpenDelay     = 900 * time.Millisecond
	previewCloseGrace    = 400 * time.Millisecond
	previewEnterDuration = 250 * time.Millisecond
	previewExitDuration  = 140 * time.Millisecond

	swapCrossFadeDuration       = 200 * time.Millisecond
	pointerVelocityWindow       = 90 * time.Millisecond
	pointerFastThresholdPxPerMs = 0.1
	pointerSlowSwapDuration     = 60 * time.Millisecond
)

type pointerSample struct {
	x, y float32
	at   time.Time
}

type hoverCoordinator struct {
	samples           [32]pointerSample
	sampleCount       int
	px, py            float32
	inWin, hadPointer bool
	lastPointerMoveAt time.Time
	slowSince         time.Time

	initialized, frameCommitted    bool
	passShown                      bool
	passAnchorID, passGroupID      uint64
	passRect                       ui.Rect
	passText                       string
	anyHoverInPass, anyFocusInPass bool
	groupStack                     [8]uint64
	groupDepth                     int
	suppressed, prevSuppressed     [16]uint64
	suppressedCount                int
	prevSuppressedCount            int

	prevShown                  bool
	prevAnchorID, prevGroupID  uint64
	prevRect                   ui.Rect
	prevText                   string
	lastShownAt                time.Time
	lastShownGroupID           uint64
	hoverID, hoverGroupID      uint64
	hoverSince, focusSince     time.Time
	focusID, dismissedID       uint64
	touchID                    uint64
}

func frameNow(c *ui.Context) time.Time {
	if now := c.Now(); !now.IsZero() {
		return now
	}
	return time.Now()
}

func pointInRect(x, y float32, r ui.Rect) bool {
	return r.W > 0 && r.H > 0 && x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H
}

func syncHoverCoordinator(c *ui.Context) *hoverCoordinator {
	root := c.Root()
	if !root.Valid() {
		return &hoverCoordinator{}
	}
	coord := ui.Local(root, "studio-hover-coord", func() hoverCoordinator {
		return hoverCoordinator{frameCommitted: true}
	})
	if !root.Changed() {
		root.SetChanged()
		now := frameNow(c)
		if coord.frameCommitted {
			if coord.initialized {
				if coord.passShown {
					coord.lastShownAt = now
					coord.lastShownGroupID = coord.passGroupID
				}
				coord.prevShown, coord.prevAnchorID, coord.prevGroupID = coord.passShown, coord.passAnchorID, coord.passGroupID
				coord.prevRect, coord.prevText = coord.passRect, coord.passText
				coord.prevSuppressed, coord.prevSuppressedCount = coord.suppressed, coord.suppressedCount
				if !coord.anyHoverInPass {
					coord.hoverID, coord.hoverGroupID = 0, 0
				}
				if !coord.anyFocusInPass {
					coord.focusID = 0
				}
				if coord.dismissedID != 0 && coord.dismissedID != coord.hoverID &&
					coord.dismissedID != coord.focusID && !coord.isSuppressed(coord.dismissedID) {
					coord.dismissedID = 0
				}
			}
			coord.initialized, coord.frameCommitted = true, false
			px, py, inWin := root.PointerPosition()
			coord.recordPointer(px, py, inWin, now)
			if coord.touchID != 0 && root.Pressed() {
				coord.touchID = 0
			}
		}
		coord.passShown, coord.passAnchorID, coord.passGroupID = false, 0, 0
		coord.passRect, coord.passText = ui.Rect{}, ""
		coord.anyHoverInPass, coord.anyFocusInPass = false, false
		coord.groupDepth, coord.suppressedCount = 0, 0

		ui.Overlay(c, func() {
			ui.Box(c.Key("studio-hover-frame-end")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().
				Draw(func(*ui.Painter, ui.Rect) {
					coord.frameCommitted = true
				})
		})
	}
	return coord
}

func (h *hoverCoordinator) recordPointer(px, py float32, inWin bool, now time.Time) {
	if !inWin {
		h.inWin = false
		h.hadPointer = false
		h.sampleCount = 0
		h.slowSince = time.Time{}
		return
	}
	moved := h.hadPointer && (px != h.px || py != h.py)
	h.px, h.py, h.inWin = px, py, true
	if !h.hadPointer {
		h.hadPointer = true
		h.samples[0] = pointerSample{x: px, y: py, at: now}
		h.sampleCount = 1
		h.slowSince = now
		return
	}
	if moved {
		h.lastPointerMoveAt = now
		if h.sampleCount == len(h.samples) {
			copy(h.samples[:], h.samples[1:])
			h.sampleCount--
		}
		h.samples[h.sampleCount] = pointerSample{x: px, y: py, at: now}
		h.sampleCount++
	}
	cutoff := now.Add(-pointerVelocityWindow)
	keepFrom := 0
	for keepFrom+1 < h.sampleCount && h.samples[keepFrom+1].at.Before(cutoff) {
		keepFrom++
	}
	if keepFrom > 0 {
		copy(h.samples[:], h.samples[keepFrom:h.sampleCount])
		h.sampleCount -= keepFrom
	}
	if h.speedPxPerMs(now) > pointerFastThresholdPxPerMs {
		h.slowSince = time.Time{}
	} else if h.slowSince.IsZero() {
		h.slowSince = now
	}
}

func (h *hoverCoordinator) speedPxPerMs(now time.Time) float64 {
	if !h.inWin || h.sampleCount == 0 {
		return 0
	}
	cutoff := now.Add(-pointerVelocityWindow)
	var dist float64
	prevX, prevY := h.samples[0].x, h.samples[0].y
	prevAt := h.samples[0].at
	for i := 1; i < h.sampleCount; i++ {
		cur := h.samples[i]
		if cur.at.After(cutoff) {
			dx := float64(cur.x - prevX)
			dy := float64(cur.y - prevY)
			seg := math.Hypot(dx, dy)
			if prevAt.Before(cutoff) && cur.at.After(prevAt) {
				frac := float64(cur.at.Sub(cutoff)) / float64(cur.at.Sub(prevAt))
				seg *= math.Max(0, math.Min(1, frac))
			}
			dist += seg
		}
		prevX, prevY, prevAt = cur.x, cur.y, cur.at
	}
	return dist / float64(pointerVelocityWindow.Milliseconds())
}

func (h *hoverCoordinator) isFast(c *ui.Context, now time.Time) bool {
	if h.speedPxPerMs(now) <= pointerFastThresholdPxPerMs {
		return false
	}
	if !h.lastPointerMoveAt.IsZero() {
		if rem := pointerVelocityWindow - now.Sub(h.lastPointerMoveAt); rem > 0 {
			c.After(rem + time.Millisecond)
		} else {
			c.After(16 * time.Millisecond)
		}
	}
	return true
}

func (h *hoverCoordinator) restedSlowlyFor(c *ui.Context, now time.Time, d time.Duration) bool {
	if h.isFast(c, now) {
		return false
	}
	if h.slowSince.IsZero() {
		h.slowSince = now
	}
	if wait := d - now.Sub(h.slowSince); wait > 0 {
		c.After(wait)
		return false
	}
	return true
}

func (h *hoverCoordinator) currentGroup() uint64 {
	if h.groupDepth <= 0 {
		return 0
	}
	return h.groupStack[h.groupDepth-1]
}

func (h *hoverCoordinator) suppress(id uint64) {
	if id == 0 {
		return
	}
	h.dismissedID = id
	if h.suppressedCount < len(h.suppressed) {
		h.suppressed[h.suppressedCount] = id
		h.suppressedCount++
	}
}

func (h *hoverCoordinator) isSuppressed(id uint64) bool {
	for i := 0; i < h.suppressedCount; i++ {
		if h.suppressed[i] == id {
			return true
		}
	}
	for i := 0; i < h.prevSuppressedCount; i++ {
		if h.prevSuppressed[i] == id {
			return true
		}
	}
	return false
}

func suppressTooltipFor(c *ui.Context, anchor ui.Element) {
	if !anchor.Valid() {
		return
	}
	syncHoverCoordinator(c).suppress(anchor.ID())
}

// studioTooltipGroup groups adjacent tooltipped triggers (such as a toolbar or action row)
// so moving across inter-button gaps swaps labels in place without unmounting or flickering.
func studioTooltipGroup(c *ui.Context, container ui.Element, children func()) ui.Element {
	if !container.Valid() {
		if children != nil {
			children()
		}
		return container
	}
	coord := syncHoverCoordinator(c)
	groupID := container.ID()
	pushed := false
	if coord.groupDepth < len(coord.groupStack) {
		coord.groupStack[coord.groupDepth] = groupID
		coord.groupDepth++
		pushed = true
	}
	container.Children(children)
	if pushed {
		coord.groupDepth--
	}

	groupHovered := container.Hovered() || (coord.inWin && pointInRect(coord.px, coord.py, container.Bounds()))
	if !coord.passShown && groupHovered && coord.prevShown && coord.prevGroupID == groupID &&
		coord.dismissedID == 0 && coord.prevText != "" && coord.prevRect.W > 0 {
		r, txt := coord.prevRect, coord.prevText
		coord.passShown, coord.passAnchorID, coord.passGroupID = true, coord.prevAnchorID, groupID
		coord.passRect, coord.passText = r, txt
		ui.Overlay(c, func() {
			phantom := ui.Box(c.Key("studio-label-phantom")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).PassThrough()
			renderLabelTipOverlayNode(c, coord, phantom, coord.prevAnchorID, txt)
		})
	}
	return container
}

// studioTooltip attaches a Studio label tooltip to anchor with 500ms rest open delay,
// 0ms warm handoff within 400ms, 140ms/60ms motion, press/Escape dismissal, keyboard focus,
// touch long-press, and disabled-trigger hover support.
func studioTooltip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	if text == "" || !anchor.Valid() {
		return ui.Element{}
	}
	anchor.Description(text)
	return studioTooltipVisual(c, anchor, text)
}

func studioTooltipVisual(c *ui.Context, anchor ui.Element, text string) ui.Element {
	if text == "" || !anchor.Valid() {
		return ui.Element{}
	}
	coord := syncHoverCoordinator(c)
	now := frameNow(c)
	id := anchor.ID()
	groupID := coord.currentGroup()

	disabled := anchor.IsDisabled()
	hovered := anchor.Hovered()
	if !hovered && disabled && coord.inWin && coord.suppressedCount == 0 && coord.prevSuppressedCount == 0 {
		hovered = pointInRect(coord.px, coord.py, anchor.Bounds())
	}
	focused := anchor.FocusVisible()

	if !disabled && anchor.LongPressed() {
		coord.touchID = id
	}
	if anchor.Pressed() || anchor.Clicked() || coord.isSuppressed(id) {
		coord.dismissedID = id
		if coord.touchID == id {
			coord.touchID = 0
		}
	}

	if hovered {
		coord.anyHoverInPass = true
		if coord.hoverID != id {
			coord.hoverID = id
			coord.hoverGroupID = groupID
			coord.hoverSince = now
		}
	}
	if focused {
		coord.anyFocusInPass = true
		if coord.focusID != id {
			coord.focusID = id
			coord.focusSince = now
		}
	}

	if coord.passShown || coord.dismissedID == id {
		return ui.Element{}
	}

	showByTouch := coord.touchID == id
	showByFocus := focused && (coord.lastPointerMoveAt.IsZero() || !coord.lastPointerMoveAt.After(coord.focusSince))
	showByHover := false
	if hovered && coord.hoverID == id {
		warm := coord.prevShown || (!coord.lastShownAt.IsZero() && now.Sub(coord.lastShownAt) <= labelWarmWindow)
		if warm {
			showByHover = true
		} else if wait := labelOpenDelay - now.Sub(coord.hoverSince); wait > 0 {
			c.After(wait)
		} else if !coord.isFast(c, now) {
			showByHover = true
		}
	}

	if !showByTouch && !showByFocus && !showByHover {
		return ui.Element{}
	}

	coord.passShown = true
	coord.passAnchorID = id
	coord.passGroupID = groupID
	coord.passRect = anchor.Bounds()
	coord.passText = text

	var tip ui.Element
	ui.Overlay(c, func() {
		tip = renderLabelTipOverlayNode(c, coord, anchor, id, text)
	})
	return tip
}

func renderLabelTipOverlayNode(c *ui.Context, coord *hoverCoordinator, target ui.Element, anchorID uint64, text string) ui.Element {
	t := tokens(c)
	theme := c.Theme()
	reduced := c.Preferences().ReduceMotion
	tr := ui.ElementTransition{
		Duration:     labelEnterDuration,
		ExitDuration: labelExitDuration,
		Position:     !reduced,
		Colors:       reduced,
		Enter:        &ui.Motion{Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
	tip := ui.Box(c.Key("studio-label-tip")).Absolute().PassThrough().Role(ui.RoleTooltip).
		MaxWidth(theme.Space(80)).
		Padding(theme.Space(1.25), theme.Space(2)).
		Radius(t.Radius.Control).
		Background(t.Color.FgPrimary).
		TextColor(t.Color.Surface).
		Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, .18)).
		AttachTo(target, ui.AnchorTop, ui.AnchorBottom).
		AttachEdgeMargin(t.Density.Row).
		Margin(0, 0, theme.Space(1.5), 0).
		Transition(tr)
	tip.Children(func() {
		ui.Text(c, text).FontSize(t.Type.Caption).TextColor(t.Color.Surface)
	})
	if tip.OverlayShortcut(0, ui.KeyEscape) {
		coord.dismissedID = anchorID
		coord.touchID = 0
	}
	return tip
}

// studioTooltipIfTruncated attaches a tooltip and keyboard focusability only when
// displayedText does not contain fullText or overflows el's laid-out width.
func studioTooltipIfTruncated(c *ui.Context, el ui.Element, displayedText, fullText string, fontSize float32, bold bool) {
	if fullText == "" || !el.Valid() {
		return
	}
	cutOff := !strings.Contains(displayedText, fullText)
	if !cutOff {
		availW := el.LayoutBounds().W
		if availW <= 0 {
			availW = el.Bounds().W
		}
		if availW > 0 {
			span := ui.Span{Text: displayedText, Size: fontSize}
			if bold {
				span.Weight = 700
			}
			natW, _ := c.MeasureText(0, span)
			cutOff = natW > availW+1
		}
	}
	if !cutOff {
		return
	}
	el.Focusable().Label(fullText)
	if el.LongPressed() {
		syncHoverCoordinator(c).touchID = el.ID()
	}
	studioTooltip(c, el, fullText)
}

// withDisabledTooltip wraps a potentially disabled control so both pointer hover (500ms)
// and keyboard Tab focus (0ms) reach its explanation tooltip without enabling clicks.
func withDisabledTooltip(c *ui.Context, tip string, render func() ui.Element) ui.Element {
	if tip == "" {
		return render()
	}
	var ctrl ui.Element
	wrap := ui.Row(c)
	wrap.Children(func() {
		ctrl = render()
	})
	if ctrl.Valid() && ctrl.IsDisabled() {
		ctrl.Description(tip)
		wrap.Role(ui.RoleNone).Focusable()
		studioTooltip(c, wrap, tip)
	} else {
		studioTooltip(c, ctrl, tip)
	}
	return ctrl
}

// pointInSafeTriangle reports whether pt lies inside the safe triangle formed by origin
// and two corners of a target panel's near edge, preventing diagonal crossings from swapping early.
func pointInSafeTriangle(px, py, ox, oy, ax, ay, bx, by float32) bool {
	sign := func(x1, y1, x2, y2, x3, y3 float32) float32 {
		return (x1-x3)*(y2-y3) - (x2-x3)*(y1-y3)
	}
	d1 := sign(px, py, ox, oy, ax, ay)
	d2 := sign(px, py, ax, ay, bx, by)
	d3 := sign(px, py, bx, by, ox, oy)
	hasNeg := (d1 < 0) || (d2 < 0) || (d3 < 0)
	hasPos := (d1 > 0) || (d2 > 0) || (d3 > 0)
	return !(hasNeg && hasPos)
}
