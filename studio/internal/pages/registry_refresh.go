package pages

import (
	"encoding/json"

	"inferyard/studio/internal/core"
)

type refreshMask uint16

const (
	refreshNone      refreshMask = 0
	refreshCore      refreshMask = 1 << iota
	refreshProgress              // ProgressCached + dashboard candidate state
	refreshHistory               // History + ContextSpeedHistory
	refreshReports               // Reports + ReportExclusivities
	refreshRegistry              // Registry + ModelBenchmarkEvidence
	refreshEngineVfy             // VerifyEngines when visible

	refreshAll = refreshCore | refreshProgress | refreshHistory | refreshReports | refreshRegistry | refreshEngineVfy
)

func refreshMaskFor(events []core.Event, reset bool) refreshMask {
	if reset {
		return refreshAll
	}
	var mask refreshMask
	for _, event := range events {
		switch event.Type {
		case "sync.reset":
			return refreshAll
		case "update.state", "cli.upgrade", "services.memory", "engine.updates", "run.progress", "run.resources", "download.progress", "engine.check":
			continue
		case "services.state", "engine.state", "service.exited", "service.exit_notice_cleared":
			mask |= refreshCore
		case "device.updated", "assets.updated":
			mask |= refreshCore | refreshRegistry | refreshEngineVfy
		case "download.state":
			var payload struct {
				Task struct {
					State string `json:"state"`
				} `json:"task"`
			}
			if json.Unmarshal(event.Data, &payload) == nil && payload.Task.State == "done" {
				mask |= refreshCore | refreshRegistry | refreshEngineVfy
			}
		case "report.created":
			mask |= refreshCore | refreshReports | refreshProgress
		case "run.state":
			mask |= refreshCore | refreshHistory | refreshProgress
		default:
			mask |= refreshCore | refreshProgress
		}
	}
	return mask
}

func fullRefresh(events []core.Event, reset bool) bool {
	return refreshMaskFor(events, reset) == refreshAll
}

func needsRefresh(events []core.Event, reset bool) bool {
	return refreshMaskFor(events, reset) != refreshNone
}
