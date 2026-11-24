package desktop

import (
	"context"
	"encoding/json"
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

type Message struct{ Title, Body string }

func Subscribe(ctx context.Context, bus *core.EventBus, show func(Message) error, logf func(string, ...any)) {
	SubscribeWithFilter(ctx, bus, nil, show, logf)
}

func SubscribeWithFilter(ctx context.Context, bus *core.EventBus, suppress func(core.Event) bool, show func(Message) error, logf func(string, ...any)) {
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
					if suppress != nil && suppress(event) {
						continue
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
	switch event.Type {
	case "report.created":
		return reportCreatedMessage(event.Data), true
	case "run.state":
		return runStateMessage(event.Data)
	case "download.state":
		return downloadStateMessage(event.Data)
	case "engine.state":
		return engineStateMessage(event.Data)
	default:
		return Message{}, false
	}
}

func reportCreatedMessage(data json.RawMessage) Message {
	var payload struct {
		Report struct {
			ReportId string `json:"report_id"`
		} `json:"report"`
	}
	if json.Unmarshal(data, &payload) == nil && strings.TrimSpace(payload.Report.ReportId) != "" {
		return Message{
			Title: l10n.TR("desktop.notif.report_created.title"),
			Body:  l10n.TRf("desktop.notif.report_created.body_id", "id", payload.Report.ReportId),
		}
	}
	return Message{
		Title: l10n.TR("desktop.notif.report_created.title"),
		Body:  l10n.TR("desktop.notif.report_created.body"),
	}
}

func runStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Task struct {
			Kind   string  `json:"kind"`
			State  string  `json:"state"`
			Bundle *string `json:"bundle"`
		} `json:"task"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	name := l10n.TR("desktop.notif.run.name_default")
	if payload.Task.Kind == "context_speed" {
		name = l10n.TR("desktop.notif.run.name_context_speed")
	} else if payload.Task.Bundle != nil && strings.TrimSpace(*payload.Task.Bundle) != "" {
		name = l10n.TRf("desktop.notif.run.name_bundle", "bundle", *payload.Task.Bundle)
	}
	switch payload.Task.State {
	case "done":
		return Message{
			Title: l10n.TRf("desktop.notif.run.done.title", "name", name),
			Body:  l10n.TR("desktop.notif.run.done.body"),
		}, true
	case "failed":
		return Message{
			Title: l10n.TRf("desktop.notif.run.failed.title", "name", name),
			Body:  l10n.TR("desktop.notif.run.failed.body"),
		}, true
	case "cancelled":
		return Message{
			Title: l10n.TRf("desktop.notif.run.cancelled.title", "name", name),
			Body:  l10n.TR("desktop.notif.run.cancelled.body"),
		}, true
	default:
		return Message{}, false
	}
}

func downloadStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Task struct {
			Kind        string `json:"kind"`
			State       string `json:"state"`
			DisplayName string `json:"display_name"`
			OutName     string `json:"out_name"`
		} `json:"task"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	if payload.Task.Kind == "polystrata" {
		label := strings.TrimSpace(payload.Task.DisplayName)
		if label == "" {
			label = "PolyStrata"
		}
		switch payload.Task.State {
		case "done":
			return Message{
				Title: l10n.TR("desktop.notif.download.polystrata.done.title"),
				Body:  l10n.TRf("desktop.notif.download.polystrata.done.body", "label", label),
			}, true
		case "failed":
			return Message{
				Title: l10n.TR("desktop.notif.download.polystrata.failed.title"),
				Body:  l10n.TRf("desktop.notif.download.polystrata.failed.body", "label", label),
			}, true
		case "cancelled":
			return Message{
				Title: l10n.TR("desktop.notif.download.polystrata.cancelled.title"),
				Body:  l10n.TRf("desktop.notif.download.polystrata.cancelled.body", "label", label),
			}, true
		default:
			return Message{}, false
		}
	}
	kind := l10n.TR("desktop.notif.download.kind_model")
	if payload.Task.Kind == "engine" {
		kind = l10n.TR("desktop.notif.download.kind_engine")
	}
	target := kind
	if strings.TrimSpace(payload.Task.OutName) != "" {
		target = l10n.TRf("desktop.notif.download.target", "kind", kind, "name", payload.Task.OutName)
	}
	switch payload.Task.State {
	case "done":
		return Message{
			Title: l10n.TRf("desktop.notif.download.done.title", "kind", kind),
			Body:  l10n.TRf("desktop.notif.download.done.body", "target", target),
		}, true
	case "failed":
		return Message{
			Title: l10n.TRf("desktop.notif.download.failed.title", "kind", kind),
			Body:  l10n.TRf("desktop.notif.download.failed.body", "target", target),
		}, true
	case "cancelled":
		return Message{
			Title: l10n.TRf("desktop.notif.download.cancelled.title", "kind", kind),
			Body:  l10n.TRf("desktop.notif.download.cancelled.body", "target", target),
		}, true
	default:
		return Message{}, false
	}
}

func engineStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Engine struct {
			State string `json:"state"`
		} `json:"engine"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	switch payload.Engine.State {
	case "failed":
		return Message{
			Title: l10n.TR("desktop.notif.engine.failed.title"),
			Body:  l10n.TR("desktop.notif.engine.failed.body"),
		}, true
	case "orphaned":
		return Message{
			Title: l10n.TR("desktop.notif.engine.orphaned.title"),
			Body:  l10n.TR("desktop.notif.engine.orphaned.body"),
		}, true
	default:
		return Message{}, false
	}
}
