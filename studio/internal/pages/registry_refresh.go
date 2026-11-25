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
		if event.Type != "update.state" && event.Type != "cli.upgrade" && event.Type != "services.memory" && event.Type != "engine.updates" {
			return true
		}
	}
	return false
}
