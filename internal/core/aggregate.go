package core

import (
	"encoding/json"
	"inferyard-studio/internal/evidence"
	"os"
	"path/filepath"
	"sort"
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
	previous          object
}

func newAggregate(raw []byte) *aggregate {
	a := &aggregate{Progress: RunProgress{Cases: []CaseProgress{}}, requests: map[string]int{}}
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
func (a *aggregate) resources(records []evidence.Record) []ResourceSample {
	points := []ResourceSample{}
	for _, r := range records {
		m := document(r.Raw)
		metric := str(text(m["metric_name"]))
		v := number(m["value"])
		if m["missing_reason"] != nil {
			v = nil
		}
		p := ResourceSample{Utc: str(text(m["utc"]))}
		switch metric {
		case "system_mem_available":
			if v != nil {
				p.MemAvailableBytes = ptr(int64(*v))
			}
		case "service_rss":
			if v != nil {
				p.ServiceRssBytes = ptr(int64(*v))
			}
		case "temperature":
			p.TemperatureCelsius = v
		case "service_cpu_ticks":
			if m["capture"] != "periodic" {
				continue
			}
			old := a.previous
			dt, pt, hz, oldval := number(m["read_finished_ns"]), number(old["read_finished_ns"]), number(m["clock_ticks_per_second"]), number(old["value"])
			same := true
			for _, k := range []string{"clock_id", "source", "server_pid", "process_start_ticks", "boot_id", "clock_ticks_per_second"} {
				if m[k] != old[k] {
					same = false
				}
			}
			if same && v != nil && oldval != nil && old["missing_reason"] == nil && dt != nil && pt != nil && hz != nil && *hz > 0 && *dt > *pt && *dt-*pt <= 2e9 && *v >= *oldval {
				p.ServiceCpuPercent = ptr((*v - *oldval) / *hz / ((*dt - *pt) / 1e9) * 100)
			}
			a.previous = m
		default:
			continue
		}
		points = append(points, p)
	}
	return points
}
func summarize(r evidence.Run, host string) (RunSummary, *aggregate, error) {
	a := newAggregate(r.Inputs)
	rows, _, err := evidence.Tail(filepath.Join(r.Path, "events.jsonl"), evidence.Cursor{})
	if err != nil && !os.IsNotExist(err) {
		return RunSummary{}, nil, err
	}
	a.events(rows)
	run, config, bundle := payload(r.Inputs, "run"), payload(r.Inputs, "config"), payload(r.Inputs, "bundle")
	v := RunSummary{RunId: r.ID, Path: r.Path, Status: "partial", Sealed: r.State == evidence.Sealed, StopReason: a.stop, StartedAt: a.started, FinishedAt: a.finished, Relation: text(run["relation"]), ParentRunId: text(run["parent_run_id"]), ModelName: text(obj(config["model"])["display_name"]), ModelPath: text(obj(config["model"])["local_path"]), Engine: text(obj(config["engine"])["adapter"]), Bundle: text(bundle["bundle_id"])}
	if r.BatchPath != "" {
		v.BatchId = ptr(filepath.Base(r.BatchPath))
	}
	if a.stop != nil {
		v.Completeness = ptr("incomplete")
		complete := *a.stop == "plan_finished" && a.Progress.Total != nil && a.Progress.Done == *a.Progress.Total && r.State == evidence.Sealed && run["relation"] != "resume" && run["diagnostic"] != true && run["kind"] == "run"
		for _, c := range a.Progress.Cases {
			if (c.State != "completed" && c.State != "failed") || (str(c.QualityState) != "pass" && str(c.QualityState) != "fail" && str(c.QualityState) != "not_applicable") {
				complete = false
			}
		}
		if complete {
			v.Completeness = ptr("complete")
		}
	}
	dirty, e := evidence.DirtyRun(host, r.ID)
	if e != nil {
		return v, a, e
	}
	v.Status = evidence.RunStatus(v.Sealed, str(v.Completeness), str(a.stop), dirty)
	score := RunScore{}
	observed := false
	for _, c := range a.Progress.Cases {
		if c.QualityState != nil {
			observed = true
		}
		if str(c.QualityState) == "pass" {
			score.Passed++
		}
		if str(c.QualityState) == "pass" || str(c.QualityState) == "fail" {
			score.Valid++
		}
	}
	if observed {
		if score.Valid > 0 {
			score.Rate = ptr(float64(score.Passed) / float64(score.Valid))
		}
		v.Score = &score
	}
	return v, a, nil
}
func (s *Service) scan(u UserSettings) (RunHistoryResponse, *ApiError) {
	h := RunHistoryResponse{ResultsRoot: u.ResultsRoot, ScannedAt: now(), Batches: []BatchSummary{}, StandaloneRuns: []RunSummary{}}
	if u.ResultsRoot == nil {
		return h, nil
	}
	if _, e := os.Stat(*u.ResultsRoot); e != nil {
		if os.IsNotExist(e) {
			return h, nil
		}
		return h, Err("internal_error")
	}
	scanRoot, resolveErr := canonical(*u.ResultsRoot)
	if resolveErr != nil {
		return h, Err("internal_error")
	}
	index := evidence.Index(scanRoot)
	if index.Error != nil {
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
	summaries := map[string]RunSummary{}
	for _, r := range index.Runs {
		v, _, e := summarize(r, host)
		if e != nil {
			return h, Err("internal_error")
		}
		summaries[r.Path] = v
		if r.BatchPath == "" {
			h.StandaloneRuns = append(h.StandaloneRuns, v)
		}
	}
	for _, b := range index.Batches {
		bs := BatchSummary{BatchId: filepath.Base(b.Path), Path: b.Path, Runs: []RunSummary{}}
		for _, r := range b.Runs {
			bs.Runs = append(bs.Runs, summaries[r.Path])
		}
		markResumable(bs.Runs, b.Runs)
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
func markResumable(summaries []RunSummary, runs []evidence.Run) {
	if len(runs) == 0 {
		return
	}
	plan := payload(runs[0].Inputs, "plan")
	trials, _ := plan["trials"].([]any)
	for _, trial := range trials {
		id := str(text(obj(trial)["trial_id"]))
		group := []int{}
		latest := -1
		for i, r := range runs {
			run := payload(r.Inputs, "run")
			if str(text(run["trial_id"])) == id {
				group = append(group, i)
				if run["relation"] != "resume" {
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
				if str(text(payload(runs[i].Inputs, "run")["parent_run_id"])) == runs[latest].ID {
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
		a := newAggregate(runs[latest].Inputs)
		rows, _, err := evidence.Tail(filepath.Join(runs[latest].Path, "events.jsonl"), evidence.Cursor{})
		if err != nil && !os.IsNotExist(err) {
			return
		}
		a.events(rows)
		if a.Progress.Total != nil && a.Progress.Done < *a.Progress.Total {
			summaries[latest].Resumable = true
			return
		}
		durationStarted, durationComplete := false, false
		for _, r := range rows {
			if r.EventType == "duration_started" {
				durationStarted = true
			}
			if r.EventType == "duration_closed" {
				durationComplete = str(text(obj(document(r.Raw)["data"])["reason"])) == "duration_elapsed"
			}
		}
		if durationStarted && !durationComplete {
			summaries[latest].Resumable = true
			return
		}
	}
}

func (s *Service) rebuild(u UserSettings) {
	h, e := s.scan(u)
	s.mu.Lock()
	defer s.mu.Unlock()
	if str(s.settings.ResultsRoot) != str(u.ResultsRoot) {
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
		h := clone(s.history)
		s.mu.Unlock()
		return h, nil
	}
	u := clone(s.settings)
	s.mu.Unlock()
	h, e := s.scan(u)
	if e == nil {
		s.mu.Lock()
		if str(s.settings.ResultsRoot) == str(u.ResultsRoot) {
			s.history = h
		}
		s.mu.Unlock()
	}
	return h, e
}
