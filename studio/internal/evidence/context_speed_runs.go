package evidence

import (
	"encoding/json"
	"errors"
)

func ContextSpeedRuns(root, planHash string, trials []string) ([]Run, error) {
	index := Index(root)
	if index.Error != nil {
		return nil, index.Error
	}
	selected := []Run{}
	seen := map[string]bool{}
	allowed := map[string]bool{}
	for _, id := range trials {
		allowed[id] = true
	}
	for _, run := range index.Runs {
		var inputs struct {
			Documents map[string]struct {
				Payload json.RawMessage `json:"payload"`
			} `json:"documents"`
		}
		var identity struct {
			ID    string `json:"run_id"`
			Trial string `json:"trial_id"`
			Plan  string `json:"plan_sha256"`
		}
		if json.Unmarshal(run.Inputs, &inputs) != nil || json.Unmarshal(inputs.Documents["run"].Payload, &identity) != nil ||
			!speedPlan(inputs.Documents["plan"].Payload) || identity.ID != run.ID || identity.Plan != planHash || !allowed[identity.Trial] || seen[identity.Trial] {
			return nil, errors.New("context_speed_run_ownership_mismatch")
		}
		seen[identity.Trial] = true
		selected = append(selected, run)
	}
	return selected, nil
}
