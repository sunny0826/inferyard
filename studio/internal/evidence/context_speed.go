package evidence

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ContextSpeedBatchPaths discovers frozen profile declarations. It does not
// authenticate them; opening each entry must invoke the CLI reader.
func ContextSpeedBatchPaths(root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, err := os.Lstat(filepath.Join(path, "batch.json")); err != nil {
			return nil
		}
		planPath := filepath.Join(path, "frozen-plan", "plan.json")
		resolved, e := filepath.EvalSymlinks(planPath)
		if e != nil {
			paths = append(paths, path)
			return filepath.SkipDir
		}
		rel, e := filepath.Rel(root, resolved)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			paths = append(paths, path)
			return filepath.SkipDir
		}
		raw, err := readObject(planPath)
		if err != nil || speedPlan(raw) || !knownOrdinaryPlan(raw) {
			paths = append(paths, path)
		}
		return filepath.SkipDir
	})
	return paths, err
}

func speedPlan(raw []byte) bool {
	var p struct {
		Experiment struct {
			Profile string `json:"profile"`
		} `json:"experiment"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Experiment.Profile == "context-speed.v1"
}

func IsContextSpeedRun(raw []byte) bool {
	var inputs struct {
		Documents map[string]json.RawMessage `json:"documents"`
	}
	if json.Unmarshal(raw, &inputs) != nil {
		return false
	}
	var plan struct {
		Payload json.RawMessage `json:"payload"`
	}
	return json.Unmarshal(inputs.Documents["plan"], &plan) == nil && speedPlan(plan.Payload)
}

func knownOrdinaryPlan(raw []byte) bool {
	var p struct {
		Version    int    `json:"schema_version"`
		Hash       string `json:"plan_sha256"`
		Experiment struct {
			Profile   *string           `json:"profile"`
			Workloads []json.RawMessage `json:"workloads"`
		} `json:"experiment"`
		Trials []json.RawMessage `json:"trials"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Version == 3 && len(p.Hash) == 64 && p.Experiment.Profile == nil && len(p.Experiment.Workloads) > 0 && len(p.Trials) > 0
}

func IsContextSpeedBatch(path string) bool {
	plan := filepath.Join(path, "frozen-plan", "plan.json")
	resolved, err := filepath.EvalSymlinks(plan)
	if err != nil {
		return false
	}
	root, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	raw, err := readObject(plan)
	return err == nil && speedPlan(raw)
}
