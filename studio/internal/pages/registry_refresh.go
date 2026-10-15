package pages

import "inferyard/studio/internal/core"

// Progress-only events retain the asset list. Completed downloads and device
// snapshots must become visible on the next event refresh, without a TTL delay.
func fullRefresh(events []core.Event, reset bool) bool {
	if reset {
		return true
	}
	for _, event := range events {
		switch event.Type {
		case "sync.reset", "report.created", "run.state", "engine.state", "download.state", "device.updated":
			return true
		}
	}
	return false
}
