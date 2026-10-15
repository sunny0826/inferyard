package pages

import (
	"inferyard/studio/internal/core"
	"testing"
	"time"
)

func TestRegistryRefreshEventScope(t *testing.T) {
	bus := core.NewEventBus()
	_, off, cursor := bus.Subscribe()
	defer off()
	for range 5 {
		_ = bus.Publish("download.progress", map[string]any{"downloaded_bytes": 42})
	}
	events, reset, next := bus.After(cursor)
	if fullRefresh(events, reset) {
		t.Fatal("download progress triggered an asset scan")
	}
	_ = bus.Publish("download.state", map[string]any{"task": core.DownloadTask{Kind: "engine", State: "done"}})
	events, reset, _ = bus.After(next)
	if !fullRefresh(events, reset) {
		t.Fatal("completed engine not refreshed immediately")
	}
	for _, kind := range []string{"engine.state", "device.updated", "sync.reset"} {
		if !fullRefresh([]core.Event{{Type: kind}}, false) {
			t.Fatal("missing refresh", kind)
		}
	}
	if !fullRefresh(nil, true) {
		t.Fatal("lost events did not rebuild registry")
	}
	if fullRefresh([]core.Event{{Type: "run.progress"}}, false) {
		t.Fatal("run progress triggered an asset scan")
	}
}

func TestEngineVerificationRefreshThrottle(t *testing.T) {
	s := viewState{}
	now := time.Now()
	if !s.shouldVerifyEngines(false, true, now) {
		t.Fatal("initial visible scan skipped")
	}
	for i := 1; i < 5; i++ {
		if s.shouldVerifyEngines(fullRefresh([]core.Event{{Type: "download.progress"}}, false), true, now.Add(time.Duration(i)*time.Second)) {
			t.Fatal("download progress rebuilt verification targets")
		}
	}
	if !s.shouldVerifyEngines(false, true, now.Add(5*time.Second)) {
		t.Fatal("TTL did not refresh targets")
	}
	for _, kind := range []string{"download.state", "sync.reset", "engine.state"} {
		if !s.shouldVerifyEngines(fullRefresh([]core.Event{{Type: kind}}, false), true, now.Add(6*time.Second)) {
			t.Fatal("event did not refresh immediately", kind)
		}
	}
	if s.shouldVerifyEngines(true, false, now.Add(7*time.Second)) {
		t.Fatal("hidden page started verification")
	}
}
