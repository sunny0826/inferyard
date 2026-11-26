package core

import (
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type serviceCaches struct {
	mu           sync.Mutex
	reports      map[string]cachedStudioReport
	runInputs    map[string]cachedRunInputs
	speedResults map[string]cachedSpeedResult
	candidates   map[string]cachedCandidateDesc
	runs         map[string]cachedRunScan
}

type cachedStudioReport struct {
	size    int64
	modTime int64
	report  inferyard.StudioReport
}

type cachedRunInputs struct {
	size         int64
	modTime      int64
	engineFamily string
	modelPath    string
}

type cachedSpeedResult struct {
	size    int64
	modTime int64
	cliPath string
	planSHA string
	result  inferyard.ContextSpeedResult
}

type cachedCandidateDesc struct {
	size        int64
	modTime     int64
	cliKey      string
	installsKey string
	enginePath  string
	engineSize  int64
	engineMTime int64
	modelPath   string
	modelSize   int64
	modelMTime  int64
	desc        inferyard.CandidateDescription
}

func cloneValPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneRunProgress(p *RunProgress) *RunProgress {
	if p == nil {
		return nil
	}
	out := RunProgress{
		Phase:         cloneValPtr(p.Phase),
		Done:          p.Done,
		Total:         cloneValPtr(p.Total),
		CurrentCaseId: cloneValPtr(p.CurrentCaseId),
		LastSeq:       p.LastSeq,
	}
	if p.Cases != nil {
		out.Cases = make([]CaseProgress, len(p.Cases))
		for i, c := range p.Cases {
			out.Cases[i] = CaseProgress{
				CaseId:        c.CaseId,
				PlanIndex:     cloneValPtr(c.PlanIndex),
				State:         c.State,
				Category:      cloneValPtr(c.Category),
				QualityState:  cloneValPtr(c.QualityState),
				StartedAt:     cloneValPtr(c.StartedAt),
				FinishedAt:    cloneValPtr(c.FinishedAt),
				DurationMs:    cloneValPtr(c.DurationMs),
				ErrorCategory: cloneValPtr(c.ErrorCategory),
			}
		}
	}
	return &out
}

func cloneResourceSamples(in []ResourceSample) []ResourceSample {
	if in == nil {
		return nil
	}
	out := make([]ResourceSample, len(in))
	for i, s := range in {
		out[i] = ResourceSample{
			Source:         s.Source,
			Seq:            s.Seq,
			Utc:            s.Utc,
			ReadStartedNs:  s.ReadStartedNs,
			ReadFinishedNs: s.ReadFinishedNs,
			Phase:          s.Phase,
			RequestID:      cloneValPtr(s.RequestID),
			Value:          cloneValPtr(s.Value),
			MissingReason:  cloneValPtr(s.MissingReason),
		}
	}
	return out
}

func cloneRunTask(t RunTask) RunTask {
	out := t
	out.CandidatePath = cloneValPtr(t.CandidatePath)
	out.ConfigPath = cloneValPtr(t.ConfigPath)
	out.ResumeFrom = cloneValPtr(t.ResumeFrom)
	out.Bundle = cloneValPtr(t.Bundle)
	out.RunId = cloneValPtr(t.RunId)
	out.RunPath = cloneValPtr(t.RunPath)
	out.StartedAt = cloneValPtr(t.StartedAt)
	out.FinishedAt = cloneValPtr(t.FinishedAt)
	if t.ContextSpeed != nil {
		out.ContextSpeed = clone(t.ContextSpeed)
	}
	if t.Exit != nil {
		exit := *t.Exit
		exit.CliStatus = cloneValPtr(t.Exit.CliStatus)
		exit.Completeness = cloneValPtr(t.Exit.Completeness)
		exit.StopReason = cloneValPtr(t.Exit.StopReason)
		if t.Exit.Limitations != nil {
			exit.Limitations = append([]string(nil), t.Exit.Limitations...)
		}
		out.Exit = &exit
	}
	out.Error = cloneApiError(t.Error)
	return out
}

func cloneRunTaskDetail(d RunTaskDetail) RunTaskDetail {
	return RunTaskDetail{
		Task:      cloneRunTask(d.Task),
		Progress:  cloneRunProgress(d.Progress),
		Resources: cloneResourceSamples(d.Resources),
	}
}

func cloneStudioCounts(c inferyard.StudioCounts) inferyard.StudioCounts {
	return inferyard.StudioCounts{
		Numerator:   cloneValPtr(c.Numerator),
		Denominator: cloneValPtr(c.Denominator),
		Excluded:    cloneValPtr(c.Excluded),
	}
}

func cloneStudioReport(r inferyard.StudioReport) inferyard.StudioReport {
	out := inferyard.StudioReport{
		SchemaVersion: r.SchemaVersion,
		Kind:          r.Kind,
	}
	if r.Runs == nil {
		return out
	}
	out.Runs = make([]inferyard.StudioRun, len(r.Runs))
	for i, run := range r.Runs {
		cp := run
		cp.Threads = cloneValPtr(run.Threads)
		cp.Profile.Title = cloneValPtr(run.Profile.Title)
		cp.Profile.Engine.BinaryPath = cloneValPtr(run.Profile.Engine.BinaryPath)
		cp.Profile.Machine.CPU = cloneValPtr(run.Profile.Machine.CPU)
		cp.Profile.Machine.LogicalCPUs = cloneValPtr(run.Profile.Machine.LogicalCPUs)
		cp.Profile.Machine.MemoryTotalBytes = cloneValPtr(run.Profile.Machine.MemoryTotalBytes)
		cp.Summary.CompletionRate = cloneStudioCounts(run.Summary.CompletionRate)
		cp.Dashboard.Quality = inferyard.StudioQuality{
			StudioCounts: cloneStudioCounts(run.Dashboard.Quality.StudioCounts),
			Complete:     run.Dashboard.Quality.Complete,
			Reason:       cloneValPtr(run.Dashboard.Quality.Reason),
		}
		if run.Dashboard.Categories != nil {
			cp.Dashboard.Categories = make([]inferyard.StudioCategory, len(run.Dashboard.Categories))
			for j, cat := range run.Dashboard.Categories {
				cp.Dashboard.Categories[j] = inferyard.StudioCategory{
					ID:    cat.ID,
					Label: cat.Label,
					Rate:  cloneStudioCounts(cat.Rate),
				}
			}
		}
		cp.Dashboard.Latency = inferyard.StudioLatency{
			SampleCount: cloneValPtr(run.Dashboard.Latency.SampleCount),
			Excluded:    cloneValPtr(run.Dashboard.Latency.Excluded),
			Min:         cloneValPtr(run.Dashboard.Latency.Min),
			P50:         cloneValPtr(run.Dashboard.Latency.P50),
			P95:         cloneValPtr(run.Dashboard.Latency.P95),
			Max:         cloneValPtr(run.Dashboard.Latency.Max),
		}
		cp.Dashboard.SampledPeakRSSBytes = cloneValPtr(run.Dashboard.SampledPeakRSSBytes)
		cp.Dashboard.SampledPeakRSSReason = cloneValPtr(run.Dashboard.SampledPeakRSSReason)
		cp.Dashboard.SampledMinimumAvailableBytes = cloneValPtr(run.Dashboard.SampledMinimumAvailableBytes)
		cp.Dashboard.MinimumAvailableReason = cloneValPtr(run.Dashboard.MinimumAvailableReason)
		if run.Dashboard.Throughput != nil {
			tp := *run.Dashboard.Throughput
			tp.RequestsPerSecond = cloneValPtr(tp.RequestsPerSecond)
			tp.RequestsPerSecondReason = cloneValPtr(tp.RequestsPerSecondReason)
			tp.OutputTokensPerSecond = cloneValPtr(tp.OutputTokensPerSecond)
			tp.OutputTokensPerSecondReason = cloneValPtr(tp.OutputTokensPerSecondReason)
			tp.GoodputPerSecond = cloneValPtr(tp.GoodputPerSecond)
			tp.GoodputPerSecondReason = cloneValPtr(tp.GoodputPerSecondReason)
			cp.Dashboard.Throughput = &tp
		}
		if run.Dashboard.OutputBlockIntervals != nil {
			iv := *run.Dashboard.OutputBlockIntervals
			iv.Min = cloneValPtr(iv.Min)
			iv.P50 = cloneValPtr(iv.P50)
			iv.P95 = cloneValPtr(iv.P95)
			iv.P95Reason = cloneValPtr(iv.P95Reason)
			iv.Max = cloneValPtr(iv.Max)
			iv.Reason = cloneValPtr(iv.Reason)
			cp.Dashboard.OutputBlockIntervals = &iv
		}
		if run.Requests != nil {
			cp.Requests = make([]inferyard.StudioRequest, len(run.Requests))
			for j, req := range run.Requests {
				cp.Requests[j] = inferyard.StudioRequest{
					CaseID:        req.CaseID,
					CategoryLabel: req.CategoryLabel,
					State:         req.State,
					Quality:       cloneValPtr(req.Quality),
					LatencyMS:     cloneValPtr(req.LatencyMS),
				}
			}
		}
		if run.SVGOutputs != nil {
			cp.SVGOutputs = make([]inferyard.StudioSVGOutput, len(run.SVGOutputs))
			for j, svg := range run.SVGOutputs {
				cp.SVGOutputs[j] = inferyard.StudioSVGOutput{
					CaseID: svg.CaseID,
					Prompt: svg.Prompt,
					Status: svg.Status,
					SVG:    cloneValPtr(svg.SVG),
					Bytes:  svg.Bytes,
				}
			}
		}
		out.Runs[i] = cp
	}
	return out
}

func reportCacheKey(p string) string {
	if resolved, err := canonical(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

func (s *Service) loadStudioReport(studioJSONPath string) (inferyard.StudioReport, os.FileInfo, error) {
	cleanPath := reportCacheKey(studioJSONPath)
	st, err := os.Stat(cleanPath)
	if err != nil {
		s.evictStudioReportCache(cleanPath)
		return inferyard.StudioReport{}, nil, err
	}
	if !st.Mode().IsRegular() {
		s.evictStudioReportCache(cleanPath)
		return inferyard.StudioReport{}, nil, os.ErrInvalid
	}
	size, modTime := st.Size(), st.ModTime().UnixNano()
	s.caches.mu.Lock()
	if entry, ok := s.caches.reports[cleanPath]; ok && entry.size == size && entry.modTime == modTime {
		copied := cloneStudioReport(entry.report)
		s.caches.mu.Unlock()
		return copied, st, nil
	}
	s.caches.mu.Unlock()

	raw, err := os.ReadFile(cleanPath)
	if err != nil {
		s.evictStudioReportCache(cleanPath)
		return inferyard.StudioReport{}, st, err
	}
	report, err := inferyard.ParseStudioReport(raw)
	if err != nil {
		s.evictStudioReportCache(cleanPath)
		return inferyard.StudioReport{}, st, err
	}
	s.caches.mu.Lock()
	if s.caches.reports == nil {
		s.caches.reports = map[string]cachedStudioReport{}
	}
	if len(s.caches.reports) >= 512 {
		clear(s.caches.reports)
	}
	s.caches.reports[cleanPath] = cachedStudioReport{
		size:    size,
		modTime: modTime,
		report:  cloneStudioReport(report),
	}
	s.caches.mu.Unlock()
	return report, st, nil
}

func (s *Service) evictStudioReportCache(cleanPath string) {
	key := reportCacheKey(cleanPath)
	s.caches.mu.Lock()
	delete(s.caches.reports, key)
	s.caches.mu.Unlock()
}

// CachedReportIndex returns a cloned ReportIndex immediately when studio.json is
// already cached and its file size and modification timestamp are unchanged.
func (s *Service) CachedReportIndex(reference string) (*ReportIndex, bool) {
	path, e := s.resolveReportStudioJSON(reference)
	if e != nil {
		return nil, false
	}
	cleanPath := reportCacheKey(path)
	st, err := os.Stat(cleanPath)
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	s.caches.mu.Lock()
	entry, ok := s.caches.reports[cleanPath]
	if !ok || entry.size != st.Size() || entry.modTime != st.ModTime().UnixNano() {
		s.caches.mu.Unlock()
		return nil, false
	}
	copied := cloneStudioReport(entry.report)
	s.caches.mu.Unlock()
	return &copied, true
}

func (s *Service) readRunInputsConfigCached(sourcePath, reportDir string) (engineFamily, modelPath string) {
	p := strings.TrimSpace(sourcePath)
	if p == "" {
		return "", ""
	}
	if !filepath.IsAbs(p) && reportDir != "" {
		p = filepath.Join(reportDir, p)
	}
	inputsPath := filepath.Clean(filepath.Join(p, "inputs.json"))
	st, err := os.Stat(inputsPath)
	if err != nil || !st.Mode().IsRegular() {
		return "", ""
	}
	size, modTime := st.Size(), st.ModTime().UnixNano()
	s.caches.mu.Lock()
	if entry, ok := s.caches.runInputs[inputsPath]; ok && entry.size == size && entry.modTime == modTime {
		s.caches.mu.Unlock()
		return entry.engineFamily, entry.modelPath
	}
	s.caches.mu.Unlock()

	fam, mp := readRunInputsConfig(sourcePath, reportDir)
	s.caches.mu.Lock()
	if s.caches.runInputs == nil {
		s.caches.runInputs = map[string]cachedRunInputs{}
	}
	if len(s.caches.runInputs) >= 1024 {
		clear(s.caches.runInputs)
	}
	s.caches.runInputs[inputsPath] = cachedRunInputs{
		size:         size,
		modTime:      modTime,
		engineFamily: fam,
		modelPath:    mp,
	}
	s.caches.mu.Unlock()
	return fam, mp
}

func (s *Service) cachedSpeedResultGet(batchPath, cliPath, planSHA string) (inferyard.ContextSpeedResult, bool) {
	st, err := os.Stat(filepath.Join(batchPath, "batch.json"))
	if err != nil || !st.Mode().IsRegular() {
		return inferyard.ContextSpeedResult{}, false
	}
	s.caches.mu.Lock()
	defer s.caches.mu.Unlock()
	entry, ok := s.caches.speedResults[batchPath]
	if !ok || entry.size != st.Size() || entry.modTime != st.ModTime().UnixNano() || entry.cliPath != cliPath || entry.planSHA != planSHA {
		return inferyard.ContextSpeedResult{}, false
	}
	return clone(entry.result), true
}

func (s *Service) cachedSpeedResultPut(batchPath, cliPath, planSHA string, result inferyard.ContextSpeedResult) {
	st, err := os.Stat(filepath.Join(batchPath, "batch.json"))
	if err != nil || !st.Mode().IsRegular() {
		return
	}
	s.caches.mu.Lock()
	defer s.caches.mu.Unlock()
	if s.caches.speedResults == nil {
		s.caches.speedResults = map[string]cachedSpeedResult{}
	}
	if len(s.caches.speedResults) >= 256 {
		clear(s.caches.speedResults)
	}
	s.caches.speedResults[batchPath] = cachedSpeedResult{
		size:    st.Size(),
		modTime: st.ModTime().UnixNano(),
		cliPath: cliPath,
		planSHA: planSHA,
		result:  clone(result),
	}
}
