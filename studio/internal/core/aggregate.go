package core

import (
	"encoding/json"
	"inferyard/studio/internal/evidence"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type object map[string]any

func obj(v any) object { m, _ := v.(map[string]any); return m }
func text(v any) *string {
	v1, ok := v.(string)
	if !ok || v1 == "" {
		return nil
	}
	return &v1
}
func number(v any) *float64 {
	n, ok := v.(float64)
	if !ok {
		return nil
	}
	return &n
}
func integer(v any) *int64 {
	n := number(v)
	if n == nil {
		return nil
	}
	return ptr(int64(*n))
}
func document(raw []byte) object { var m object; _ = json.Unmarshal(raw, &m); return m }
func payload(raw []byte, name string) object {
	return obj(obj(obj(document(raw)["documents"])[name])["payload"])
}

type aggregate struct {
	Progress          RunProgress
	requests          map[string]int
	stop              *string
	started, finished *string
	cpuPrevious       map[ResourceSource]object
	resourceGap       time.Duration
}

func newAggregate(raw []byte) *aggregate {
	a := &aggregate{
		Progress: RunProgress{Cases: []CaseProgress{}}, requests: map[string]int{},
		cpuPrevious: map[ResourceSource]object{}, resourceGap: 2 * time.Second,
	}
	if interval := number(obj(payload(raw, "config")["telemetry"])["interval_ms"]); interval != nil && *interval > 0 {
		a.resourceGap = time.Duration(*interval * 2 * float64(time.Millisecond))
	}
	selection := payload(raw, "selection")
	if ids, ok := selection["case_ids"].([]any); ok {
		a.Progress.Total = ptr(int64(len(ids)))
		for _, id := range ids {
			if cid, ok := id.(string); ok {
				a.Progress.Cases = append(a.Progress.Cases, CaseProgress{CaseId: cid, State: "pending"})
			}
		}
	}
	return a
}
func (a *aggregate) events(records []evidence.Record) {
	for _, record := range records {
		m := document(record.Raw)
		utc := text(m["utc"])
		if a.started == nil {
			a.started = utc
		}
		a.Progress.LastSeq = record.Seq
		if record.Phase != "" {
			a.Progress.Phase = ptr(record.Phase)
		}
		d := obj(m["data"])
		request := str(text(m["request_id"]))
		switch record.EventType {
		case "request_started":
			id := str(text(d["case_id"]))
			if id == "" {
				continue
			}
			i := -1
			for j, c := range a.Progress.Cases {
				if c.CaseId == id {
					i = j
					break
				}
			}
			if i < 0 {
				i = len(a.Progress.Cases)
				a.Progress.Cases = append(a.Progress.Cases, CaseProgress{CaseId: id})
			}
			a.requests[request] = i
			c := &a.Progress.Cases[i]
			c.State = "running"
			c.StartedAt = utc
			c.PlanIndex = integer(d["plan_index"])
			a.Progress.CurrentCaseId = &c.CaseId
		case "request_finished":
			i, ok := a.requests[request]
			if !ok {
				continue
			}
			c := &a.Progress.Cases[i]
			c.State = str(text(d["execution_state"]))
			c.FinishedAt = utc
			c.ErrorCategory = text(d["error_category"])
			start, end := number(d["t_send_ns"]), number(d["t_terminal_ns"])
			if start != nil && end != nil && *end >= *start {
				c.DurationMs = ptr((*end - *start) / 1e6)
			}
			a.Progress.CurrentCaseId = nil
		case "score":
			i, ok := a.requests[request]
			if ok {
				c := &a.Progress.Cases[i]
				c.Category = text(d["category"])
				c.QualityState = text(d["quality_state"])
			}
		case "run_stopped":
			a.stop = text(d["reason"])
			a.finished = utc
		}
	}
	a.Progress.Done = 0
	for _, c := range a.Progress.Cases {
		switch c.State {
		case "completed", "failed", "cancelled", "invalid":
			a.Progress.Done++
		}
	}
}
func summarize(r evidence.Run, host string) (RunSummary, *aggregate, error) {
	a := newAggregate(r.Inputs)
	rows, _, err := evidence.Tail(filepath.Join(r.Path, "events.jsonl"), evidence.Cursor{})
	if err != nil && !os.IsNotExist(err) {
		return RunSummary{}, nil, err
	}
	a.events(rows)
	run, config, bundle := payload(r.Inputs, "run"), payload(r.Inputs, "config"), payload(r.Inputs, "bundle")
	categories := caseCategories(r.Inputs)
	v := RunSummary{RunId: r.ID, Path: r.Path, Status: "partial", Sealed: r.State == evidence.Sealed, StopReason: a.stop, StartedAt: a.started, FinishedAt: a.finished, Relation: text(run["relation"]), ParentRunId: text(run["parent_run_id"]), ModelName: text(obj(config["model"])["display_name"]), ModelPath: text(obj(config["model"])["local_path"]), Engine: text(obj(config["engine"])["adapter"]), Bundle: text(bundle["bundle_id"])}
	if r.BatchPath != "" {
		v.BatchId = ptr(filepath.Base(r.BatchPath))
	}
	if a.stop != nil {
		v.Completeness = ptr("incomplete")
		if runComplete(a.Progress, r, run, a.stop) {
			v.Completeness = ptr("complete")
		}
	}
	dirty, e := evidence.DirtyRun(host, r.ID)
	if e != nil {
		return v, a, e
	}
	v.Status = evidence.RunStatus(v.Sealed, str(v.Completeness), str(a.stop), dirty)
	if score, ok := runScore(a.Progress.Cases, categories, str(v.Completeness) == "complete"); ok {
		v.Score = &score
	}
	return v, a, nil
}

func (s *Service) scan(u UserSettings) (RunHistoryResponse, *ApiError) {
	results := s.resultsRoot(u)
	h := RunHistoryResponse{ResultsRoot: results, ScannedAt: now(), Batches: []BatchSummary{}, StandaloneRuns: []RunSummary{}}
	if results == nil {
		return h, nil
	}
	if _, e := os.Stat(*results); e != nil {
		if os.IsNotExist(e) {
			return h, nil
		}
		return h, Err("internal_error")
	}
	scanRoot, resolveErr := canonical(*results)
	if resolveErr != nil {
		return h, Err("internal_error")
	}
	host := s.hostStatePath
	if host == "" {
		var e error
		host, e = evidence.HostStatePath()
		if e != nil {
			return h, Err("internal_error")
		}
	}
	dirtyRunID, dirtyErr := readHostDirtyRunID(host)
	if dirtyErr != nil {
		return h, Err("internal_error")
	}

	type scannedBatch struct {
		path string
		runs []cachedRunScan
	}
	var allRuns []cachedRunScan
	var batches []scannedBatch
	skipped := 0
	var walkErr error
	var walk func(string, string)
	walk = func(dir, batch string) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			skipped++
			if err != nil && !os.IsNotExist(err) && walkErr == nil {
				walkErr = err
			}
			return
		}
		if _, err := os.Lstat(filepath.Join(dir, "inputs.json")); !os.IsNotExist(err) {
			entry, ok := s.scanRunCached(dir, batch)
			if !ok {
				skipped++
				return
			}
			allRuns = append(allRuns, entry)
			return
		}
		if _, err := os.Lstat(filepath.Join(dir, "batch.json")); !os.IsNotExist(err) {
			if _, err := readRegularJSONObject(filepath.Join(dir, "batch.json")); err != nil {
				skipped++
				return
			}
			start := len(allRuns)
			walk(filepath.Join(dir, "runs"), dir)
			runs := append([]cachedRunScan(nil), allRuns[start:]...)
			batches = append(batches, scannedBatch{path: dir, runs: runs})
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			skipped++
			if !os.IsNotExist(err) && walkErr == nil {
				walkErr = err
			}
			return
		}
		children := 0
		for _, entry := range entries {
			if entry.IsDir() {
				children++
				walk(filepath.Join(dir, entry.Name()), batch)
			} else if entry.Type()&os.ModeSymlink != 0 {
				skipped++
			}
		}
		if children == 0 {
			skipped++
		}
	}
	walk(scanRoot, "")
	if walkErr != nil {
		return h, Err("internal_error")
	}
	defer func() {
		if skipped > 0 {
			log.Printf("history scan: skipped %d entries", skipped)
		}
	}()

	summaries := make(map[string]RunSummary, len(allRuns))
	for _, entry := range allRuns {
		if entry.isContextSpeed {
			continue
		}
		v := entry.baseSummary
		dirty := dirtyRunID != "" && entry.id == dirtyRunID
		v.Status = evidence.RunStatus(v.Sealed, str(v.Completeness), str(v.StopReason), dirty)
		summaries[entry.path] = v
		if entry.batchPath == "" {
			h.StandaloneRuns = append(h.StandaloneRuns, v)
		}
	}
	for _, b := range batches {
		if evidence.IsContextSpeedBatch(b.path) {
			continue
		}
		bs := BatchSummary{BatchId: filepath.Base(b.path), Path: b.path, Runs: []RunSummary{}}
		var validRuns []cachedRunScan
		for _, r := range b.runs {
			if summary, ok := summaries[r.path]; ok {
				bs.Runs = append(bs.Runs, summary)
				validRuns = append(validRuns, r)
			}
		}
		if len(bs.Runs) == 0 && len(b.runs) > 0 {
			continue
		}
		markResumableCached(bs.Runs, validRuns)
		sort.SliceStable(bs.Runs, func(i, j int) bool { return str(bs.Runs[i].StartedAt) > str(bs.Runs[j].StartedAt) })
		h.Batches = append(h.Batches, bs)
	}
	sort.SliceStable(h.StandaloneRuns, func(i, j int) bool { return str(h.StandaloneRuns[i].StartedAt) > str(h.StandaloneRuns[j].StartedAt) })
	sort.SliceStable(h.Batches, func(i, j int) bool {
		a, b := "", ""
		if len(h.Batches[i].Runs) > 0 {
			a = str(h.Batches[i].Runs[0].StartedAt)
		}
		if len(h.Batches[j].Runs) > 0 {
			b = str(h.Batches[j].Runs[0].StartedAt)
		}
		return a > b
	})
	return h, nil
}

func markResumableCached(summaries []RunSummary, runs []cachedRunScan) {
	if len(runs) == 0 {
		return
	}
	for _, id := range runs[0].planTrialIDs {
		group := []int{}
		latest := -1
		for i, r := range runs {
			if r.trialID == id {
				group = append(group, i)
				if r.relation != "resume" {
					if latest != -1 {
						return
					}
					latest = i
				}
			}
		}
		if latest < 0 {
			return
		}
		visited := map[int]bool{}
		for {
			if visited[latest] {
				return
			}
			visited[latest] = true
			child := -1
			for _, i := range group {
				if runs[i].parentRunID == runs[latest].id {
					if child != -1 {
						return
					}
					child = i
				}
			}
			if child < 0 {
				break
			}
			latest = child
		}
		if len(visited) != len(group) {
			return
		}
		if runs[latest].resumableByEvents {
			summaries[latest].Resumable = true
			return
		}
	}
}

func (s *Service) rebuild(u UserSettings) {
	h, e := s.scan(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	if str(s.resultsRoot(s.settings)) != str(s.resultsRoot(u)) {
		return
	}
	if e == nil {
		s.history = h
	}
	s.rebuilding = false
}
func (s *Service) History() (RunHistoryResponse, *ApiError) {
	s.mu.Lock()
	if s.rebuilding {
		h := cloneRunHistoryResponse(s.history)
		s.mu.Unlock()
		return h, nil
	}
	u := clone(s.settings)
	s.mu.Unlock()
	h, e := s.scan(u)
	if e == nil {
		s.mu.Lock()
		if str(s.resultsRoot(s.settings)) == str(s.resultsRoot(u)) {
			s.history = h
		}
		s.mu.Unlock()
	}
	return h, e
}
