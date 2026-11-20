//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestDeviceEmptyStateCollectionRespectsBusy(t *testing.T) {
	p := newPreviewSession(context.Background())
	s := p.state
	s.selected = "device"
	s.device.Snapshot, s.device.Refreshing, s.device.LastError, s.busy = nil, false, nil, true
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(p.theme(c))
		ui.Column(c).Children(func() { s.deviceView(c) })
	}, 1120, 1200)
	tt.Frame()
	if !tt.HasText("采集设备信息") {
		t.Fatalf("empty-state collection button missing: %v", tt.Texts())
	}
	if err := tt.Click("采集设备信息"); err != nil {
		t.Fatalf("collection button missing: %v", err)
	}
	if s.device.Refreshing {
		t.Fatal("busy page started a device collection")
	}

	s.busy = false
	tt.Frame()
	click(t, tt, "采集设备信息")
	if !s.device.Refreshing {
		t.Fatalf("idle page did not start collection: %v", tt.Texts())
	}
	p.advance(600 * time.Millisecond)
	tt.Frame()
	if s.device.Refreshing {
		t.Fatal("simulated collection never finished")
	}
	if s.device.Snapshot == nil {
		t.Fatal("collection finished without a snapshot")
	}
}
