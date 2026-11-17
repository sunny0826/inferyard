//go:build ui_debug && !release

package pages

import (
	"context"
	"testing"
	"time"
)

// The history table reads a simulated run's score the way it reads a real one:
// executed cases count while the run is still going, with the rate withheld,
// and a finished run shows every case passed with its rate. A run with a
// generated report must never read as unscored.
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
	scored := 0
	for _, text := range tt.Texts() {
		if text == "120/120 · 100.0%" {
			scored++
		}
	}
	if scored != 2 || tt.HasText(termNoScore) {
		t.Fatal("finished scopes must show their scores", tt.Texts())
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
