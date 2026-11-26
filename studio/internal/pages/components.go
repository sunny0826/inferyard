package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func card(c *ui.Context, title string, fn func()) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).
		Children(func() { heading(c, title, 2); fn() })
}

func statusBadge(c *ui.Context, label string, color ui.Color) ui.Element {
	theme := c.Theme()
	t := tokens(c)
	background, foreground := badgeSurfaceColors(theme, color)
	badge := components.Badge(c, label).
		Padding(t.Density.Tight, t.Density.Row).Radius(t.Radius.Full).
		TextColor(foreground).Background(background)
	if c.Preferences().HighContrast {
		badge.Background(theme.Surface).Border(1, color)
	}
	return badge
}

func badgeSurfaceColors(t *ui.Theme, tint ui.Color) (background, foreground ui.Color) {
	return t.Surface.Mix(tint, .12), tint.Mix(t.Text, .3)
}

type phaseProgressOptions struct {
	Labels   []string
	Current  int
	RowLabel string
	BarLabel string
	HideBar  bool
}

func phaseProgress(c *ui.Context, o phaseProgressOptions) {
	t := tokens(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Label(o.RowLabel).Children(func() {
		for i, label := range o.Labels {
			if i > 0 {
				lineColor := t.Color.BorderDefault
				if i <= o.Current {
					lineColor = t.Color.Success.Solid.Alpha(.55)
				}
				ui.Box(c).Height(2).MinWidth(10).MaxWidth(28).Grow(1).Radius(t.Radius.Full).Background(lineColor)
			}
			stepNum := fmt.Sprintf("%d", i+1)
			switch {
			case i < o.Current:
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Box(c).Size(20, 20).Radius(t.Radius.Full).Shrink(0).
						Background(t.Color.Success.SubtleBg).Border(1, t.Color.Success.Border).
						AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						components.Icon(c, lucide.Check).Size(12, 12).TextColor(t.Color.Success.Fg)
					})
					ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).SingleLine()
				})
			case i == o.Current:
				pill := ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).
					Padding(3, t.Density.Row, 3, 4).Radius(t.Radius.Full).
					Background(t.Color.Active.SubtleBg).Border(1, t.Color.Active.Border).
					Description(trf(c, "common.phase.current", "label", label))
				pill.Children(func() {
					ui.Box(c).Size(18, 18).Radius(t.Radius.Full).Shrink(0).
						Background(t.Color.Active.Solid).
						AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, stepNum).Bold().FontSize(11).FontFeatures("tnum").TextColor(c.Theme().Surface).SingleLine()
					})
					ui.Text(c, label).Bold().FontSize(t.Type.Caption).TextColor(t.Color.Active.Fg).SingleLine()
				})
			default:
				ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					ui.Box(c).Size(20, 20).Radius(t.Radius.Full).Shrink(0).
						Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).
						AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Text(c, stepNum).FontSize(11).FontFeatures("tnum").TextColor(t.Color.FgTertiary).SingleLine()
					})
					ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary).SingleLine()
				})
			}
		}
	})
	if o.HideBar {
		return
	}
	value := float64(0)
	if len(o.Labels) > 0 {
		value = min(1, float64(o.Current)/float64(len(o.Labels)))
	}
	progressBar(c, value, o.BarLabel).Height(6)
}

func metricCard(c *ui.Context, label, value, detail, note string) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).MinWidth(0).Gap(t.Metric.Gap).Padding(t.Metric.PaddingY, t.Metric.PaddingX), materialCard).
		Children(func() {
			ui.Text(c, label).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			ui.Text(c, value).Bold().FontSize(t.Type.MetricLarge).FontFeatures("tnum").Selectable()
			if detail != "" {
				ui.Text(c, detail).TextColor(t.Color.FgSecondary)
			}
			if note != "" {
				ui.Text(c, note).TextColor(t.Color.FgSecondary)
			}
		})
}

type taskMetric struct {
	label, value, reason string
}

type taskCardOptions struct {
	kind, name, state string
	color             ui.Color
	active            bool
	metrics           []taskMetric
	status            string
	headerRight       func()
	notice            func()
	actions           func()
}

func taskCard(c *ui.Context, options taskCardOptions) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).Label(options.name).Gap(t.Density.Stack).Padding(t.Card.Padding), materialInset).Children(func() {
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.CardGap).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, options.name).Bold().FontSize(t.Type.Section)
				if options.kind != "" {
					ui.Text(c, options.kind).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
				}
			})
			if options.headerRight != nil {
				options.headerRight()
			} else {
				statusBadge(c, options.state, options.color)
			}
		})
		if options.status != "" {
			ui.Text(c, options.status).TextColor(c.Theme().TextMuted)
		} else {
			columns := max(1, min(3, len(options.metrics)))
			width, _ := c.Size()
			if width < 900 || c.Preferences().TextScale > 1 {
				columns = 1
			}
			ui.Grid(c).Columns(columns).Gap(t.Density.Stack).Children(func() {
				for _, metric := range options.metrics {
					ui.Column(c).Gap(t.Density.Tight).Children(func() {
						ui.Text(c, metric.label).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
						ui.Text(c, metric.value).Bold().FontSize(t.Type.Metric).FontFeatures("tnum")
						if metric.reason != "" {
							ui.Text(c, metric.reason).FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
						}
					})
				}
			})
		}
		if options.active {
			progressBar(c, -1, tr(c, "common.task.download_activity")).Height(t.Density.Tight)
		}
		if options.notice != nil {
			options.notice()
		}
		if options.actions != nil {
			options.actions()
		}
	})
}

func stepTrack(c *ui.Context, number, title string, fn func()) {
	t := tokens(c)
	theme := c.Theme()
	size := float32(26)
	badgeBg := stateColor(c, stateActive)
	if number == "✓" {
		badgeBg = stateColor(c, stateSuccess)
	}
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding).
		Label(trf(c, "common.step.label", "number", number)), materialCard).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Box(c).Label(trf(c, "common.step.number_label", "number", number)).
				Size(size, size).Shrink(0).Radius(t.Radius.Full).Background(badgeBg).
				AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, number).Bold().FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(theme.Surface).SingleLine()
			})
			heading(c, title, 2)
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Children(fn)
	})
}

func selectField[T comparable](c *ui.Context, name, description string, selected *T, options []components.NativeSelectOption[T]) {
	f := ui.Field(c, name, func() {
		t := tokens(c)
		current := ""
		for _, option := range options {
			if option.Value == *selected {
				current = option.Label
				break
			}
		}
		components.NativeSelect(c, selected, options).Trigger.
			MinHeight(t.Control.MD).
			Padding(t.Density.Tight, c.Theme().Space(6), t.Density.Tight, t.Density.Stack).
			Radius(t.Radius.Control).
			Label(name).Value(current)
	})
	if description != "" {
		f.Description(description)
	}
}

func segmentedControl[T comparable](c *ui.Context, name string, selected *T, options []components.NativeSelectOption[T]) ui.Element {
	if len(options) == 0 {
		return ui.Element{}
	}
	t := tokens(c)
	idx := 0
	for i, option := range options {
		if option.Value == *selected {
			idx = i
			break
		}
	}
	seg := ui.SegmentedBase(c, &idx, len(options))
	trackRadius := t.Radius.Floating
	trackPad := t.Density.Tight
	itemRadius := concentricRadius(trackRadius, trackPad)
	segHeight := max(float32(20), t.Control.MD-trackPad*2-2)
	track := seg.Track.AlignItems(ui.Center).Gap(t.Density.Tight).
		MinHeight(t.Control.MD).
		Padding(trackPad).Radius(trackRadius).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Label(name).
		Children(func() {
			for i, option := range options {
				active := i == idx
				item := seg.Segment(i).
					AlignItems(ui.Center).Justify(ui.Center).
					MinHeight(segHeight).
					Padding(0, t.Density.Stack).
					Radius(itemRadius).
					Label(option.Label).
					Disabled(option.Disabled)
				if active {
					border := t.Color.BorderSubtle
					if c.Preferences().HighContrast {
						border = stateColor(c, stateActive)
					}
					applyShadow(item.Background(t.Color.Surface).Border(1, border), t.Elevation.XS)
				} else if item.Hovered() && !option.Disabled {
					item.Background(t.Color.Surface.Alpha(.6))
				}
				item.Children(func() {
					label := ui.Text(c, option.Label).SingleLine()
					if active {
						label.Bold().TextColor(t.Color.FgPrimary)
					} else {
						label.TextColor(t.Color.FgSecondary)
					}
				})
			}
		})
	if seg.Track.Changed() && idx >= 0 && idx < len(options) && !options[idx].Disabled {
		*selected = options[idx].Value
	}
	return track
}

func segmentedField[T comparable](c *ui.Context, name, description string, selected *T, options []components.NativeSelectOption[T]) {
	if len(options) == 0 {
		return
	}
	f := ui.Field(c, name, func() {
		segmentedControl(c, name, selected, options).AlignSelf(ui.Start)
	})
	if description != "" {
		f.Description(description)
	}
}

type searchableSelectState struct {
	query        string
	lastSelected string
	wasOpen      bool
}

func filterSearchableOptions(options []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return options
	}
	prefix := make([]string, 0, len(options))
	contains := make([]string, 0, len(options))
	for _, opt := range options {
		lower := strings.ToLower(opt)
		if strings.HasPrefix(lower, q) {
			prefix = append(prefix, opt)
		} else if strings.Contains(lower, q) {
			contains = append(contains, opt)
		}
	}
	return append(prefix, contains...)
}

func searchableSelectField(c *ui.Context, name, description, placeholder, emptyFormat string, selected *string, options []string) {
	f := ui.Field(c, name, func() {
		t := tokens(c)
		container := ui.Column(c).FillWidth().MaxWidth(420)
		state := ui.Local(container, "searchable-select", func() searchableSelectState {
			return searchableSelectState{query: *selected, lastSelected: *selected}
		})
		if state.lastSelected != *selected {
			state.query = *selected
			state.lastSelected = *selected
		}
		container.Children(func() {
			cb := ui.ComboboxBase(c, &state.query)
			border := t.Color.BorderDefault
			if cb.Input.Focused() {
				border = t.Color.BorderFocus
			}
			applyMaterial(c, cb.Input.FillWidth().Label(name).Placeholder(placeholder).
				MinHeight(t.Control.MD).
				Padding(c.Theme().Space(2.25), t.Density.Stack), materialControl).Border(1, border)

			visible := options
			if cb.Filtering() {
				visible = filterSearchableOptions(options, state.query)
			}
			cb.Popup(func(panel ui.Element) {
				popupPad := t.Density.Tight
				itemRadius := concentricRadius(t.Radius.Floating, popupPad)
				applyMaterial(c, panel.FillWidth().MinWidth(260).MaxHeight(260).
					Padding(popupPad), materialFloating).
					Children(func() {
						ui.Scroll(c).MaxHeight(248).Children(func() {
							ui.Column(c).FillWidth().Gap(t.Density.Hairline).Children(func() {
								if len(visible) == 0 {
									msg := tr(c, "common.select.empty")
									if len(options) > 0 {
										msg = fmt.Sprintf(emptyFormat, strings.TrimSpace(state.query))
									}
									ui.Text(c, msg).TextColor(t.Color.FgSecondary).Padding(t.Density.Inset, t.Density.Row)
									return
								}
								for _, opt := range visible {
									item := cb.Item(opt).FillWidth().Label(opt).
										Padding(t.Density.Inset, t.Density.Row).
										Radius(itemRadius)
									if item.Highlighted() {
										item.Background(t.Color.SurfaceHover)
									}
									picked := opt == *selected
									item.Children(func() {
										ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
											mark := "  "
											if picked {
												mark = "✓ "
											}
											ui.Text(c, mark).TextColor(t.Color.FgPrimary)
											label := ui.Text(c, opt).SingleLine().Grow(1).MinWidth(0)
											if picked {
												label.Bold()
											}
										})
									})
								}
							})
						})
					})
			})
			if chosen, ok := cb.Chosen(); ok {
				*selected = chosen
				state.query = chosen
				state.lastSelected = chosen
			} else if state.wasOpen && !cb.Open() {
				state.query = *selected
			}
			state.wasOpen = cb.Open()
		})
	})
	if description != "" {
		f.Description(description)
	}
}

func studioDialog(c *ui.Context, open *bool, options components.DialogOptions, build func(panel ui.Element)) ui.Element {
	t := tokens(c)
	return components.Dialog(c, open, options, func(panel ui.Element) {
		panel.Padding(t.Density.Section).Gap(t.Density.Stack).Radius(t.Radius.Modal)
		if build != nil {
			build(panel)
		}
	})
}

func studioAlertDialog(c *ui.Context, open *bool, options components.AlertDialogOptions) components.DialogResult {
	return studioAlertDialogStyled(c, open, options, false)
}

func studioDestructiveAlertDialog(c *ui.Context, open *bool, options components.AlertDialogOptions) components.DialogResult {
	return studioAlertDialogStyled(c, open, options, true)
}

func studioAlertDialogStyled(c *ui.Context, open *bool, options components.AlertDialogOptions, destructive bool) components.DialogResult {
	result := components.DialogNone
	wasOpen := *open
	opts := options.DialogOptions
	if opts.Label == "" {
		opts.Label = options.Title
	}
	t := tokens(c)
	studioDialog(c, open, opts, func(panel ui.Element) {
		panel.Role(ui.RoleAlertDialog)
		if options.Title != "" {
			components.Text(c, options.Title).Bold()
		}
		if options.Message != "" {
			components.Text(c, options.Message).TextColor(t.Color.FgSecondary)
		}
		ui.Row(c).FillWidth().Wrap().Justify(ui.End).Gap(t.Density.Row).Children(func() {
			if !options.HideCancel && button(c, options.CancelLabel).Clicked() {
				result = components.DialogCancelled
				*open = false
			}
			confirmBtn := primaryButton
			if destructive {
				confirmBtn = dangerButton
			}
			if confirmBtn(c, options.ConfirmLabel).Clicked() {
				result = components.DialogConfirmed
				*open = false
			}
		})
	})
	if wasOpen && !*open && result == components.DialogNone {
		result = components.DialogCancelled
	}
	return result
}

func studioPopover(c *ui.Context, anchor ui.Element, open *bool, build func(panel ui.Element)) ui.Element {
	t := tokens(c)
	return ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		applyMaterial(c, panel.Column().Padding(t.Card.Padding).Gap(t.Density.Stack).
			Margin(t.Density.Row, 0, 0, 0).TextColor(t.Color.FgPrimary).Clip().
			AttachEdgeMargin(t.Density.Stack), materialFloating)
		if build != nil {
			build(panel)
		}
	})
}
