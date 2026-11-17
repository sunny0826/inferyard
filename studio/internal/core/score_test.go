package core

import (
	"fmt"
	"testing"
)

// runScoreCases reduces case qualities the same way the event aggregate does,
// without needing a run directory.
func runScoreCases(qualities []string) (RunScore, bool) {
	cases := make([]CaseProgress, len(qualities))
	for i, quality := range qualities {
		cases[i] = CaseProgress{CaseId: fmt.Sprintf("case-%d", i), State: "completed"}
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
	score, ok := runScoreCases([]string{"pass", "pass", "pass", "unscorable"})
	if !ok || score.Passed != 3 || score.Valid != 4 || score.Unscorable != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil {
		t.Fatalf("unscorable case still produced rate %v", *score.Rate)
	}
}

// A fully judged run keeps the ordinary ratio, and failures stay in it.
func TestRunScoreCountsFailuresAndKeepsRate(t *testing.T) {
	score, ok := runScoreCases([]string{"pass", "fail", "pass", "fail"})
	if !ok || score.Passed != 2 || score.Valid != 4 || score.Unscorable != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 0.5 {
		t.Fatalf("rate = %v", score.Rate)
	}
}

// Not-applicable categories are outside quality and do not enter any count.
func TestRunScoreIgnoresNotApplicable(t *testing.T) {
	score, ok := runScoreCases([]string{"pass", "not_applicable"})
	if !ok || score.Passed != 1 || score.Valid != 1 || score.Unscorable != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
}

// Without any quality observation there is no score at all, rather than zeros.
func TestRunScoreWithoutObservationsIsAbsent(t *testing.T) {
	if _, ok := runScoreCases([]string{"", ""}); ok {
		t.Fatal("unobserved quality produced a score")
	}
}
