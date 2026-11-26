package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func (s *viewState) speedResultView(c *ui.Context) {
	w, r := s.speed, s.speed.opened
	s.header(c, tr(c, "speed.result.title"))
	ui.Text(c, r.model+" · "+r.engine+" · "+inferyard.ContextSpeedBundleID)
	if s.preview != nil {
		ui.Text(c, trf(c, "speed.result.meta_preview", "id", r.id, "status", r.label())).TextColor(stateColor(c, stateWarning))
	} else {
		ui.Text(c, r.id+" · "+r.label()).TextColor(stateColor(c, stateWarning))
	}
	ui.Row(c).Wrap().Gap(tokens(c).Density.Row).Children(func() {
		button(c, tr(c, "speed.action.back_history")).OnClick(func() {
			w.opened, w.readError, w.reading = nil, "", false
			s.reportsTab = reportsTabSpeed
		})
		button(c, tr(c, "speed.action.new_with_combo")).Disabled(s.busy).OnClick(func() { s.newSpeedTask(r) })
	})
	if w.reading {
		host := ui.Column(c.Key("speed-result-loading-" + r.id)).FillWidth()
		show := delayedVisible(c, host, "speed-read", true)
		host.Children(func() {
			if show {
				skeletonCard(c, tr(c, "speed.result.card_title"), 3, tr(c, "speed.result.loading_label"))
			}
		})
		return
	}
	if w.readError != "" {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateDanger,
			Message: w.readError,
			Actions: func() {
				primaryButton(c, tr(c, "speed.action.retry_read")).OnClick(func() { s.openSpeed(r.id) })
			},
		})
		return
	}
	if r.stage == speedCancelled {
		infoBanner(c, tr(c, "speed.result.cancelled_notice"))
	} else if r.summary == nil {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Size:    bannerCompact,
			Message: tr(c, "speed.result.incomplete_notice"),
		})
	}
	card(c, tr(c, "speed.result.card_title"), func() {
		if r.summary != nil {
			ui.Text(c, tr(c, "speed.result.valid_median"))
			if r.result != nil && r.result.Summary != nil {
				v := r.result.Summary.Columns
				ui.Text(c, trf(c, "speed.result.range",
					"time_min", fmt.Sprintf("%.2f", v.Time.Min),
					"time_max", fmt.Sprintf("%.2f", v.Time.Max),
					"prefill_min", fmt.Sprintf("%.2f", v.Prefill.Min),
					"prefill_max", fmt.Sprintf("%.2f", v.Prefill.Max),
					"decode_min", fmt.Sprintf("%.2f", v.Decode.Min),
					"decode_max", fmt.Sprintf("%.2f", v.Decode.Max),
				))
			}
			speedTable(c.Key("speed-summary"), &w.table, tr(c, "speed.table.summary_label"), r, []speedSample{*r.summary})
		} else {
			ui.Text(c, trf(c, "speed.result.valid_partial", "valid", r.validCount()))
			if len(r.samples) > 0 {
				speedTable(c.Key("speed-partial"), &w.table, tr(c, "speed.table.partial_label"), r, r.samples)
			}
		}
	})
	speedResultFolds(c, w, r)
}

func speedResultFolds(c *ui.Context, w *speedWorkspace, r *speedTask) {
	if r.summary == nil || r.reason != "" {
		componentFold(c.Key(r.id), tr(c, "speed.result.diagnostics_fold"), func() {
			if r.reason != "" {
				ui.Text(c, r.reason).TextColor(stateColor(c, stateWarning))
			}
			for i, sample := range r.samples {
				if sample.missing != "" {
					ui.Text(c, trf(c, "speed.result.sample_missing", "index", i+1, "detail", sample.missing)).TextColor(stateColor(c, stateWarning))
				}
				if sample.reason != "" {
					ui.Text(c, trf(c, "speed.result.sample_reason", "index", i+1, "reason", sample.reason)).TextColor(stateColor(c, stateWarning))
				}
			}
		})
	}
	if r.summary != nil {
		componentFold(c, tr(c, "speed.result.samples_fold"), func() {
			speedTable(c.Key("speed-samples"), &w.samples, tr(c, "speed.table.samples_label"), r, r.samples)
		})
	}
}

func speedTable(c *ui.Context, state *pageTableState, label string, task *speedTask, rows []speedSample) {
	columns := []ui.TableColumn{
		{Title: tr(c, "speed.col.model_engine"), MinWidth: c.Theme().Rem(14)},
		{Title: tr(c, "speed.col.context_tokens"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.prompt_tokens"), MinWidth: c.Theme().Rem(11)},
		{Title: tr(c, "speed.col.input_time"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.prefill"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.decode"), MinWidth: c.Theme().Rem(9)},
	}
	loc := contextLocale(c)
	selectableTable(state, func(i int) any { return i }, func(i int) string { return l10n.Tf(loc, "speed.table.row_label", "index", i+1) })
	pageTable(c, state, label, columns, len(rows), func(row, col int) {
		v := rows[row]
		prompt := "—"
		if v.prompt != nil {
			prompt = fmt.Sprint(*v.prompt)
		}
		if col == 0 {
			speedIdentity(c, task)
			return
		}
		context := "—"
		if v.context != nil {
			context = speedContext(*v.context)
		}
		ui.Text(c, []string{"", context, prompt, speedNumber(v.seconds), speedNumber(v.prefill), speedNumber(v.decode)}[col]).SingleLine()
	}).Height(c.Theme().Rem(4)*float32(len(rows)+1) + 2)
}

func speedContext(tokens int) string {
	if tokens <= 0 {
		return "—"
	}
	return fmt.Sprintf("%gk", float64(tokens)/1024)
}

func speedNumber(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.2f", *v)
}

func (s *viewState) speedHistoryView(c *ui.Context) {
	if s.speed == nil || len(s.speed.history) == 0 {
		return
	}
	w := s.speed
	card(c, tr(c, "speed.history.card_title"), func() {
		selectableTable(&w.historyTable, func(i int) any { return w.history[i].id }, func(i int) string { return w.history[i].id })
		pageTable(c.Key("speed-history"), &w.historyTable, tr(c, "speed.history.table_label"), []ui.TableColumn{
			{Title: tr(c, "speed.history.col_task"), MinWidth: c.Theme().Rem(11)}, {Title: tr(c, "speed.col.model_engine"), MinWidth: c.Theme().Rem(24)},
			{Title: tr(c, "speed.history.col_context"), MinWidth: c.Theme().Rem(6)}, {Title: tr(c, "speed.history.col_status"), MinWidth: c.Theme().Rem(6)},
		}, len(w.history), func(row, col int) {
			r := w.history[row]
			if col == 1 {
				speedIdentity(c, r)
				return
			}
			ui.Text(c, []string{r.id, "", speedContext(r.context), r.label()}[col]).SingleLine()
		}).Height(c.Theme().Rem(4)*float32(min(len(w.history), 5)+1) + 2)
		if i := selectedRow(&w.historyTable, len(w.history)); i >= 0 {
			if primaryButton(c, tr(c, "speed.history.open_result")).Clicked() || w.historyTable.submitted {
				s.openSpeed(w.history[i].id)
			}
		}
	})
}

func speedIdentity(c *ui.Context, task *speedTask) {
	ui.Column(c).Gap(tokens(c).Density.Tight).Children(func() {
		ui.Text(c, task.model)
		ui.Text(c, task.engine).TextColor(c.Theme().TextMuted)
	})
}
