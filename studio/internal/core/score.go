package core

import (
	"encoding/json"

	"inferyard/studio/internal/evidence"
)

// The quality reduction below follows the CLI's own rules, so Studio never
// reports a rate the CLI would refuse to compute.
//
//	cli/src/inferyard/analysis/quality.py
//	  - performance and svg cases are skipped before any group exists.
//	  - an execution state other than completed/failed is "excluded" and makes
//	    the run incomplete.
//	  - completed and failed both occupy the denominator (valid += 1).
//	  - only a completed case can carry a score; an execution failure counts as
//	    not passed without a score of its own.
//	  - a completed case without a score, or with an unscorable score, stays in
//	    the denominator and makes the run incomplete.
//	  - rate() gives a value only when no reason applies.
//	cli/src/inferyard/analysis/trial_summary.py
//	  - completeness requires plan_finished, every request in completed/failed,
//	    no truncation and every quality_state in pass/fail/not_applicable.

// qualityExempt reports whether the CLI keeps a category out of every quality
// denominator. It is decided by the frozen category, never by a score: a failed
// execution produces no score, so reading the category off the score would
// count it.
func qualityExempt(category string) bool {
	return category == "performance" || category == "svg"
}

// runScore reduces executed cases to the shared score. categories maps the
// frozen bundle's case ids to their categories; complete reports whether the
// run is complete by the CLI rule. A nil result means nothing executed.
func runScore(cases []CaseProgress, categories map[string]string, complete bool) (RunScore, bool) {
	score, counted := RunScore{}, false
	for _, c := range cases {
		if folded, counts := foldCase(score, c, categories); counts {
			score, counted = folded, true
		}
	}
	if !counted {
		return RunScore{}, false
	}
	if complete && score.Valid > 0 && score.Unscorable == 0 && score.Missing == 0 {
		score.Rate = ptr(float64(score.Passed) / float64(score.Valid))
	}
	return score, true
}

// foldCase adds one case's slot. Cases outside the quality categories, and
// cases that never executed, stay out of every count; an executed case always
// takes a slot, whether it passed, failed to answer, or failed to score.
func foldCase(score RunScore, c CaseProgress, categories map[string]string) (RunScore, bool) {
	if c.State != "completed" && c.State != "failed" {
		return score, false
	}
	if category, known := categories[c.CaseId]; known {
		if qualityExempt(category) {
			return score, false
		}
	} else if str(c.QualityState) == "not_applicable" {
		// Without a frozen category the score's own verdict is the only
		// evidence; it is how exempt cases were recognised before the bundle
		// was read.
		return score, false
	}
	score.Valid++
	if c.State == "failed" {
		score.Failed++
		return score, true
	}
	switch str(c.QualityState) {
	case "pass":
		score.Passed++
	case "unscorable":
		score.Unscorable++
	case "fail":
	default:
		score.Missing++
	}
	return score, true
}

// frozenCase is the part of one frozen bundle case the reduction reads.
type frozenCase struct {
	CaseID   string `json:"case_id"`
	Category string `json:"category"`
}

// frozenBundle is the bundle document as the reduction reads it.
type frozenBundle struct {
	Cases []frozenCase `json:"cases"`
}

// inputsDocuments is the envelope part that carries the frozen documents.
type inputsDocuments struct {
	Documents map[string]struct {
		Payload frozenBundle `json:"payload"`
	} `json:"documents"`
}

// caseCategories maps the frozen bundle's case ids to their categories. The
// bundle document is part of the run's immutable inputs, so the mapping cannot
// drift from the plan the run was started with.
func caseCategories(inputs []byte) map[string]string {
	categories := map[string]string{}
	var envelope inputsDocuments
	if json.Unmarshal(inputs, &envelope) != nil {
		return categories
	}
	for _, c := range envelope.Documents["bundle"].Payload.Cases {
		if c.CaseID != "" {
			categories[c.CaseID] = c.Category
		}
	}
	return categories
}

// runComplete is the CLI's completeness rule (trial_summary.py): the plan must
// have finished, every planned case must have executed, the run must be sealed
// and be an ordinary initial run, and every case must carry a final quality
// verdict. A failed execution is a verdict — the CLI records it as "fail" —
// while a completed case without a score, or an unscorable one, is not.
func runComplete(progress RunProgress, r evidence.Run, run object, stop *string) bool {
	if str(stop) != "plan_finished" {
		return false
	}
	if str(text(run["kind"])) != "run" || run["diagnostic"] == true || str(text(run["relation"])) == "resume" {
		return false
	}
	if r.State != evidence.Sealed || progress.Total == nil || progress.Done != *progress.Total {
		return false
	}
	for _, c := range progress.Cases {
		if !caseVerdictFinal(c) {
			return false
		}
	}
	return true
}

// caseVerdictFinal reports whether one case reached a terminal quality verdict
// under the CLI rule, including the "failed execution counts as fail" mapping
// from event_ledger.py.
func caseVerdictFinal(c CaseProgress) bool {
	switch c.State {
	case "failed":
		return true
	case "completed":
		quality := str(c.QualityState)
		return quality == "pass" || quality == "fail" || quality == "not_applicable"
	}
	return false
}
