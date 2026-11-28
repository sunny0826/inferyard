package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"inferyard/studio/internal/evidence"
	"os"
	"path/filepath"
)

type fileStatSig struct {
	exists  bool
	regular bool
	size    int64
	modTime int64
}

type cachedRunScan struct {
	batchPath         string
	inputsSig         fileStatSig
	manifestSig       fileStatSig
	eventsSig         fileStatSig
	isContextSpeed    bool
	id                string
	path              string
	sealed            bool
	trialID           string
	relation          string
	parentRunID       string
	planTrialIDs      []string
	baseSummary       RunSummary
	resumableByEvents bool
}

func lstatSig(path string) (fileStatSig, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fileStatSig{}, nil
	}
	if err != nil {
		return fileStatSig{}, err
	}
	return fileStatSig{
		exists:  true,
		regular: info.Mode().IsRegular(),
		size:    info.Size(),
		modTime: info.ModTime().UnixNano(),
	}, nil
}

func cloneRunSummary(r RunSummary) RunSummary {
	out := r
	out.BatchId = cloneValPtr(r.BatchId)
	out.Completeness = cloneValPtr(r.Completeness)
	out.StopReason = cloneValPtr(r.StopReason)
	out.ModelName = cloneValPtr(r.ModelName)
	out.ModelPath = cloneValPtr(r.ModelPath)
	out.Engine = cloneValPtr(r.Engine)
	out.EngineFamily = cloneValPtr(r.EngineFamily)
	out.ServedModel = cloneValPtr(r.ServedModel)
	out.Bundle = cloneValPtr(r.Bundle)
	if r.Score != nil {
		sc := *r.Score
		sc.Rate = cloneValPtr(r.Score.Rate)
		out.Score = &sc
	}
	out.Relation = cloneValPtr(r.Relation)
	out.ParentRunId = cloneValPtr(r.ParentRunId)
	out.StartedAt = cloneValPtr(r.StartedAt)
	out.FinishedAt = cloneValPtr(r.FinishedAt)
	return out
}

func cloneRunHistoryResponse(h RunHistoryResponse) RunHistoryResponse {
	out := RunHistoryResponse{
		ResultsRoot: cloneValPtr(h.ResultsRoot),
		ScannedAt:   h.ScannedAt,
		Batches:     make([]BatchSummary, len(h.Batches)),
	}
	for i, b := range h.Batches {
		runs := make([]RunSummary, len(b.Runs))
		for j, r := range b.Runs {
			runs[j] = cloneRunSummary(r)
		}
		out.Batches[i] = BatchSummary{
			BatchId: b.BatchId,
			Path:    b.Path,
			Runs:    runs,
		}
	}
	out.StandaloneRuns = make([]RunSummary, len(h.StandaloneRuns))
	for i, r := range h.StandaloneRuns {
		out.StandaloneRuns[i] = cloneRunSummary(r)
	}
	return out
}

func readHostDirtyRunID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var state struct {
		Schema int    `json:"schema_version"`
		Dirty  *bool  `json:"dirty"`
		RunID  string `json:"run_id"`
		Kind   string `json:"kind"`
	}
	if err = json.Unmarshal(raw, &state); err != nil {
		return "", err
	}
	if state.Schema > 1 {
		return "", nil
	}
	if state.Schema < 1 || state.Dirty == nil || (*state.Dirty && (state.RunID == "" || state.Kind == "")) {
		return "", errors.New("invalid host marker")
	}
	if *state.Dirty && state.Kind == "run" {
		return state.RunID, nil
	}
	return "", nil
}

func readLayout2Run(dir, batch string) (evidence.Run, error) {
	run := evidence.Run{Path: dir, BatchPath: batch, State: evidence.Partial}
	raw, err := readRegularJSONObject(filepath.Join(dir, "inputs.json"))
	if err != nil {
		return run, err
	}
	var inputs struct {
		SchemaVersion int                        `json:"schema_version"`
		Layout        int                        `json:"evidence_layout_version"`
		RunID         string                     `json:"run_id"`
		Documents     map[string]json.RawMessage `json:"documents"`
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		return run, err
	}
	if inputs.SchemaVersion != 3 || inputs.Layout != 2 || inputs.RunID == "" || len(inputs.Documents["run"]) == 0 {
		return run, fmt.Errorf("invalid layout2 inputs")
	}
	run.ID, run.Inputs = inputs.RunID, raw
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, lerr := os.Lstat(manifestPath); os.IsNotExist(lerr) {
		return run, nil
	}
	mraw, err := readRegularJSONObject(manifestPath)
	if err != nil {
		return run, err
	}
	var manifest struct {
		SchemaVersion int    `json:"schema_version"`
		Layout        int    `json:"evidence_layout_version"`
		RunID         string `json:"run_id"`
		Sealed        *bool  `json:"sealed"`
	}
	if err = json.Unmarshal(mraw, &manifest); err != nil {
		return run, err
	}
	if manifest.SchemaVersion != 3 || manifest.Layout != 2 || manifest.RunID != run.ID || manifest.Sealed == nil {
		return run, fmt.Errorf("invalid manifest identity or seal")
	}
	run.Manifest = mraw
	if *manifest.Sealed {
		run.State = evidence.Sealed
	}
	return run, nil
}

func readRegularJSONObject(path string) (json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("invalid object: %s", path)
	}
	return raw, nil
}

func (s *Service) scanRunCached(dir, batch string) (cachedRunScan, bool) {
	inSig, inErr := lstatSig(filepath.Join(dir, "inputs.json"))
	if inErr != nil || !inSig.exists || !inSig.regular {
		return cachedRunScan{}, false
	}
	manSig, manErr := lstatSig(filepath.Join(dir, "manifest.json"))
	if manErr != nil || (manSig.exists && !manSig.regular) {
		return cachedRunScan{}, false
	}
	evSig, evErr := lstatSig(filepath.Join(dir, "events.jsonl"))
	if evErr != nil || (evSig.exists && !evSig.regular) {
		return cachedRunScan{}, false
	}

	s.caches.mu.Lock()
	if entry, ok := s.caches.runs[dir]; ok &&
		entry.batchPath == batch &&
		entry.inputsSig == inSig &&
		entry.manifestSig == manSig &&
		entry.eventsSig == evSig {
		copied := entry
		copied.baseSummary = cloneRunSummary(entry.baseSummary)
		s.caches.mu.Unlock()
		return copied, true
	}
	s.caches.mu.Unlock()

	r, err := readLayout2Run(dir, batch)
	if err != nil {
		return cachedRunScan{}, false
	}
	if evidence.IsContextSpeedRun(r.Inputs) {
		entry := cachedRunScan{
			batchPath:      batch,
			inputsSig:      inSig,
			manifestSig:    manSig,
			eventsSig:      evSig,
			isContextSpeed: true,
			id:             r.ID,
			path:           dir,
		}
		s.storeCachedRunScan(dir, entry)
		return entry, true
	}

	a := newAggregate(r.Inputs)
	rows, _, tailErr := evidence.Tail(filepath.Join(r.Path, "events.jsonl"), evidence.Cursor{})
	if tailErr != nil && !os.IsNotExist(tailErr) {
		return cachedRunScan{}, false
	}
	a.events(rows)

	resumableByEvents := a.Progress.Total != nil && a.Progress.Done < *a.Progress.Total
	if !resumableByEvents {
		durationStarted, durationComplete := false, false
		for _, rec := range rows {
			if rec.EventType == "duration_started" {
				durationStarted = true
			}
			if rec.EventType == "duration_closed" {
				durationComplete = str(text(obj(document(rec.Raw)["data"])["reason"])) == "duration_elapsed"
			}
		}
		resumableByEvents = durationStarted && !durationComplete
	}

	doc := document(r.Inputs)
	docs := obj(doc["documents"])
	runDoc := obj(obj(docs["run"])["payload"])
	configDoc := obj(obj(docs["config"])["payload"])
	bundleDoc := obj(obj(docs["bundle"])["payload"])
	planDoc := obj(obj(docs["plan"])["payload"])

	var planTrials []string
	if rawTrials, ok := planDoc["trials"].([]any); ok {
		planTrials = make([]string, 0, len(rawTrials))
		for _, tr := range rawTrials {
			planTrials = append(planTrials, str(text(obj(tr)["trial_id"])))
		}
	}

	categories := caseCategories(r.Inputs)
	v := RunSummary{
		RunId:       r.ID,
		Path:        r.Path,
		Status:      "partial",
		Sealed:      r.State == evidence.Sealed,
		StopReason:  a.stop,
		StartedAt:   a.started,
		FinishedAt:  a.finished,
		Relation:    text(runDoc["relation"]),
		ParentRunId: text(runDoc["parent_run_id"]),
		ModelName:    text(obj(configDoc["model"])["display_name"]),
		ModelPath:    text(obj(configDoc["model"])["local_path"]),
		Engine:       text(obj(configDoc["engine"])["adapter"]),
		EngineFamily: text(obj(configDoc["engine"])["family"]),
		ServedModel:  text(obj(configDoc["engine"])["served_model"]),
		Bundle:       text(bundleDoc["bundle_id"]),
	}
	if r.BatchPath != "" {
		v.BatchId = ptr(filepath.Base(r.BatchPath))
	}
	if a.stop != nil {
		v.Completeness = ptr("incomplete")
		if runComplete(a.Progress, r, runDoc, a.stop) {
			v.Completeness = ptr("complete")
		}
	}
	if score, ok := runScore(a.Progress.Cases, categories, str(v.Completeness) == "complete"); ok {
		v.Score = &score
	}

	entry := cachedRunScan{
		batchPath:         batch,
		inputsSig:         inSig,
		manifestSig:       manSig,
		eventsSig:         evSig,
		id:                r.ID,
		path:              r.Path,
		sealed:            v.Sealed,
		trialID:           str(text(runDoc["trial_id"])),
		relation:          str(text(runDoc["relation"])),
		parentRunID:       str(text(runDoc["parent_run_id"])),
		planTrialIDs:      planTrials,
		baseSummary:       cloneRunSummary(v),
		resumableByEvents: resumableByEvents,
	}
	s.storeCachedRunScan(dir, entry)
	return entry, true
}

func (s *Service) storeCachedRunScan(dir string, entry cachedRunScan) {
	s.caches.mu.Lock()
	defer s.caches.mu.Unlock()
	if s.caches.runs == nil {
		s.caches.runs = map[string]cachedRunScan{}
	}
	if len(s.caches.runs) >= 2048 {
		clear(s.caches.runs)
	}
	copied := entry
	copied.baseSummary = cloneRunSummary(entry.baseSummary)
	s.caches.runs[dir] = copied
}
