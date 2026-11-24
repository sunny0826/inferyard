package evidence

import (
	"encoding/json"
	"errors"
	"os"
)

type DirtyProcess struct {
	PID        int64 `json:"pid"`
	StartTicks int64 `json:"start_ticks"`
}

type DirtyState struct {
	DirtyToken        string
	RunID             string
	Kind              string
	ServerPID         int64
	ProcessStartTicks int64
	LocalProcesses    []DirtyProcess
}

func ReadDirtyState(path string) (*DirtyState, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state struct {
		Schema            int             `json:"schema_version"`
		Dirty             *bool           `json:"dirty"`
		DirtyToken        string          `json:"dirty_token"`
		RunID             string          `json:"run_id"`
		Kind              string          `json:"kind"`
		ServerPID         int64           `json:"server_pid"`
		ProcessStartTicks int64           `json:"process_start_ticks"`
		LocalService      json.RawMessage `json:"local_service"`
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if state.Schema > 1 {
		return nil, nil
	}
	if state.Schema < 1 || state.Dirty == nil || (*state.Dirty && (state.RunID == "" || state.Kind == "")) {
		return nil, errors.New("invalid host marker")
	}
	if !*state.Dirty {
		return nil, nil
	}
	out := &DirtyState{
		DirtyToken:        state.DirtyToken,
		RunID:             state.RunID,
		Kind:              state.Kind,
		ServerPID:         state.ServerPID,
		ProcessStartTicks: state.ProcessStartTicks,
	}
	if len(state.LocalService) > 0 && string(state.LocalService) != "null" {
		var local struct {
			Service struct {
				PID        int64          `json:"pid"`
				StartTicks int64          `json:"start_ticks"`
				Workers    []DirtyProcess `json:"workers"`
			} `json:"service"`
		}
		if err := json.Unmarshal(state.LocalService, &local); err != nil || local.Service.PID <= 0 || local.Service.StartTicks <= 0 || local.Service.Workers == nil {
			return nil, errors.New("invalid local service marker")
		}
		out.LocalProcesses = append(out.LocalProcesses, DirtyProcess{PID: local.Service.PID, StartTicks: local.Service.StartTicks})
		for _, w := range local.Service.Workers {
			if w.PID <= 0 || w.StartTicks <= 0 {
				return nil, errors.New("invalid local worker marker")
			}
			out.LocalProcesses = append(out.LocalProcesses, w)
		}
	}
	return out, nil
}

func DirtyRun(path, runID string) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var state struct {
		Schema int    `json:"schema_version"`
		Dirty  *bool  `json:"dirty"`
		RunID  string `json:"run_id"`
		Kind   string `json:"kind"`
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return false, err
	}
	if state.Schema > 1 {
		return false, nil
	}
	if state.Schema < 1 || state.Dirty == nil || (*state.Dirty && (state.RunID == "" || state.Kind == "")) {
		return false, errors.New("invalid host marker")
	}
	return *state.Dirty && state.RunID == runID && state.Kind == "run", nil
}

func RunStatus(sealed bool, completeness, stopReason string, dirty bool) string {
	if stopReason == "user_cancelled" {
		return "cancelled"
	}
	if dirty {
		return "dirty"
	}
	if !sealed || completeness != "complete" {
		return "partial"
	}
	return "complete"
}
