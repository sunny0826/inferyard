package desktop

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"inferyard/studio/internal/l10n"
)

type trayIconSet struct {
	mu    sync.Mutex
	base  []byte
	cache map[TrayEngineState][]byte
}

func newTrayIconSet(base []byte) *trayIconSet {
	return &trayIconSet{base: base, cache: make(map[TrayEngineState][]byte)}
}

func (s *trayIconSet) forState(state TrayEngineState) []byte {
	if state == "" {
		state = TrayEngineIdle
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if icon, ok := s.cache[state]; ok {
		return icon
	}
	icon := s.base
	if state != TrayEngineIdle && len(s.base) > 0 {
		if decoded, err := png.Decode(bytes.NewReader(s.base)); err == nil {
			bounds := decoded.Bounds()
			rgba := image.NewRGBA(bounds)
			draw.Draw(rgba, bounds, decoded, bounds.Min, draw.Src)
			drawTrayBadge(rgba, trayBadgeColor(state))
			var encoded bytes.Buffer
			if png.Encode(&encoded, rgba) == nil {
				icon = encoded.Bytes()
			}
		}
	}
	s.cache[state] = icon
	return icon
}

func trayBadgeColor(state TrayEngineState) color.RGBA {
	switch state {
	case TrayEngineRunning:
		return color.RGBA{R: 34, G: 197, B: 94, A: 255}
	case TrayEngineStarting, TrayEngineStopping:
		return color.RGBA{R: 245, G: 158, B: 11, A: 255}
	case TrayEngineFailed, TrayEngineOrphaned:
		return color.RGBA{R: 239, G: 68, B: 68, A: 255}
	default:
		return color.RGBA{R: 59, G: 130, B: 246, A: 255}
	}
}

func drawTrayBadge(icon *image.RGBA, fill color.RGBA) {
	bounds := icon.Bounds()
	size := min(bounds.Dx(), bounds.Dy())
	if size < 4 {
		return
	}
	radius := float64(size) * 0.28
	cx := float64(bounds.Max.X-1) - radius
	cy := float64(bounds.Max.Y-1) - radius
	drawTrayCircle(icon, cx, cy, radius, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	drawTrayCircle(icon, cx, cy, radius*0.72, fill)
}

func drawTrayCircle(icon *image.RGBA, cx, cy, radius float64, fill color.RGBA) {
	bounds := image.Rect(int(cx-radius)-1, int(cy-radius)-1, int(cx+radius)+2, int(cy+radius)+2).Intersect(icon.Bounds())
	if bounds.Empty() {
		return
	}
	mask := image.NewAlpha(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			covered := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px := float64(x) + (float64(sx)+0.5)/4
					py := float64(y) + (float64(sy)+0.5)/4
					dx, dy := px-cx, py-cy
					if dx*dx+dy*dy <= radius*radius {
						covered++
					}
				}
			}
			mask.SetAlpha(x, y, color.Alpha{A: uint8(covered * 255 / 16)})
		}
	}
	draw.DrawMask(icon, bounds, image.NewUniform(fill), image.Point{}, mask, bounds.Min, draw.Over)
}

func buildTrayMenu(status TrayEngineStatus, onShow, onQuit func()) *mygo.Menu {
	items := []*mygo.MenuItem{{
		Label:    l10n.TRf("tray.status.summary", "state", trayStateLabel(status.State), "count", status.ActiveCount),
		Disabled: true,
	}}
	for _, service := range status.Services {
		items = append(items, &mygo.MenuItem{Label: trayServiceLabel(service), Disabled: true})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: l10n.TR("tray.show_window"), Click: func(*mygo.MenuItem, *mygo.Window) { onShow() }},
		mygo.Separator(),
		&mygo.MenuItem{Label: l10n.TR("tray.quit"), Click: func(*mygo.MenuItem, *mygo.Window) { onQuit() }},
	)
	return mygo.NewMenu(items)
}

func trayServiceLabel(service TrayServiceSummary) string {
	model := service.ModelName
	if model == "" {
		model = l10n.TR("app.name")
	}
	details := make([]string, 0, 4)
	for _, value := range []string{service.EngineName, trayServicePort(service), trayStateLabel(service.State)} {
		if value != "" {
			details = append(details, value)
		}
	}
	if service.LANAccess {
		details = append(details, l10n.TR("services.card.badge.lan"))
	}
	if len(details) == 0 {
		return "• " + model
	}
	return fmt.Sprintf("• %s (%s)", model, strings.Join(details, " · "))
}

func trayServicePort(service TrayServiceSummary) string {
	if service.Port > 0 {
		return fmt.Sprintf(":%d", service.Port)
	}
	return ""
}

func trayStateLabel(state TrayEngineState) string {
	switch state {
	case TrayEngineStarting:
		return l10n.TR("common.state.starting")
	case TrayEngineRunning:
		return l10n.TR("common.state.running")
	case TrayEngineStopping:
		return l10n.TR("common.state.stopping")
	case TrayEngineFailed:
		return l10n.TR("common.state.failed")
	case TrayEngineOrphaned:
		return l10n.TR("common.state.orphaned")
	default:
		return l10n.TR("tray.state.idle")
	}
}

func trayTitle(status TrayEngineStatus) string {
	if status.State == "" || status.State == TrayEngineIdle {
		return ""
	}
	if status.ActiveCount > 0 {
		return l10n.TRf("tray.title.active", "count", status.ActiveCount)
	}
	return trayStateLabel(status.State)
}

func trayToolTip(status TrayEngineStatus) string {
	app := l10n.TR("app.name")
	if status.State == "" || status.State == TrayEngineIdle {
		return l10n.TRf("tray.tooltip.idle", "app", app)
	}
	if len(status.Services) == 1 {
		service := status.Services[0]
		if trayServicePort(service) == "" {
			return l10n.TRf("tray.tooltip.single_no_port", "app", app, "state", trayStateLabel(service.State), "model", service.ModelName)
		}
		return l10n.TRf("tray.tooltip.single", "app", app, "state", trayStateLabel(service.State), "model", service.ModelName, "port", trayServicePort(service))
	}
	return l10n.TRf("tray.tooltip.multiple", "app", app, "state", trayStateLabel(status.State), "count", status.ActiveCount)
}
