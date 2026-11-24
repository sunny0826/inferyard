package pages

import "github.com/egoist/mygo/ui"

func termBenchmarkStageText(c *ui.Context) string  { return tr(c, "common.term.benchmark_stage") }
func termStartBenchmarkText(c *ui.Context) string  { return tr(c, "common.term.start_benchmark") }
func termCandidateEngineText(c *ui.Context) string { return tr(c, "common.term.candidate_engine") }
func termBundleText(c *ui.Context) string          { return tr(c, "common.term.bundle") }
func termSealedText(c *ui.Context) string          { return tr(c, "common.term.sealed") }
