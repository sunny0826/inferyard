package core

import (
	"fmt"
	"testing"
)

// scoreCase is one synthetic case for the reduction table. Category is the
// frozen bundle category; leaving it empty exercises the fallback to the
// score's own verdict.
type scoreCase struct {
	id, state, quality, category string
}

// reduceCases builds the case list and the frozen category map the reduction
// needs, then calls the same function the aggregate uses.
func reduceCases(t *testing.T, complete bool, cases ...scoreCase) (RunScore, bool) {
	t.Helper()
	progress := make([]CaseProgress, len(cases))
	categories := map[string]string{}
	for i, c := range cases {
		id := c.id
		if id == "" {
			id = fmt.Sprintf("case-%d", i)
		}
		progress[i] = CaseProgress{CaseId: id, State: c.state}
		if c.state == "" {
			progress[i].State = "completed"
		}
		if c.quality != "" {
			progress[i].QualityState = ptr(c.quality)
		}
		if c.category != "" {
			categories[id] = c.category
		}
	}
	return runScore(progress, categories, complete)
}

// A complete run with failures keeps its rate: the failed case occupies its
// slot and counts as not passed, exactly as the CLI's event ledger maps a
// failed execution to quality_state "fail".
func TestRunScoreCountsFailedExecutionAsNotPassed(t *testing.T) {
	score, ok := reduceCases(t, true,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "failed", category: "qa"},
	)
	if !ok || score.Passed != 3 || score.Valid != 4 || score.Failed != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 0.75 {
		t.Fatalf("rate = %v, want 0.75", score.Rate)
	}
	if score.RateGap() != "" {
		t.Fatalf("failed execution withheld the rate: %q", score.RateGap())
	}
}

// A failed case in an exempt category never enters the denominator, because the
// category comes from the frozen bundle and not from a score the failure never
// produced.
func TestRunScoreExcludesExemptCategoriesByFrozenCategory(t *testing.T) {
	score, ok := reduceCases(t, true,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "failed", category: "svg"},
		scoreCase{state: "failed", category: "performance"},
	)
	if !ok || score.Passed != 1 || score.Valid != 1 || score.Failed != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate == nil || *score.Rate != 1 {
		t.Fatalf("rate = %v", score.Rate)
	}
}

// Without a frozen category the score's own verdict is the only evidence;
// not_applicable still means "outside quality".
func TestRunScoreFallsBackToNotApplicable(t *testing.T) {
	score, ok := reduceCases(t, true,
		scoreCase{state: "completed", quality: "pass"},
		scoreCase{state: "completed", quality: "not_applicable"},
	)
	if !ok || score.Passed != 1 || score.Valid != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
}

// An unscorable case keeps its slot and withholds the rate.
func TestRunScoreWithholdsRateForUnscorable(t *testing.T) {
	score, ok := reduceCases(t, true,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "completed", quality: "unscorable", category: "qa"},
	)
	if !ok || score.Passed != 1 || score.Valid != 2 || score.Unscorable != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil || score.RateGap() != "不可评分" {
		t.Fatalf("rate = %v gap = %q", score.Rate, score.RateGap())
	}
}

// A completed case whose score is missing keeps its slot and withholds the rate.
func TestRunScoreWithholdsRateForMissingScore(t *testing.T) {
	score, ok := reduceCases(t, true,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "completed", category: "qa"},
	)
	if !ok || score.Passed != 1 || score.Valid != 2 || score.Missing != 1 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil || score.RateGap() != "缺评分" {
		t.Fatalf("rate = %v gap = %q", score.Rate, score.RateGap())
	}
}

// An incomplete run withholds the rate even when every counted case passed,
// because the CLI reports the quality section as incomplete.
func TestRunScoreWithholdsRateForIncompleteRun(t *testing.T) {
	score, ok := reduceCases(t, false,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "completed", quality: "pass", category: "qa"},
	)
	if !ok || score.Passed != 2 || score.Valid != 2 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
	if score.Rate != nil || score.RateGap() != "运行不完整" {
		t.Fatalf("rate = %v gap = %q", score.Rate, score.RateGap())
	}
}

// Cases that never executed stay out of every count.
func TestRunScoreIgnoresUnexecuted(t *testing.T) {
	score, ok := reduceCases(t, false,
		scoreCase{state: "completed", quality: "pass", category: "qa"},
		scoreCase{state: "cancelled", category: "qa"},
		scoreCase{state: "invalid", category: "qa"},
		scoreCase{state: "not_executed", category: "qa"},
	)
	if !ok || score.Passed != 1 || score.Valid != 1 || score.Failed != 0 {
		t.Fatalf("score = %+v ok=%v", score, ok)
	}
}

// Without any executed case there is no score at all, rather than zeros.
func TestRunScoreWithoutExecutedCasesIsAbsent(t *testing.T) {
	if _, ok := reduceCases(t, false, scoreCase{state: "cancelled", category: "qa"}); ok {
		t.Fatal("unexecuted cases produced a score")
	}
}
