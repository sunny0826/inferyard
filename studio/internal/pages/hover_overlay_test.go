package pages

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
)

func hasOpenTooltip(tt *ui.Tester, text string) bool {
	for _, info := range tt.AccessAll() {
		if info.Role == ui.RoleTooltip && (text == "" || info.Name == text || strings.Contains(info.Name, text)) {
			return true
		}
	}
	return false
}

func TestToolbarTooltipSweepGroupHandoffWarmAndPressDismiss(t *testing.T) {
	clickedA := 0
	view := func(c *ui.Context) {
		ui.Column(c).Padding(24).Children(func() {
			studioTooltipGroup(c, ui.Row(c).AlignItems(ui.Center).Gap(8), func() {
				if outlineIconButton(c, reicon.Copy, "Action Alpha").Clicked() {
					clickedA++
				}
				outlineIconButton(c, reicon.Code, "Action Beta")
				outlineIconButton(c, reicon.Play, "Action Gamma")
			})
		})
	}

	tt := ui.NewTester(view, 600, 240)
	tt.Frame()

	btnA, okA := tt.AccessByRole(ui.RoleButton, "Action Alpha")
	btnB, okB := tt.AccessByRole(ui.RoleButton, "Action Beta")
	btnC, okC := tt.AccessByRole(ui.RoleButton, "Action Gamma")
	if !okA || !okB || !okC {
		t.Fatalf("missing toolbar buttons: A=%v B=%v C=%v", okA, okB, okC)
	}
	cy := btnA.Bounds.Y + btnA.Bounds.H/2

	// 1. Fast sweep across the toolbar (> 0.1 px/ms) opens zero labels.
	tt.Move(10, cy)
	tt.Frame()
	for x := float32(25); x <= btnC.Bounds.X+btnC.Bounds.W+20; x += 30 {
		tt.Advance(16 * time.Millisecond)
		tt.Move(x, cy)
		tt.Frame()
		if hasOpenTooltip(tt, "") {
			t.Fatalf("fast sweep at x=%.0f unexpectedly opened a tooltip: %+v", x, tt.AccessAll())
		}
	}

	// Move away and let velocity settle.
	tt.Move(4, 4)
	tt.Advance(500 * time.Millisecond)

	// 2. Resting < 500ms keeps label closed; reaching 500ms opens it.
	ax := btnA.Bounds.X + btnA.Bounds.W/2
	tt.Move(ax, cy)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("tooltip opened before 500ms cold hover delay")
	}
	tt.Advance(260 * time.Millisecond)
	if !hasOpenTooltip(tt, "Action Alpha") {
		t.Fatalf("expected Action Alpha tooltip after 500ms rest, access=%+v", tt.AccessAll())
	}

	// 3. Moving across the 8px inter-button gap keeps the active label mounted without flicker,
	// and entering Button B swaps text in place at 0ms.
	gapX := btnA.Bounds.X + btnA.Bounds.W + 4
	tt.Move(gapX, cy)
	tt.Frame()
	if !hasOpenTooltip(tt, "Action Alpha") {
		t.Fatalf("tooltip disappeared inside 8px toolbar gap, access=%+v", tt.AccessAll())
	}

	bx := btnB.Bounds.X + btnB.Bounds.W/2
	tt.Move(bx, cy)
	tt.Frame()
	if !hasOpenTooltip(tt, "Action Beta") {
		t.Fatalf("expected 0ms in-place swap to Action Beta, access=%+v", tt.AccessAll())
	}

	// 4. Leaving the group hides the label; re-entering within 400ms opens at 0ms.
	tt.Move(4, 4)
	tt.Advance(80 * time.Millisecond)
	if hasOpenTooltip(tt, "") {
		t.Fatalf("expected tooltip hidden after leaving toolbar group, access=%+v", tt.AccessAll())
	}

	cx := btnC.Bounds.X + btnC.Bounds.W/2
	tt.Move(cx, cy)
	tt.Frame()
	if !hasOpenTooltip(tt, "Action Gamma") {
		t.Fatalf("expected 0ms warm re-entry within 400ms for Action Gamma, access=%+v", tt.AccessAll())
	}

	// 5. Leaving > 400ms restores the 500ms rest delay.
	tt.Move(4, 4)
	tt.Advance(460 * time.Millisecond)
	tt.Move(ax, cy)
	tt.Frame()
	tt.Advance(250 * time.Millisecond)
	if hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("expected cold 500ms wait after >400ms outside group")
	}
	tt.Advance(310 * time.Millisecond)
	if !hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("expected Action Alpha to open after full 500ms rest")
	}

	// 6. Pressing a button dismisses its tooltip and keeps it suppressed until pointer leaves it.
	tt.ClickAt(ax, cy)
	if clickedA != 1 {
		t.Fatalf("expected button A click count = 1, got %d", clickedA)
	}
	tt.Advance(80 * time.Millisecond)
	if hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("expected tooltip dismissed after pressing button A")
	}
	tt.Advance(600 * time.Millisecond)
	if hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("expected tooltip to stay hidden while pointer remains on pressed button A")
	}
	tt.Move(4, 4)
	tt.Advance(460 * time.Millisecond)
	tt.Move(ax, cy)
	tt.Advance(560 * time.Millisecond)
	if !hasOpenTooltip(tt, "Action Alpha") {
		t.Fatal("expected tooltip to re-open after leaving and resting on button A again")
	}
}

func TestDisabledControlsExposeReasonTooltipOnHoverAndKeyboardFocus(t *testing.T) {
	t.Run("WrappedDisabledButtons", func(t *testing.T) {
		clicked := 0
		view := func(c *ui.Context) {
			ui.Row(c).Padding(20).Gap(12).AlignItems(ui.Center).Children(func() {
				iconButtonWithTooltip(c, "Blocked: round active", func() ui.Element {
					b := outlineIconButton(c, reicon.Play, "").Label("Run task").Disabled(true)
					if b.Clicked() {
						clicked++
					}
					return b
				})
				engineHeaderActionButton(c, engineHeaderActionOptions{
					statusText:  "Running",
					actionLabel: "Stop Engine",
					kind:        stateSuccess,
					action:      "stop",
					disabled:    true,
					tooltip:     "Stop disabled while benchmark runs",
				})
			})
		}

		// Hover (500ms) opens disabled reason tooltip without firing click.
		ttHover := ui.NewTester(view, 520, 200)
		ttHover.Frame()
		runBtn, ok := ttHover.AccessByRole(ui.RoleButton, "Run task")
		if !ok {
			t.Fatal("missing disabled Run task button")
		}
		ttHover.Move(runBtn.Bounds.X+runBtn.Bounds.W/2, runBtn.Bounds.Y+runBtn.Bounds.H/2)
		ttHover.Advance(250 * time.Millisecond)
		if hasOpenTooltip(ttHover, "Blocked: round active") {
			t.Fatal("disabled tooltip opened before 500ms hover")
		}
		ttHover.Advance(310 * time.Millisecond)
		if !hasOpenTooltip(ttHover, "Blocked: round active") {
			t.Fatalf("expected disabled reason tooltip after 500ms hover, access=%+v", ttHover.AccessAll())
		}
		ttHover.ClickAt(runBtn.Bounds.X+runBtn.Bounds.W/2, runBtn.Bounds.Y+runBtn.Bounds.H/2)
		if clicked != 0 {
			t.Fatalf("disabled button must not fire click, got %d", clicked)
		}

		stopPill, ok := ttHover.AccessByRole(ui.RoleButton, "Stop Engine")
		if !ok {
			t.Fatal("missing disabled Stop Engine pill")
		}
		ttHover.Move(4, 4)
		ttHover.Advance(460 * time.Millisecond)
		ttHover.Move(stopPill.Bounds.X+stopPill.Bounds.W/2, stopPill.Bounds.Y+stopPill.Bounds.H/2)
		ttHover.Advance(560 * time.Millisecond)
		if !hasOpenTooltip(ttHover, "Stop disabled while benchmark runs") {
			t.Fatalf("expected disabled pill tooltip on 500ms hover, access=%+v", ttHover.AccessAll())
		}

		// Keyboard Tab focus opens disabled reason tooltips at 0ms.
		ttKey := ui.NewTester(view, 520, 200)
		ttKey.Frame()
		ttKey.Key(0, ui.KeyTab)
		if !hasOpenTooltip(ttKey, "Blocked: round active") {
			t.Fatalf("expected 0ms tooltip on first Tab stop, access=%+v", ttKey.AccessAll())
		}
		ttKey.Key(0, ui.KeyTab)
		if !hasOpenTooltip(ttKey, "Stop disabled while benchmark runs") {
			t.Fatalf("expected 0ms tooltip on second Tab stop, access=%+v", ttKey.AccessAll())
		}
	})

	t.Run("ServicesDisabledStartModelButton", func(t *testing.T) {
		s := readyState("services")
		s.serveSupported = false
		s.preview = nil
		wantReason := tr(nil, "models.serve.blocked.cli_outdated")
		btnLabel := tr(nil, "services.page.action.start_model")

		tt := ui.NewTester(s.servicesView, 1000, 720)
		tt.Frame()
		btn, ok := tt.AccessByRole(ui.RoleButton, btnLabel)
		if !ok {
			t.Fatalf("missing %q button in servicesView", btnLabel)
		}
		tt.Move(btn.Bounds.X+btn.Bounds.W/2, btn.Bounds.Y+btn.Bounds.H/2)
		tt.Advance(560 * time.Millisecond)
		if !hasOpenTooltip(tt, wantReason) {
			t.Fatalf("expected %q on 500ms hover over disabled startModelBtn, access=%+v", wantReason, tt.AccessAll())
		}

		ttKey := ui.NewTester(s.servicesView, 1000, 720)
		ttKey.Frame()
		ttKey.Key(0, ui.KeyTab)
		if !hasOpenTooltip(ttKey, wantReason) {
			t.Fatalf("expected %q at 0ms on Tab focus of disabled startModelBtn, access=%+v", wantReason, ttKey.AccessAll())
		}
	})
}

func TestTruncatedTextTooltipGating(t *testing.T) {
	shortPath := "/opt/m"
	longPath := "/Users/developer/Library/Application Support/InferYard/models/huggingface/Qwen2.5-72B-Instruct-GGUF/qwen2.5-72b-instruct-q4_k_m-00001-of-00012.gguf"
	shortContainedFull := "/tmp/runs"
	shortContainedDisp := "Results: /tmp/runs"
	basenameFull := "/var/lib/inferyard/results/run-20261020-001"
	basenameDisp := "Run dir: run-20261020-001"

	view := func(c *ui.Context) {
		t := tokens(c)
		ui.Column(c).Width(340).Padding(16).Gap(12).Children(func() {
			pathKeyValueRow(c, "ShortKey", shortPath)
			pathKeyValueRow(c, "LongKey", longPath)

			el1 := ui.Text(c, shortContainedDisp).FontSize(t.Type.Caption).MaxWidth(300).SingleLine().TruncateMiddle()
			studioTooltipIfTruncated(c, el1, shortContainedDisp, shortContainedFull, t.Type.Caption, false)

			el2 := ui.Text(c, basenameDisp).FontSize(t.Type.Caption).MaxWidth(300).SingleLine().TruncateMiddle()
			studioTooltipIfTruncated(c, el2, basenameDisp, basenameFull, t.Type.Caption, false)
		})
	}

	tt := ui.NewTester(view, 400, 320)
	tt.Frame()
	tt.Frame()

	// 1. Short fitting path does NOT attach focusable tooltip or open on hover.
	shortRect, ok := tt.Find(shortPath)
	if !ok {
		t.Fatalf("short path %q not found", shortPath)
	}
	tt.Move(shortRect.X+10, shortRect.Y+shortRect.H/2)
	tt.Advance(620 * time.Millisecond)
	if hasOpenTooltip(tt, "") {
		t.Fatalf("short fitting path must not open a tooltip, got %+v", tt.AccessAll())
	}

	// 2. Prefix-wrapped text that already contains fullText without overflowing shows no tooltip.
	r1, ok := tt.Find(shortContainedDisp)
	if !ok {
		t.Fatalf("contained display text %q not found", shortContainedDisp)
	}
	tt.Move(4, 4)
	tt.Advance(460 * time.Millisecond)
	tt.Move(r1.X+10, r1.Y+r1.H/2)
	tt.Advance(620 * time.Millisecond)
	if hasOpenTooltip(tt, "") {
		t.Fatalf("non-overflowing contained path must not open tooltip, got %+v", tt.AccessAll())
	}

	// 3. Overflowing path and basename-shortened path open full tooltip on 500ms hover and 0ms Tab focus.
	ttKey := ui.NewTester(view, 400, 320)
	ttKey.Frame()
	ttKey.Frame()
	ttKey.Key(0, ui.KeyTab)
	if !hasOpenTooltip(ttKey, longPath) {
		t.Fatalf("expected overflowing path focused first on Tab with full tooltip, access=%+v", ttKey.AccessAll())
	}
	ttKey.Key(0, ui.KeyTab)
	if !hasOpenTooltip(ttKey, basenameFull) {
		t.Fatalf("expected basename-shortened path focused second on Tab with full tooltip %q, access=%+v", basenameFull, ttKey.AccessAll())
	}

	r2, ok := tt.Find(basenameFull)
	if !ok {
		t.Fatalf("expected accessible label %q on shortened path", basenameFull)
	}
	tt.Move(4, 4)
	tt.Advance(460 * time.Millisecond)
	tt.Move(r2.X+10, r2.Y+r2.H/2)
	tt.Advance(560 * time.Millisecond)
	if !hasOpenTooltip(tt, basenameFull) {
		t.Fatalf("expected full path tooltip %q after 500ms hover, access=%+v", basenameFull, tt.AccessAll())
	}
}

func TestMorphingStatusPillsZeroWidthShiftAndKeyboardFocus(t *testing.T) {
	for _, locale := range []string{l10n.LanguageZhHans, l10n.LanguageEn} {
		t.Run(locale, func(t *testing.T) {
			restore := l10n.SetTestLocale(locale)
			defer restore()

			view := func(c *ui.Context) {
				ui.Row(c).Padding(24).Gap(16).AlignItems(ui.Center).Children(func() {
					engineHeaderActionButton(c, engineHeaderActionOptions{
						statusText:  stateLabelFor(locale, "running"),
						actionLabel: tr(c, "engine.action.stop"),
						kind:        stateSuccess,
						action:      "stop",
						tooltip:     tr(c, "engine.tooltip.click_stop"),
					})
					polystrataInstallStatusButton(c, false, false)
				})
			}

			tt := ui.NewTester(view, 640, 200)
			tt.SetLocale(locale)
			tt.Frame()

			stopLabel := l10n.T(locale, "engine.action.stop")
			cancelLabel := l10n.T(locale, "downloads.polystrata.cancel_install")
			stopIdle, ok1 := tt.AccessByRole(ui.RoleButton, stopLabel)
			polyIdle, ok2 := tt.AccessByRole(ui.RoleButton, cancelLabel)
			if !ok1 || !ok2 {
				t.Fatalf("missing morphing pills: stop=%v poly=%v", ok1, ok2)
			}

			// Pointer hover morphs text with 0px width shift.
			tt.Move(stopIdle.Bounds.X+stopIdle.Bounds.W/2, stopIdle.Bounds.Y+stopIdle.Bounds.H/2)
			tt.Frame()
			stopHover, _ := tt.AccessByRole(ui.RoleButton, stopLabel)
			if !tt.HasText(l10n.T(locale, "engine.action.stop_short")) {
				t.Fatalf("expected hovered stop pill to morph label, texts=%v", tt.Texts())
			}
			if math.Abs(float64(stopHover.Bounds.W-stopIdle.Bounds.W)) > 0.01 {
				t.Fatalf("engineHeaderActionButton width shifted on hover: idle=%.2f hover=%.2f", stopIdle.Bounds.W, stopHover.Bounds.W)
			}

			tt.Move(polyIdle.Bounds.X+polyIdle.Bounds.W/2, polyIdle.Bounds.Y+polyIdle.Bounds.H/2)
			tt.Frame()
			polyHover, _ := tt.AccessByRole(ui.RoleButton, cancelLabel)
			if math.Abs(float64(polyHover.Bounds.W-polyIdle.Bounds.W)) > 0.01 {
				t.Fatalf("polystrataInstallStatusButton width shifted on hover: idle=%.2f hover=%.2f", polyIdle.Bounds.W, polyHover.Bounds.W)
			}

			// Keyboard focus morphs text at 0ms with 0px width shift.
			ttKey := ui.NewTester(view, 640, 200)
			ttKey.SetLocale(locale)
			ttKey.Frame()
			ttKey.Key(0, ui.KeyTab)
			stopFocus, _ := ttKey.AccessByRole(ui.RoleButton, stopLabel)
			if !ttKey.HasText(l10n.T(locale, "engine.action.stop_short")) {
				t.Fatalf("expected Tab focus on stop pill to morph label, texts=%v", ttKey.Texts())
			}
			if math.Abs(float64(stopFocus.Bounds.W-stopIdle.Bounds.W)) > 0.01 {
				t.Fatalf("engineHeaderActionButton width shifted on focus: idle=%.2f focus=%.2f", stopIdle.Bounds.W, stopFocus.Bounds.W)
			}

			ttKey.Key(0, ui.KeyTab)
			polyFocus, _ := ttKey.AccessByRole(ui.RoleButton, cancelLabel)
			if math.Abs(float64(polyFocus.Bounds.W-polyIdle.Bounds.W)) > 0.01 {
				t.Fatalf("polystrataInstallStatusButton width shifted on focus: idle=%.2f focus=%.2f", polyIdle.Bounds.W, polyFocus.Bounds.W)
			}
		})
	}
}
