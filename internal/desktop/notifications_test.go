package desktop

import (
	"context"
	"errors"
	"fmt"
	"inferyard-studio/internal/core"
	"strings"
	"testing"
	"time"
)

func TestEventNotifications(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := core.NewEventBus()
	messages := make(chan Message, 10)
	logs := make(chan string, 10)
	Subscribe(ctx, bus, false, func(m Message) error { messages <- m; return errors.New("notification permission denied") }, func(f string, a ...any) { logs <- fmt.Sprintf(f, a...) })
	for _, kind := range []string{"run.state", "download.state"} {
		for _, state := range []string{"done", "failed", "cancelled"} {
			bus.Publish(kind, map[string]any{"task": map[string]string{"state": state, "out_dir": "/private/secret"}})
			select {
			case m := <-messages:
				if !strings.Contains(m.Title, state) || strings.Contains(m.Body, "/private") {
					t.Fatalf("unexpected message: %+v", m)
				}
			case <-time.After(time.Second):
				t.Fatal("missing terminal notification")
			}
			select {
			case entry := <-logs:
				if strings.Contains(entry, "/private") {
					t.Fatal(entry)
				}
			case <-time.After(time.Second):
				t.Fatal("missing failure log")
			}
		}
	}
	bus.Publish("report.created", map[string]any{"report": map[string]string{"path": "/private/secret"}})
	select {
	case m := <-messages:
		if m.Title != "Report ready" {
			t.Fatal(m)
		}
	case <-time.After(time.Second):
		t.Fatal("missing report notification")
	}
	for _, kind := range []string{"run.state", "download.state", "run.progress", "device.updated"} {
		bus.Publish(kind, map[string]any{"task": map[string]string{"state": "running"}})
	}
	select {
	case m := <-messages:
		t.Fatalf("unexpected notification: %+v", m)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
}

func TestHeadlessNotifications(t *testing.T) {
	bus := core.NewEventBus()
	Subscribe(context.Background(), bus, true, func(Message) error { t.Error("headless delivery"); return nil }, t.Logf)
	bus.Publish("report.created", map[string]any{})
}
