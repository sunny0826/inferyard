//go:build ui_debug && !release

package pages

import (
	"fmt"
	"path/filepath"
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

func localizePreviewReportRun(run *core.ReportRun, locale string) {
	catMath := l10n.T(locale, "preview.fixture.cat_math")
	catQA := l10n.T(locale, "preview.fixture.cat_qa")
	catSim := l10n.T(locale, "preview.fixture.category")
	zhMath := l10n.T(l10n.LanguageZhHans, "preview.fixture.cat_math")
	enMath := l10n.T(l10n.LanguageEn, "preview.fixture.cat_math")
	zhQA := l10n.T(l10n.LanguageZhHans, "preview.fixture.cat_qa")
	enQA := l10n.T(l10n.LanguageEn, "preview.fixture.cat_qa")
	zhSim := l10n.T(l10n.LanguageZhHans, "preview.fixture.category")
	enSim := l10n.T(l10n.LanguageEn, "preview.fixture.category")
	for i := range run.Dashboard.Categories {
		switch run.Dashboard.Categories[i].ID {
		case "math":
			if run.Dashboard.Categories[i].Label == zhMath || run.Dashboard.Categories[i].Label == enMath {
				run.Dashboard.Categories[i].Label = catMath
			} else {
				run.Dashboard.Categories[i].Label = catSim
			}
		case "qa":
			run.Dashboard.Categories[i].Label = catQA
		}
	}
	for i := range run.Requests {
		switch run.Requests[i].CategoryLabel {
		case zhMath, enMath:
			run.Requests[i].CategoryLabel = catMath
		case zhQA, enQA:
			run.Requests[i].CategoryLabel = catQA
		case zhSim, enSim:
			run.Requests[i].CategoryLabel = catSim
		}
	}
}

func previewReportSummary(id string) core.ReportSummary {
	index := previewReportFixture(id)
	modified := "2026-10-16T23:59:30Z"
	if id == "compare-report" {
		modified = "2026-10-17T00:05:00Z"
	}
	summary := core.ReportSummary{
		ReportId:   id,
		Path:       "/preview/reports/" + id,
		ModifiedAt: previewValue(modified),
	}
	for _, run := range index.Runs {
		summary.RunIds = append(summary.RunIds, run.Source.RunID)
		if run.Source.Path != "" {
			p := run.Source.Path
			summary.RunPaths = append(summary.RunPaths, &p)
		}
		summary.RunSummaries = append(summary.RunSummaries, core.ReportListRun{
			RunID: run.Source.RunID, Model: run.Model, BundleLabel: run.Facets.BundleLabel,
			Title: run.Profile.Title, Quality: run.Dashboard.Quality,
		})
	}
	return summary
}

func previewReportFixture(reference string) *core.ReportIndex {
	index, err := inferyard.ParseStudioReport(previewReportJSON)
	if err != nil {
		panic("invalid bundled UI preview report: " + err.Error())
	}
	baseRun := &index.Runs[0]
	baseRun.Source.RunID = "preview-run"
	baseRun.Source.Path = "/preview/results/preview-run"
	baseRun.Model = "Preview-Model-Q4.gguf"
	baseRun.Profile.Title = previewValue("Preview-Model-Q4.gguf")
	baseRun.Profile.Machine.CPU = previewValue("Apple M4 Pro")
	localizePreviewReportRun(baseRun, l10n.CurrentLocale())

	switch reference {
	case "pelican-report":
		baseRun.Source.RunID = "pelican-preview-run"
		baseRun.Source.Path = "/preview/results/pelican-preview-run"
		baseRun.Facets.BundleLabel = inferyard.DefaultBundlePelican + " · 1.0.0"
		zero := core.ReportCounts{Numerator: previewValue(int64(0)), Denominator: previewValue(int64(0)), Excluded: previewValue(int64(0))}
		baseRun.Dashboard.Quality = core.ReportQuality{StudioCounts: zero, Complete: false, Reason: previewValue("quality_incomplete_or_not_applicable")}
		baseRun.Dashboard.Categories = []core.ReportCategory{}
		latency := 2480.0
		baseRun.Requests = []core.ReportRequest{{
			CaseID:        "svg-pelican-01",
			CategoryLabel: "svg",
			State:         "completed",
			Quality:       previewValue("not_applicable"),
			LatencyMS:     &latency,
		}}
		baseRun.Summary.CompletionRate = core.ReportCounts{Numerator: previewValue(int64(1)), Denominator: previewValue(int64(1)), Excluded: previewValue(int64(0))}
		baseRun.Dashboard.Latency = core.ReportLatency{
			SampleCount: previewValue(int64(1)),
			Excluded:    previewValue(int64(0)),
			Min:         &latency,
			P50:         &latency,
			Max:         &latency,
		}
		baseRun.SVGOutputs = previewPelicanOutputs()
	case "long-report":
		for i := range 40 {
			baseRun.Requests = append(baseRun.Requests, core.ReportRequest{
				CaseID:        fmt.Sprintf("preview-case-%03d", i),
				CategoryLabel: l10n.TR("preview.fixture.category"),
				State:         "completed",
			})
		}
		baseRun.Dashboard.Latency.Excluded = previewValue(int64(len(baseRun.Requests)) - *baseRun.Dashboard.Latency.SampleCount)
	case "compare-report":
		secondIdx, _ := inferyard.ParseStudioReport(previewReportJSON)
		second := secondIdx.Runs[0]
		second.Source.RunID = "preview-run-small"
		second.Source.Path = "/preview/results/preview-run-small"
		second.Model = "Preview-Small.gguf"
		second.Profile.Title = previewValue("Preview-Small.gguf")
		second.Profile.Engine.BinaryPath = previewValue("/preview/engines/mlx-lm")
		second.Profile.Engine.Adapter = "local_openai_v1"
		second.Profile.Machine.CPU = previewValue("Apple M4 Pro")
		second.Dashboard.Quality = core.ReportQuality{
			StudioCounts: core.ReportCounts{Numerator: previewValue(int64(10)), Denominator: previewValue(int64(13)), Excluded: previewValue(int64(0))},
			Complete:     true,
		}
		second.Dashboard.Throughput.OutputTokensPerSecond = previewValue(54.2)
		second.Dashboard.Latency.P50 = previewValue(615.0)
		localizePreviewReportRun(&second, l10n.CurrentLocale())
		index.Runs = append(index.Runs, second)
	}
	return &index
}

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
	switch reference {
	case "preview-report", "compare-report", "long-report", "pelican-report", "broken-report":
		return previewReportFixture(reference), nil
	default:
		return nil, core.Err("not_found")
	}
}

func (p *previewSession) detail(id string) *core.RunTaskDetail {
	if detail := p.details[id]; detail != nil {
		return detail
	}
	if id == "preview-run" || id == "preview-run-small" {
		detail := previewDetailFixture(id)
		detail.Task.State = "done"
		detail.Task.RunId = previewValue(id)
		detail.Task.RunPath = previewValue("/preview/results/" + id)
		p.details[id] = detail
		return detail
	}
	return nil
}

func (p *previewSession) createReport(paths []string) string {
	s := p.state
	id := p.nextID("report")
	index := &core.ReportIndex{}
	reportsRoot := emptyText(s.settings.Effective.ReportsRoot)
	if reportsRoot == "" {
		reportsRoot = "/preview/reports"
	}
	summary := core.ReportSummary{ReportId: id, Path: filepath.Join(reportsRoot, id), ModifiedAt: previewValue(p.timestamp())}
	for _, history := range s.historyRuns() {
		if !slices.Contains(paths, history.Path) {
			continue
		}
		run := previewReportFixture("").Runs[0]
		run.Model, run.Profile.Title = emptyText(history.ModelName), history.ModelName
		run.Profile.Engine.BinaryPath = history.Engine
		run.Source.RunID, run.Source.Path = history.RunId, history.Path
		run.Facets.BundleLabel = emptyText(history.Bundle)
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
