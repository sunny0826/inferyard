//go:build ui_debug && !release

package pages

import (
	"slices"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

const previewPelicanSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 360 220" width="360" height="220">` +
	`<rect width="360" height="220" fill="#f8fafc"/>` +
	`<line x1="20" y1="184" x2="340" y2="184" stroke="#cbd5e1" stroke-width="3" stroke-linecap="round"/>` +
	`<g fill="none" stroke="#1e293b" stroke-width="4">` +
	`<g transform="translate(115 150)"><circle r="30"/><line x1="-26" y1="0" x2="26" y2="0" stroke-width="2"/><line x1="0" y1="-26" x2="0" y2="26" stroke-width="2"/>` +
	`<animateTransform attributeName="transform" type="rotate" from="0 0 0" to="360 0 0" dur="1.4s" repeatCount="indefinite" additive="sum"/></g>` +
	`<g transform="translate(245 150)"><circle r="30"/><line x1="-26" y1="0" x2="26" y2="0" stroke-width="2"/><line x1="0" y1="-26" x2="0" y2="26" stroke-width="2"/>` +
	`<animateTransform attributeName="transform" type="rotate" from="0 0 0" to="360 0 0" dur="1.4s" repeatCount="indefinite" additive="sum"/></g>` +
	`<path d="M115 150 L168 150 L215 102 L145 102 Z M215 102 L245 150 M205 90 L230 90" stroke-linejoin="round" stroke-linecap="round"/>` +
	`</g>` +
	`<g><animateTransform attributeName="transform" type="translate" values="0 0;0 -4;0 0" dur="0.7s" repeatCount="indefinite"/>` +
	`<ellipse cx="165" cy="94" rx="28" ry="18" fill="#ffffff" stroke="#334155" stroke-width="3"/>` +
	`<path d="M150 92 Q132 76 146 72 Q165 78 176 92" fill="#e2e8f0" stroke="#334155" stroke-width="2.5">` +
	`<animateTransform attributeName="transform" type="rotate" values="0 160 90;-14 160 90;0 160 90" dur="0.7s" repeatCount="indefinite"/></path>` +
	`<path d="M184 86 Q196 66 202 58" fill="none" stroke="#334155" stroke-width="6" stroke-linecap="round"/>` +
	`<circle cx="204" cy="54" r="11" fill="#ffffff" stroke="#334155" stroke-width="3"/>` +
	`<circle cx="208" cy="51" r="2" fill="#0f172a"/>` +
	`<path d="M214 52 L252 57 Q234 72 213 61 Z" fill="#f59e0b" stroke="#b45309" stroke-width="2"/>` +
	`</g></svg>`

func previewPelicanOutputs() []core.ReportSVGOutput {
	svg := previewPelicanSVG
	return []core.ReportSVGOutput{{
		CaseID: "svg-pelican-01",
		Prompt: "Generate an animated SVG of a pelican riding a bicycle",
		Status: "ok",
		SVG:    &svg,
		Bytes:  int64(len(svg)),
	}}
}

func previewPelicanOutputsForRun(run core.ReportRun) []core.ReportSVGOutput {
	const prompt = "Generate an animated SVG of a pelican riding a bicycle"
	var outputs []core.ReportSVGOutput
	for _, req := range run.Requests {
		if req.CategoryLabel != svgCategoryLabel {
			continue
		}
		if req.State == "completed" {
			svg := previewPelicanSVG
			outputs = append(outputs, core.ReportSVGOutput{
				CaseID: req.CaseID,
				Prompt: prompt,
				Status: "ok",
				SVG:    &svg,
				Bytes:  int64(len(svg)),
			})
		} else {
			outputs = append(outputs, core.ReportSVGOutput{
				CaseID: req.CaseID,
				Prompt: prompt,
				Status: "not_found",
				SVG:    nil,
				Bytes:  0,
			})
		}
	}
	return outputs
}

func (p *previewSession) report(reference string) (*core.ReportIndex, *core.ApiError) {
	if index := p.indexes[reference]; index != nil {
		return index, nil
	}
	if reference == "broken-report" && !p.reportReadFailed {
		p.reportReadFailed = true
		return nil, &core.ApiError{Code: "report_unreadable", Message: l10n.TR("preview.report.read_failed")}
	}
	if reference != "preview-report" && reference != "long-report" && reference != "pelican-report" && reference != "broken-report" {
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
	summary := core.ReportSummary{ReportId: id, Path: "/preview/reports/" + id, ModifiedAt: previewValue(p.timestamp())}
	for _, history := range s.historyRuns() {
		if !slices.Contains(paths, history.Path) {
			continue
		}
		run := previewReportFixture("").Runs[0]
		run.Model, run.Profile.Title = emptyText(history.ModelName), history.ModelName
		run.Profile.Engine.BinaryPath = history.Engine
		run.Source.RunID, run.Facets.BundleLabel = history.RunId, emptyText(history.Bundle)
		run.Diagnostic = true
		if !history.Sealed {
			run.Summary.StopReason = emptyText(history.StopReason)
		}
		if detail := p.details[history.RunId]; detail != nil && detail.Progress != nil {
			applyPreviewProgress(&run, detail, emptyText(history.Bundle), history.Sealed)
			previewReportMetrics(&run, detail)
		}
		if emptyText(history.Bundle) == inferyard.DefaultBundlePelican {
			run.SVGOutputs = previewPelicanOutputsForRun(run)
		}
		index.Runs = append(index.Runs, run)
		summary.RunIds = append(summary.RunIds, history.RunId)
		summary.RunPaths = append(summary.RunPaths, previewValue(history.Path))
		summary.RunSummaries = append(summary.RunSummaries, core.ReportListRun{
			RunID: history.RunId, Model: run.Model, BundleLabel: emptyText(history.Bundle),
			Title: run.Profile.Title, Quality: run.Dashboard.Quality,
		})
	}
	p.indexes[id] = index
	s.reports = append(s.reports, summary)
	return id
}

func applyPreviewProgress(run *core.ReportRun, detail *core.RunTaskDetail, bundle string, sealed bool) {
	p := detail.Progress
	run.Requests = nil
	var completed int64
	for _, item := range p.Cases {
		if item.State == "completed" {
			completed++
		}
		run.Requests = append(run.Requests, core.ReportRequest{CaseID: item.CaseId, CategoryLabel: emptyText(item.Category), State: item.State, Quality: item.QualityState, LatencyMS: item.DurationMs})
	}
	total := p.Done
	if p.Total != nil {
		total = *p.Total
	}
	run.Summary.CompletionRate = core.ReportCounts{Numerator: previewValue(completed), Denominator: previewValue(total), Excluded: previewValue(max(total-p.Done, 0))}
	run.Dashboard.Quality, run.Dashboard.Categories = previewReportQuality(tallyPreviewCases(p.Cases), total, bundle, sealed)
}

func previewReportQuality(score core.RunScore, total int64, bundle string, sealed bool) (core.ReportQuality, []core.ReportCategory) {
	if score.Valid == 0 {
		zero := core.ReportCounts{Numerator: previewValue(int64(0)), Denominator: previewValue(int64(0)), Excluded: previewValue(int64(0))}
		return core.ReportQuality{StudioCounts: zero, Complete: false, Reason: previewValue("quality_incomplete_or_not_applicable")}, []core.ReportCategory{}
	}
	excluded := int64(0)
	if bundle != inferyard.DefaultBundlePelican && total > int64(score.Valid) {
		excluded = total - int64(score.Valid)
	}
	counts := core.ReportCounts{Numerator: previewValue(int64(score.Passed)), Denominator: previewValue(int64(score.Valid)), Excluded: previewValue(excluded)}
	complete := sealed && excluded == 0
	quality := core.ReportQuality{StudioCounts: counts, Complete: complete}
	if !complete {
		quality.Reason = previewValue("quality_incomplete_or_not_applicable")
	}
	return quality, []core.ReportCategory{{ID: "math", Label: l10n.TR("preview.fixture.category"), Rate: counts}}
}

func previewReportMetrics(run *core.ReportRun, detail *core.RunTaskDetail) {
	d := &run.Dashboard
	d.Latency.Min, d.Latency.P50, d.Latency.P95, d.Latency.Max = nil, nil, nil, nil
	var durations []float64
	for _, request := range run.Requests {
		if request.LatencyMS != nil {
			durations = append(durations, *request.LatencyMS)
		}
	}
	d.Latency.SampleCount = previewValue(int64(len(durations)))
	d.Latency.Excluded = previewValue(int64(len(run.Requests) - len(durations)))
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
		if sample.MissingReason != nil || sample.Source.Unit != "bytes" {
			continue
		}
		if v := sample.Value; sample.Source.Metric == "service_rss" && v != nil && (d.SampledPeakRSSBytes == nil || *v > *d.SampledPeakRSSBytes) {
			d.SampledPeakRSSBytes, d.SampledPeakRSSReason = previewValue(*v), nil
		}
		if v := sample.Value; sample.Source.Metric == "system_mem_available" && v != nil && (d.SampledMinimumAvailableBytes == nil || *v < *d.SampledMinimumAvailableBytes) {
			d.SampledMinimumAvailableBytes, d.MinimumAvailableReason = previewValue(*v), nil
		}
	}
}
