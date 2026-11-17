package core

import (
	"fmt"
	"testing"
)

// runScoreCases reduces one case per quality/state pair the same way the event
// aggregate does, without needing a run directory. States default to completed
// so quality-only entries also exercise the execution path.
func runScoreCases(pairs [][2]string) (RunScore, bool) {
	cases := make([]CaseProgress, len(pairs))
	for i, pair := range pairs {
		state, quality := pair[0], pair[1]
		if state == "" {
			state = "completed"
		}
		cases[i] = CaseProgress{CaseId: fmt.Sprintf("case-%d", i), State: state}
		if quality != "" {
			cases[i].QualityState = ptr(quality)
		}
	}
	return runScore(cases)
}

// An unscorable case counts as a valid case, so the denominator does not
// shrink, and the rate stays missing instead of reporting a smaller ratio.
// Three passes and one unscorable case therefore read 3 / 4 without a rate.
func TestRunScoreKeepsUnscorableCasesInDenominator(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"", "pass"}, {"", "pass"}, {"", "unscorable"}})
	if !ok || score.Passed != 3 || score.Valid != 4 || score.Unscorable != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("unscorable case still produced rate %v", *score.Rate)
	}
	if score.RateGap() != "不可评分" {
		t.Fatalf("gap = %q", score.RateGap())
	}
}

// A fully judged run keeps the ordinary ratio, and failures stay in it.
func TestRunScoreCountsFailuresAndKeepsRate(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"", "fail"}, {"", "pass"}, {"", "fail"}})
	if !ok || score.Passed != 2 || score.Valid != 4 || score.Unscorable != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 0.5 {
		t.Fatalf("rate = %v", score.Rate)
	}
}

// Not-applicable categories are outside quality and do not enter any count.
func TestRunScoreIgnoresNotApplicable(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"", "not_applicable"}})
	if !ok || score.Passed != 1 || score.Valid != 1 || score.Unscorable != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
}

// A case that failed to execute still occupies its slot in the denominator:
// three passes and one failed case read 3 / 4 without a rate, never 100%.
func TestRunScoreKeepsFailedExecutionInDenominator(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"", "pass"}, {"", "pass"}, {"failed", ""}})
	if !ok || score.Passed != 3 || score.Valid != 4 || score.Failed != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("failed execution still produced rate %v", *score.Rate)
	}
	if score.RateGap() != "执行失败" {
		t.Fatalf("gap = %q", score.RateGap())
	}
}

// A completed case with a missing score is counted but gaps the rate.
func TestRunScoreKeepsMissingScoreInDenominator(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"", ""}})
	if !ok || score.Passed != 1 || score.Valid != 2 || score.Missing != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("missing score still produced rate %v", *score.Rate)
	}
	if score.RateGap() != "缺评分" {
		t.Fatalf("gap = %q", score.RateGap())
	}
}

// A case that never executed stays out of the score entirely.
func TestRunScoreIgnoresNotExecuted(t *testing.T) {
	score, ok := runScoreCases([][2]string{{"", "pass"}, {"pending", ""}, {"cancelled", ""}})
	if !ok || score.Passed != 1 || score.Valid != 1 || score.Failed != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 1 {
		t.Fatalf("rate = %v", score.Rate)
	}
}

// Without any executed case there is no score at all, rather than zeros.
func TestRunScoreWithoutExecutedCasesIsAbsent(t *testing.T) {
	if _, ok := runScoreCases([][2]string{{"pending", ""}, {"cancelled", ""}}); ok {
		t.Fatal("unexecuted cases produced a score")
	}
}
