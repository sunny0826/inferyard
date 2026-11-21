package pages

import "inferyard/studio/internal/core"

func fullRefresh(events []core.Event, reset bool) bool {
	if reset {
		return true
	}
	for _, event := range events {
		switch event.Type {
		case "sync.reset", "report.created", "run.state", "engine.state", "download.state", "device.updated", "assets.updated":
			return true
		}
	}
	return false
}
