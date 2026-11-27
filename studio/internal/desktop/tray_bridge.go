package desktop

import (
	"context"
	"path/filepath"
	"strings"

	"inferyard/studio/internal/core"
)

// ProjectTrayStatus converts core engine snapshots into a stable tray view model.
func ProjectTrayStatus(engine core.EngineState, services []core.EngineState) TrayEngineStatus {
	status := TrayEngineStatus{State: TrayEngineIdle}
	seen := make(map[string]bool)
	for _, snapshot := range append(append([]core.EngineState(nil), services...), engine) {
		state := projectTrayState(snapshot.State)
		if state == TrayEngineIdle {
			continue
		}
		summary := TrayServiceSummary{
			ModelName:  trayModelName(snapshot),
			EngineName: trayEngineName(snapshot),
			State:      state,
			Port:       snapshot.Port,
			Endpoint:   snapshot.Endpoint,
			LANAccess:  snapshot.LANAccess,
		}
		key := strings.TrimSpace(snapshot.ServiceID)
		if key == "" {
			key = summary.ModelName + "\x00" + summary.EngineName + "\x00" + summary.Endpoint
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		status.Services = append(status.Services, summary)
		if state == TrayEngineStarting || state == TrayEngineRunning || state == TrayEngineStopping {
			status.ActiveCount++
		}
		if trayStatePriority(state) > trayStatePriority(status.State) {
			status.State = state
		}
	}
	return status
}

func projectTrayState(state string) TrayEngineState {
	switch state {
	case "starting":
		return TrayEngineStarting
	case "running":
		return TrayEngineRunning
	case "stopping":
		return TrayEngineStopping
	case "failed":
		return TrayEngineFailed
	case "orphaned":
		return TrayEngineOrphaned
	default:
		return TrayEngineIdle
	}
}

func trayStatePriority(state TrayEngineState) int {
	switch state {
	case TrayEngineOrphaned:
		return 6
	case TrayEngineFailed:
		return 5
	case TrayEngineStarting:
		return 4
	case TrayEngineStopping:
		return 3
	case TrayEngineRunning:
		return 2
	default:
		return 1
	}
}

func trayModelName(state core.EngineState) string {
	for _, value := range []string{state.ModelName, state.ServedModel, state.Model, state.Candidate} {
		if value = strings.TrimSpace(value); value != "" {
			return filepath.Base(value)
		}
	}
	return ""
}

func trayEngineName(state core.EngineState) string {
	for _, value := range []string{state.EngineName, state.Family, state.Adapter} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// StartTrayStatusBridge pushes an initial snapshot and follows engine lifecycle events.
func StartTrayStatusBridge(ctx context.Context, svc *core.Service, tray *TrayController) {
	if svc == nil || tray == nil {
		return
	}
	push := func() { tray.UpdateStatus(ProjectTrayStatus(svc.Engine(), svc.ManagedServices())) }
	if svc.Bus == nil {
		push()
		return
	}
	wake, unsubscribe, cursor := svc.Bus.Subscribe()
	push()
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				events, gap, next := svc.Bus.After(cursor)
				cursor = next
				if gap || trayStatusEvents(events) {
					push()
				}
			}
		}
	}()
}

func trayStatusEvents(events []core.Event) bool {
	for _, event := range events {
		switch event.Type {
		case "engine.state", "services.state", "service.exit", "service.exited":
			return true
		}
	}
	return false
}
