package pages

import "inferyard/studio/internal/core"

func fullRefresh(events []core.Event, reset bool) bool {
	if reset {
		return true
	}
	for _, event := range events {
		switch event.Type {
		case "sync.reset", "report.created", "run.state", "engine.state", "services.state", "service.exited", "service.exit_notice_cleared", "download.state", "device.updated", "assets.updated":
			return true
		}
	}
	return false
}

func needsRefresh(events []core.Event, reset bool) bool {
	if reset {
		return true
	}
	for _, event := range events {
		switch event.Type {
		case "update.state", "cli.upgrade", "services.memory", "engine.updates", "run.progress", "run.resources":
			continue
		default:
			return true
		}
	}
	return false
}
