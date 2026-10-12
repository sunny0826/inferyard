// Package desktop adapts application events to best-effort desktop notifications.
package desktop

import (
	"context"
	"encoding/json"
	"inferyard-studio/internal/core"
)

type Message struct{ Title, Body string }

// Subscribe registers synchronously so events published after it returns are
// observed. Delivery runs separately from publishers, including permission UI.
// Headless callers create neither a subscription nor a delivery goroutine.
func Subscribe(ctx context.Context, bus *core.EventBus, headless bool, show func(Message) error, logf func(string, ...any)) {
	if headless {
		return
	}
	wake, unsubscribe, cursor := bus.Subscribe()
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				events, gap, current := bus.After(cursor)
				cursor = current
				if gap {
					logf("desktop notification events exceeded replay buffer")
					continue
				}
				for _, event := range events {
					if ctx.Err() != nil {
						return
					}
					if message, ok := messageFor(event); ok {
						if err := show(message); err != nil {
							logf("desktop notification delivery failed: %v", err)
						}
					}
				}
			}
		}
	}()
}

func messageFor(event core.Event) (Message, bool) {
	if event.Type == "report.created" {
		return Message{"Report ready", "Your report has been generated."}, true
	}
	var title string
	switch event.Type {
	case "run.state":
		title = "Run"
	case "download.state":
		title = "Download"
	default:
		return Message{}, false
	}
	var payload struct {
		Task struct {
			State string `json:"state"`
		} `json:"task"`
	}
	if json.Unmarshal(event.Data, &payload) != nil {
		return Message{}, false
	}
	var body string
	switch payload.Task.State {
	case "done":
		body = "completed."
	case "failed":
		body = "failed. Open Studio for details."
	case "cancelled":
		body = "was cancelled."
	default:
		return Message{}, false
	}
	return Message{title + " " + payload.Task.State, "Your " + map[string]string{"Run": "run", "Download": "download"}[title] + " " + body}, true
}
