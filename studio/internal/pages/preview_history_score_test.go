//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

// The history table and report cards read simulated runs by the CLI's quality
// rules: zh-core counts executed cases and earns a pass rate on completion,
// while svg-only zh-svg-pelican stays out of the quality denominator.
func TestPreviewHistoryScoresSimulatedRuns(t *testing.T) {
	p, tt := preparePreview(t)
	p.state.model = "Preview-Small.gguf"
	tt.Frame()
	click(t, tt, "开始测评")
	p.advance(1700 * time.Millisecond)
	click(t, tt, "报告")
	if !tt.HasText("10/10 · 含 本次运行未完整执行全部题目") {
		t.Fatal("running scope must count its executed cases without a rate", tt.Texts())
	}
	p.advance(20 * time.Second)
	tt.Frame()
	if len(p.state.historyRuns()) != 2 || p.state.task.report == "" {
		t.Fatal("simulated workflow did not finish both scopes", len(p.state.historyRuns()))
	}
	assertSimulatedHistoryScores(t, p.state.historyRuns(), tt)
	assertSimulatedReportQuality(t, p, tt)
}

func assertSimulatedHistoryScores(t *testing.T, runs []core.RunSummary, tt *ui.Tester) {
	t.Helper()
	for _, run := range runs {
		switch emptyText(run.Bundle) {
		case inferyard.DefaultBundleCore:
			if run.Score == nil || runScore(run) != "120/120 · 100.0%" {
				t.Errorf("zh-core history score = %q, want 120/120 · 100.0%%", runScore(run))
			}
		case inferyard.DefaultBundlePelican:
			if run.Score != nil || runScore(run) != termNoScore {
				t.Errorf("zh-svg-pelican history score = %+v (%q), want nil (%q)", run.Score, runScore(run), termNoScore)
			}
		}
	}
	if !tt.HasText("120/120 · 100.0%") || !tt.HasText(termNoScore) {
		t.Errorf("reports page did not show zh-core rate and pelican %q: %v", termNoScore, tt.Texts())
	}
}

func assertSimulatedReportQuality(t *testing.T, p *previewSession, tt *ui.Tester) {
	t.Helper()
	for _, want := range []string{
		"Preview-Small.gguf · zh-core · 答案通过 120 / 120",
		"Preview-Small.gguf · zh-svg-pelican · 答案通过 0 / 0（质量结论不完整）",
	} {
		if !tt.HasText(want) {
			t.Errorf("report card missing %q: %v", want, tt.Texts())
		}
	}
	index, err := p.report(p.state.task.report)
	if err != nil || len(index.Runs) != 2 {
		t.Fatalf("generated report missing both runs: %v", err)
	}
	pelican := index.Runs[1]
	if emptyText(pelican.Dashboard.Quality.Reason) != "quality_incomplete_or_not_applicable" || pelican.Dashboard.Quality.Complete || len(pelican.Dashboard.Categories) != 0 {
		t.Errorf("pelican dashboard quality = %+v categories=%v", pelican.Dashboard.Quality, pelican.Dashboard.Categories)
	}
	if *pelican.Summary.CompletionRate.Numerator != 1 || *pelican.Summary.CompletionRate.Denominator != 1 {
		t.Errorf("pelican completion rate = %s, want 1 / 1", reportRate(pelican.Summary.CompletionRate))
	}
}

// Quality-exempt categories (svg, performance) and untyped not_applicable
// verdicts stay out of the preview quality denominator whether completed or failed.
func TestTallyPreviewCasesSkipsQualityExemptCategories(t *testing.T) {
	for _, category := range []string{"svg", "performance"} {
		t.Run(category, func(t *testing.T) {
			got := tallyPreviewCases([]core.CaseProgress{
				{CaseId: "c1", State: "completed", Category: previewValue(category), QualityState: previewValue("not_applicable")},
				{CaseId: "c2", State: "failed", Category: previewValue(category)},
			})
			if got != (core.RunScore{}) {
				t.Fatalf("%s cases entered quality denominator: %+v", category, got)
			}
		})
	}
	got := tallyPreviewCases([]core.CaseProgress{
		{CaseId: "legacy", State: "completed", QualityState: previewValue("not_applicable")},
		{CaseId: "qa-pass", State: "completed", Category: previewValue("qa"), QualityState: previewValue("pass")},
		{CaseId: "qa-fail", State: "failed", Category: previewValue("qa")},
	})
	if got.Valid != 2 || got.Passed != 1 || got.Failed != 1 {
		t.Fatalf("mixed cases tallied as %+v, want Valid=2 Passed=1 Failed=1", got)
	}
}

// Entering the running scene reuses the seeded run id: its history row must
// immediately reflect the 42 in-flight cases without inheriting the seeded
// pass rate, keep withholding the rate as ticks advance, and still withhold it
// if the run is cancelled.
func TestPreviewRunningSceneScoresHistoryWithoutRate(t *testing.T) {
	p := newPreviewSession(context.Background())
	p.loadScene(previewSceneIndex(t, "任务 · 运行中"))
	tt := previewTester(p, 1120, 1200)
	click(t, tt, "报告")
	if !tt.HasText("42/42 · 含 本次运行未完整执行全部题目") {
		t.Fatal("running scene inherited the seeded complete score", tt.Texts())
	}
	p.advance(500 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("52/52 · 含 本次运行未完整执行全部题目") {
		t.Fatal("running tick showed a pass rate before completion", tt.Texts())
	}
	click(t, tt, "任务")
	confirmLabels(t, tt, "取消运行", "确认取消运行")
	p.advance(500 * time.Millisecond)
	click(t, tt, "报告")
	if !tt.HasText("已取消") || !tt.HasText("52/52 · 含 本次运行未完整执行全部题目") {
		t.Fatal("cancelled scene run did not keep its incomplete score", tt.Texts())
	}
}
