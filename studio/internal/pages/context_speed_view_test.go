package pages

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func TestSpeedReasonTranslations(t *testing.T) {
	codes := strings.Fields(`five_valid_samples_required cohort_unknown cohort_mismatch
		aggregate_context_mismatch aggregate_prompt_length_mismatch request_not_completed
		sealed_run_required request_missing identity_evidence_missing token_budget_evidence_missing
		request_evidence_missing fixed_output_evidence_missing context_unverified actual_input_unknown
		context_budget_exceeded final_timing_missing final_timing_unqualified cold_cache_unproven
		input_token_mismatch fixed_output_not_reached metric_missing invalid_engine_timings not_executed unknown
		engine_build_unverified diagnostic_not_formal_measurement fixed_output_unverified identity_unverified
		seed_support_unverified generation_parameters_unverified evidence_missing final_engine_timings_missing
		ambiguous_engine_timings engine_timing_definition_unknown speculative_timing_scope_unverified
		no_uncached_prompt_tokens no_decode_steps_after_first_token nonpositive_engine_duration`)
	for _, loc := range l10n.SupportedLanguages() {
		for _, code := range codes {
			_, lang, found := l10n.Catalog().Message(loc, "speed.reason."+code)
			text := humanSpeedReason(loc, code)
			if !found || lang != loc || text == "" || text == code || strings.Contains(text, "_") {
				t.Errorf("%s translation of %s = %q (catalog locale %s)", loc, code, text, lang)
			}
		}
		if got := humanSpeedReason(loc, "future_reason"); got != "future_reason" {
			t.Errorf("%s fallback = %q", loc, got)
		}
	}
}

func TestSpeedDiagnosticsSwitchLanguageWhileOpen(t *testing.T) {
	result := inferyard.ContextSpeedResult{Samples: make([]inferyard.SpeedSample, 5)}
	result.Measurement.Reasons = []string{"five_valid_samples_required"}
	last := &result.Samples[4]
	last.Columns.Time.MissingReason = ptr("final_timing_missing")
	last.Measurement.Reasons = []string{"final_timing_missing", "metric_missing"}
	task := &speedTask{id: "speed-gap", stage: speedDone}
	applySpeedResult(task, result)
	s := viewState{speed: &speedWorkspace{opened: task}}
	tt := ui.NewTester(s.speedResultView, 1200, 900)
	tt.SetLocale(l10n.LanguageZhHans)
	tt.Frame()
	click(t, tt, l10n.T(l10n.LanguageZhHans, "speed.result.diagnostics_fold"))
	for _, tc := range []struct{ loc, colon string }{{l10n.LanguageZhHans, "："}, {l10n.LanguageEn, ": "}} {
		tt.SetLocale(tc.loc)
		tt.Frame()
		why := humanSpeedReason(tc.loc, "final_timing_missing")
		detail := l10n.T(tc.loc, "speed.metric.input_time") + tc.colon + why
		for _, text := range []string{
			humanSpeedReason(tc.loc, "five_valid_samples_required"),
			l10n.Tf(tc.loc, "speed.result.sample_missing", "index", 5, "detail", detail),
			l10n.Tf(tc.loc, "speed.result.sample_reason", "index", 5, "reason", why+l10n.T(tc.loc, "speed.list_sep")+humanSpeedReason(tc.loc, "metric_missing")),
		} {
			if !tt.HasText(text) {
				t.Errorf("%s missing %q in %v", tc.loc, text, tt.Texts())
			}
		}
		if tt.HasText("final_timing_missing") || tt.HasText("five_valid_samples_required") {
			t.Errorf("%s exposed raw diagnoses: %v", tc.loc, tt.Texts())
		}
	}
}

func TestSpeedResultSampleRowsFit(t *testing.T) {
	for _, summary := range []bool{false, true} {
		t.Run(fmt.Sprintf("summary=%t", summary), func(t *testing.T) {
			task := &speedTask{model: "Long context model with a wrapped display name", engine: "Engine release", samples: make([]speedSample, 5)}
			w := &speedWorkspace{opened: task}
			state := &w.table
			if summary {
				task.summary = &speedSample{}
				state = &w.samples
			}
			s := viewState{speed: w}
			tt := ui.NewTester(s.speedResultView, 1200, 900)
			tt.SetLocale(l10n.LanguageEn)
			tt.Frame()
			if summary {
				if !tt.HasText(l10n.T(l10n.LanguageEn, "speed.col.model_engine")) {
					t.Fatal("summary lacks the model and engine column")
				}
				engine, ok := tt.Find(task.engine)
				if !ok || engine.H <= 0 {
					t.Fatal("summary lacks the engine name below the model")
				}
				tt.ClickAt(engine.X+engine.W/2, engine.Y+engine.H-1)
				if w.table.Selected == nil || *w.table.Selected != 0 {
					t.Fatal("engine name at the bottom of the summary is clipped")
				}
				click(t, tt, l10n.T(l10n.LanguageEn, "speed.result.samples_fold"))
				tt.Advance(500 * time.Millisecond)
				tt.Frame()
			}
			if !tt.HasText(l10n.T(l10n.LanguageEn, "speed.col.sample")) {
				t.Fatal("individual measurements lack the sample column")
			}
			var last ui.Rect
			for i := 1; i <= 5; i++ {
				box, ok := tt.Find(fmt.Sprintf("#%d", i))
				if !ok || box.H <= 0 || box.Y < last.Y+last.H {
					t.Fatalf("sample %d bounds %v after %v", i, box, last)
				}
				last = box
			}
			row, ok := tt.Find(l10n.Tf(l10n.LanguageEn, "speed.table.row_label", "index", 5))
			if !ok || row.H <= 0 {
				t.Fatal("missing fifth row")
			}
			tt.ClickAt(last.X+last.W/2, row.Y+row.H-1)
			if state.Selected == nil || *state.Selected != 4 {
				t.Fatal("bottom edge of fifth row is not clickable without scrolling")
			}
		})
	}
}

func TestSpeedResultHeaderBackToHistory(t *testing.T) {
	w := &speedWorkspace{opened: &speedTask{}, readError: "read failed", reading: true}
	s := viewState{selected: "reports", loaded: true, speed: w}
	tt := ui.NewTester(s.view, 1200, 600)
	tt.SetLocale(l10n.LanguageEn)
	tt.Frame()
	backLabel := l10n.T(l10n.LanguageEn, "speed.action.back_history")
	back, ok := tt.AccessByRole(ui.RoleButton, backLabel)
	combo, hasCombo := tt.AccessByRole(ui.RoleButton, l10n.T(l10n.LanguageEn, "speed.action.new_with_combo"))
	title, hasTitle := tt.Find(l10n.T(l10n.LanguageEn, "speed.result.title"))
	if !ok || !hasCombo || !hasTitle || back.Bounds.X+back.Bounds.W > 205 || back.Bounds.X+back.Bounds.W > title.X || title.X+title.W > combo.Bounds.X {
		t.Fatalf("result header bounds: title=%v combo=%v back=%v", title, combo.Bounds, back.Bounds)
	}
	click(t, tt, backLabel)
	if w.opened != nil || w.reading || w.readError != "" || s.reportsTab != reportsTabSpeed {
		t.Fatal("back did not close the result and restore speed history")
	}
}
