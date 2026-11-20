package core

import (
	"encoding/json"

	"inferyard/studio/internal/evidence"
)

func qualityExempt(category string) bool {
	return category == "performance" || category == "svg"
}

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

func foldCase(score RunScore, c CaseProgress, categories map[string]string) (RunScore, bool) {
	if c.State != "completed" && c.State != "failed" {
		return score, false
	}
	if category, known := categories[c.CaseId]; known {
		if qualityExempt(category) {
			return score, false
		}
	} else if str(c.QualityState) == "not_applicable" {
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

type frozenCase struct {
	CaseID   string `json:"case_id"`
	Category string `json:"category"`
}

type frozenBundle struct {
	Cases []frozenCase `json:"cases"`
}

type inputsDocuments struct {
	Documents map[string]struct {
		Payload frozenBundle `json:"payload"`
	} `json:"documents"`
}

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
