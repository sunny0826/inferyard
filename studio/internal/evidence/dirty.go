package evidence

import (
	"encoding/json"
	"errors"
	"os"
)

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
