package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/evidence"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readSpeedPlan(path string) (SpeedPlanRef, *ApiError) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SpeedPlanRef{}, Err("plan_unreadable")
	}
	var p struct {
		SchemaVersion int    `json:"schema_version"`
		SHA256        string `json:"plan_sha256"`
		Experiment    struct {
			Profile string `json:"profile"`
			Budget  struct {
				MaxRequests    int     `json:"max_requests"`
				MaxWallSeconds float64 `json:"max_wall_seconds"`
			} `json:"budget"`
		} `json:"experiment"`
		Trials []struct {
			ID          string `json:"trial_id"`
			RepeatIndex int    `json:"repeat_index"`
		} `json:"trials"`
	}
	if json.Unmarshal(raw, &p) != nil || p.SchemaVersion != 3 || p.Experiment.Profile != "context-speed.v1" || len(p.SHA256) != 64 || len(p.Trials) != 5 || p.Experiment.Budget.MaxRequests != 5 || p.Experiment.Budget.MaxWallSeconds <= 0 {
		return SpeedPlanRef{}, Err("plan_invalid")
	}
	ref := SpeedPlanRef{Path: path, SHA256: p.SHA256, MaxWallSeconds: p.Experiment.Budget.MaxWallSeconds, TrialIDs: []string{}}
	seen := map[string]bool{}
	for i, t := range p.Trials {
		if t.ID == "" || t.RepeatIndex != i || seen[t.ID] {
			return SpeedPlanRef{}, Err("plan_invalid")
		}
		seen[t.ID] = true
		ref.TrialIDs = append(ref.TrialIDs, t.ID)
	}
	return ref, nil
}

func saveSpeedTask(root string, task RunTask) error {
	if root == "" || task.TaskId == "" || strings.ContainsAny(task.TaskId, `/\:.`) {
		return os.ErrInvalid
	}
	workspace := filepath.Dir(filepath.Dir(root))
	resolved, err := Resolve(workspace, root)
	if err != nil || resolved != root {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	target := filepath.Join(root, task.TaskId+".json")
	if st, err := os.Lstat(target); err == nil && !st.Mode().IsRegular() {
		return os.ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Version int     `json:"version"`
		Task    RunTask `json:"task"`
	}{1, task})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".task-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, target)
	}
	return err
}

func (s *Service) ContextSpeedHistory() ([]RunTask, *ApiError) {
	u := s.userSettings()
	w := workspaceRoot(u)
	rows := []RunTask{}
	if w == nil || u.ResultsRoot == nil {
		return rows, nil
	}
	root, err := Resolve(*w, filepath.Join(*w, "logs", "context-speed-tasks"))
	if err != nil {
		return nil, Err("task_history_unreadable")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return nil, Err("task_history_unreadable")
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		row := RunTask{TaskId: id, Kind: "context_speed", State: "failed", Error: Err("task_record_invalid"), ContextSpeed: &ContextSpeedTask{Stage: "failed"}}
		path, e := Resolve(root, filepath.Join(root, entry.Name()))
		if e == nil && !entry.IsDir() {
			raw, e := os.ReadFile(path)
			var wire struct {
				Version int     `json:"version"`
				Task    RunTask `json:"task"`
			}
			if e == nil && json.Unmarshal(raw, &wire) == nil && wire.Version == 1 && wire.Task.TaskId == id && wire.Task.Kind == "context_speed" && wire.Task.ContextSpeed != nil {
				v := wire.Task
				valid := true
				if v.ContextSpeed.Batch != nil {
					_, e = Resolve(*u.ResultsRoot, v.ContextSpeed.Batch.Path)
					valid = e == nil
				}
				if v.ContextSpeed.Plan != nil {
					_, e = Resolve(*w, v.ContextSpeed.Plan.Path)
					valid = valid && e == nil
				}
				if valid {
					row = v
					if !terminal(row.State) {
						row.State = "failed"
						row.Error = Err("task_interrupted")
					}
				}
			}
		}
		rows = append(rows, row)
	}
	paths, scanErr := evidence.ContextSpeedBatchPaths(*u.ResultsRoot)
	if scanErr != nil {
		return rows, Err("task_history_unreadable")
	}
	for _, path := range paths {
		path, err = Resolve(*u.ResultsRoot, path)
		if err != nil {
			continue
		}
		found := false
		for _, row := range rows {
			if row.ContextSpeed != nil && row.ContextSpeed.Batch != nil && row.ContextSpeed.Batch.Path == path {
				found = true
			}
		}
		if found {
			continue
		}
		planPath, pathErr := Resolve(path, filepath.Join(path, "frozen-plan", "plan.json"))
		var plan SpeedPlanRef
		planErr := Err("plan_unreadable")
		if pathErr == nil {
			plan, planErr = readSpeedPlan(planPath)
		}
		hash := sha256.Sum256([]byte(path))
		task := RunTask{TaskId: fmt.Sprintf("batch%x", hash[:16]), Kind: "context_speed", State: "done", ContextSpeed: &ContextSpeedTask{Stage: "done", Batch: &SpeedBatchRef{Path: path}}}
		if planErr != nil {
			task.State = "failed"
			task.Error = planErr
		} else {
			task.ContextSpeed.Plan = &plan
		}
		s.speedFrozenIdentity(path, task.ContextSpeed)
		rows = append(rows, task)
	}
	live := s.RunTasks()
	for _, t := range live {
		if t.ContextSpeed == nil {
			continue
		}
		found := false
		for i, r := range rows {
			if r.TaskId == t.TaskId {
				rows[i] = t
				found = true
				break
			}
		}
		if !found {
			rows = append(rows, t)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
	return rows, nil
}
