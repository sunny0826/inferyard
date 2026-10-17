//go:build ui_debug && !release

package pages

import (
	"slices"

	"inferyard/studio/internal/core"
)

func (p *previewSession) report(reference string) (*core.ReportIndex, *core.ApiError) {
	if index := p.indexes[reference]; index != nil {
		return index, nil
	}
	if reference == "broken-report" && !p.reportReadFailed {
		p.reportReadFailed = true
		return nil, &core.ApiError{Code: "report_unreadable", Message: "模拟读取失败，可重新读取。"}
	}
	if reference != "preview-report" && reference != "long-report" && reference != "broken-report" {
		return nil, core.Err("not_found")
	}
	return previewReportFixture(reference), nil
}

func (p *previewSession) detail(id string) *core.RunTaskDetail {
	if detail := p.details[id]; detail != nil {
		return detail
	}
	if id == "preview-run" {
		detail := previewDetailFixture(id)
		detail.Task.State = "done"
		p.details[id] = detail
		return detail
	}
	return nil
}

func (p *previewSession) createReport(paths []string) string {
	s := p.state
	id := p.nextID("report")
	index := &core.ReportIndex{}
	summary := core.ReportSummary{ReportId: id, Path: "/preview/reports/" + id, CreatedAt: previewValue(p.timestamp())}
	for _, history := range s.historyRuns() {
		if !slices.Contains(paths, history.Path) {
			continue
		}
		run := previewReportFixture("").Runs[0]
		run.Model, run.Profile.Title = emptyText(history.ModelName), emptyText(history.ModelName)
		run.Profile.Engine.BinaryPath = emptyText(history.Engine)
		run.Source.RunID, run.Facets.BundleLabel = history.RunId, emptyText(history.Bundle)
		run.Diagnostic = true
		if !history.Sealed {
			run.Summary.StopReason = emptyText(history.StopReason)
		}
		if detail := p.details[history.RunId]; detail != nil && detail.Progress != nil {
			p := detail.Progress
			run.Requests = nil
			for _, item := range p.Cases {
				run.Requests = append(run.Requests, core.ReportRequest{CaseID: item.CaseId, CategoryLabel: emptyText(item.Category), State: item.State, Quality: item.QualityState, LatencyMS: item.DurationMs})
			}
			rate := core.ReportRate{Numerator: previewValue(float64(p.Done)), Denominator: previewValue(float64(*p.Total)), Excluded: previewValue(0.0), Complete: previewValue(history.Sealed)}
			run.Summary.CompletionRate, run.Dashboard.Quality = rate, rate
			previewReportMetrics(&run, detail)
			if len(run.Dashboard.Categories) > 0 {
				run.Dashboard.Categories = run.Dashboard.Categories[:1]
				run.Dashboard.Categories[0].Label, run.Dashboard.Categories[0].Rate = "模拟分类", rate
			}
		}
		index.Runs = append(index.Runs, run)
		summary.RunIds = append(summary.RunIds, history.RunId)
		summary.RunPaths = append(summary.RunPaths, previewValue(history.Path))
	}
	p.indexes[id] = index
	s.reports = append(s.reports, summary)
	return id
}

func previewReportMetrics(run *core.ReportRun, detail *core.RunTaskDetail) {
	d := run.Dashboard
	d.Latency.Min, d.Latency.P50, d.Latency.P95, d.Latency.Max = nil, nil, nil, nil
	var durations []float64
	for _, request := range run.Requests {
		if request.LatencyMS != nil {
			durations = append(durations, *request.LatencyMS)
		}
	}
	d.Latency.SampleCount = previewValue(float64(len(durations)))
	d.Latency.Excluded = previewValue(float64(len(run.Requests) - len(durations)))
	if len(durations) > 0 {
		slices.Sort(durations)
		middle := len(durations) / 2
		median := durations[middle]
		if len(durations)%2 == 0 {
			median = (durations[middle-1] + median) / 2
		}
		d.Latency.Min, d.Latency.Max = previewValue(durations[0]), previewValue(durations[len(durations)-1])
		d.Latency.P50, d.Latency.P95 = previewValue(median), previewValue(durations[(len(durations)*95-1)/100])
	}
	d.SampledPeakRSSBytes, d.SampledMinimumAvailableBytes = nil, nil
	d.SampledPeakRSSReason, d.MinimumAvailableReason = previewValue("no_samples"), previewValue("no_samples")
	for _, sample := range detail.Resources {
		if v := sample.ServiceRssBytes; v != nil && (d.SampledPeakRSSBytes == nil || float64(*v) > *d.SampledPeakRSSBytes) {
			d.SampledPeakRSSBytes, d.SampledPeakRSSReason = previewValue(float64(*v)), nil
		}
		if v := sample.MemAvailableBytes; v != nil && (d.SampledMinimumAvailableBytes == nil || float64(*v) < *d.SampledMinimumAvailableBytes) {
			d.SampledMinimumAvailableBytes, d.MinimumAvailableReason = previewValue(float64(*v)), nil
		}
	}
}
