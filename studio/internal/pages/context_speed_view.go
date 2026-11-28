package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/inferyard"
	"inferyard/studio/internal/l10n"
)

func speedSampleSlots(c *ui.Context, r *speedTask) {
	t := tokens(c)
	ui.Grid(c).FillWidth().Columns(5).Gap(t.Density.Row).Children(func() {
		for idx := range 5 {
			slotLabel := fmt.Sprintf("#%d", idx+1)
			done := idx < r.completed
			active := idx == r.completed && r.stage == speedMeasuring
			bg := t.Color.Sunken
			border := t.Color.BorderSubtle
			fg := t.Color.FgTertiary
			if done {
				bg = t.Color.Success.SubtleBg
				border = t.Color.Success.Border
				fg = t.Color.Success.Fg
			} else if active {
				bg = t.Color.Active.SubtleBg
				border = t.Color.Active.Border
				fg = t.Color.Active.Fg
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(t.Density.Tight).
				Padding(t.Density.Row).Radius(t.Radius.Control).Background(bg).Border(1, border).Children(func() {
				if done {
					components.Icon(c, reicon.Check).Size(12, 12).TextColor(fg)
				} else if active {
					ui.Box(c).Size(6, 6).Radius(t.Radius.Full).Background(t.Color.Active.Solid)
				}
				txt := ui.Text(c, slotLabel).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(fg).SingleLine()
				if done || active {
					txt.Bold()
				}
			})
		}
	})
}

func speedStageKind(stage speedStage) stateKind {
	switch stage {
	case speedDone:
		return stateSuccess
	case speedFailed:
		return stateDanger
	case speedCancelled, speedPartial:
		return stateWarning
	case speedPreparing, speedBinding, speedPlanning, speedMeasuring, speedReading, speedCancelling:
		return stateActive
	default:
		return stateNeutral
	}
}

func (s *viewState) speedResultView(c *ui.Context) {
	t := tokens(c)
	w, r := s.speed, s.speed.opened
	s.subpageHeader(c, tr(c, "speed.result.title"), func() {
		outlineButton(c, tr(c, "speed.action.new_with_combo")).Disabled(s.busy).OnClick(func() { s.newSpeedTask(r) })
	})
	stageKind := speedStageKind(r.stage)
	metaText := r.id + " · " + tr(c, speedStageKeys[r.stage])
	if s.preview != nil {
		metaText = trf(c, "speed.result.meta_preview", "id", r.id, "status", tr(c, speedStageKeys[r.stage]))
	}
	applyMaterial(c, ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Stack).
		Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
		modelIconBox(c, s.modelIconHint(r.model), 32, t.Radius.SM)
		ui.Column(c).Grow(1).MinWidth(180).Gap(2).Children(func() {
			ui.Text(c, r.model+" · "+r.engine+" · "+inferyard.ContextSpeedBundleID).
				Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine().Selectable()
			ui.Text(c, metaText).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
		})
		inlineBadge(c, tr(c, speedStageKeys[r.stage]), stageKind, badgeOptions{Size: sizeSM, Style: badgeSoft})
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
	reportSectionCard(c, reicon.Gauge, tr(c, "speed.result.card_title"), nil, func() {
		if r.summary != nil {
			ui.Text(c, tr(c, "speed.result.valid_median")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			if r.result != nil && r.result.Summary != nil {
				v := r.result.Summary.Columns
				applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
					Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
					ui.Text(c, trf(c, "speed.result.range",
						"time_min", fmt.Sprintf("%.2f", v.Time.Min),
						"time_max", fmt.Sprintf("%.2f", v.Time.Max),
						"prefill_min", fmt.Sprintf("%.2f", v.Prefill.Min),
						"prefill_max", fmt.Sprintf("%.2f", v.Prefill.Max),
						"decode_min", fmt.Sprintf("%.2f", v.Decode.Min),
						"decode_max", fmt.Sprintf("%.2f", v.Decode.Max),
					)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).Selectable()
				})
			}
			speedTable(c.Key("speed-summary"), &w.table, tr(c, "speed.table.summary_label"), r, []speedSample{*r.summary})
		} else {
			ui.Text(c, trf(c, "speed.result.valid_partial", "valid", r.validCount())).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
			if len(r.samples) > 0 {
				speedTable(c.Key("speed-partial"), &w.table, tr(c, "speed.table.partial_label"), nil, r.samples)
			}
		}
	})
	speedResultFolds(c, w, r)
}

func speedResultFolds(c *ui.Context, w *speedWorkspace, r *speedTask) {
	if r.summary == nil || r.reason != "" {
		componentFoldKeyed(c.Key(r.id), "diagnostics", tr(c, "speed.result.diagnostics_fold"), func() {
			loc := contextLocale(c)
			reason := r.reason
			if r.result != nil {
				reason = speedReasonsLabel(loc, r.result.Measurement.Reasons)
			}
			if reason != "" {
				ui.Text(c, reason).TextColor(stateColor(c, stateWarning))
			}
			for i, sample := range r.samples {
				if r.result != nil {
					raw := r.result.Samples[i]
					sample.reason = speedReasonsLabel(loc, raw.Measurement.Reasons)
					sample.missing = speedMissingLabels(loc, raw.Columns)
				}
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
			speedTable(c.Key("speed-samples"), &w.samples, tr(c, "speed.table.samples_label"), nil, r.samples)
		})
	}
}

func speedTable(c *ui.Context, state *pageTableState, label string, summaryTask *speedTask, rows []speedSample) {
	first := ui.TableColumn{Title: tr(c, "speed.col.sample"), Width: c.Theme().Rem(6)}
	if summaryTask != nil {
		first = ui.TableColumn{Title: tr(c, "speed.col.model_engine"), MinWidth: c.Theme().Rem(14)}
	}
	columns := []ui.TableColumn{
		first,
		{Title: tr(c, "speed.col.context_tokens"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.prompt_tokens"), MinWidth: c.Theme().Rem(11)},
		{Title: tr(c, "speed.col.input_time"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.prefill"), MinWidth: c.Theme().Rem(9)},
		{Title: tr(c, "speed.col.decode"), MinWidth: c.Theme().Rem(9)},
	}
	loc := contextLocale(c)
	selectableTable(state, func(i int) any { return i }, func(i int) string { return l10n.Tf(loc, "speed.table.row_label", "index", i+1) })
	applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
		pageTable(c, state, label, columns, len(rows), func(row, col int) {
			v := rows[row]
			prompt := "—"
			if v.prompt != nil {
				prompt = fmt.Sprint(*v.prompt)
			}
			if col == 0 {
				if summaryTask != nil {
					speedIdentity(c, summaryTask)
				} else {
					ui.Text(c, fmt.Sprintf("#%d", row+1)).FontFeatures("tnum").SingleLine()
				}
				return
			}
			context := "—"
			if v.context != nil {
				context = speedContext(*v.context)
			}
			val := []string{"", context, prompt, speedNumber(v.seconds), speedNumber(v.prefill), speedNumber(v.decode)}[col]
			cell := ui.Text(c, val).FontFeatures("tnum").SingleLine()
			if val == "—" {
				cell.TextColor(tokens(c).Color.FgTertiary)
			}
		}).Shrink(0)
	})
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
	reportSectionCard(c, reicon.Gauge, tr(c, "speed.history.card_title"), nil, func() {
		selectableTable(&w.historyTable, func(i int) any { return w.history[i].id }, func(i int) string { return w.history[i].id })
		applyMaterial(c, ui.Box(c).FillWidth().MinWidth(0).Clip(), materialInset).Children(func() {
			pageTable(c.Key("speed-history"), &w.historyTable, tr(c, "speed.history.table_label"), []ui.TableColumn{
				{Title: tr(c, "speed.history.col_task"), MinWidth: c.Theme().Rem(11)}, {Title: tr(c, "speed.col.model_engine"), MinWidth: c.Theme().Rem(24)},
				{Title: tr(c, "speed.history.col_context"), MinWidth: c.Theme().Rem(6)}, {Title: tr(c, "speed.history.col_status"), MinWidth: c.Theme().Rem(8)},
			}, len(w.history), func(row, col int) {
				r := w.history[row]
				switch col {
				case 0:
					ui.Text(c, r.id).FontFeatures("tnum").SingleLine()
				case 1:
					speedIdentity(c, r)
				case 2:
					ui.Text(c, speedContext(r.context)).FontFeatures("tnum").SingleLine()
				case 3:
					inlineBadge(c, r.label(), speedStageKind(r.stage), badgeOptions{Size: sizeSM, Style: badgeDot})
				}
			}).Height(c.Theme().Rem(4)*float32(min(len(w.history), 5)+1) + 2)
		})
		if i := selectedRow(&w.historyTable, len(w.history)); i >= 0 {
			if primaryButton(c, tr(c, "speed.history.open_result")).Clicked() || w.historyTable.submitted {
				s.openSpeed(w.history[i].id)
			}
		}
	})
}

func speedIdentity(c *ui.Context, task *speedTask) {
	t := tokens(c)
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, task.model).Bold().SingleLine()
		ui.Text(c, task.engine).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
	})
}
