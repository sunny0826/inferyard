package pages

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func (s *viewState) downloadsStarterCard(c *ui.Context) {
	s.downloadsStarterCardInContext(c, "downloads")
}

func (s *viewState) downloadsStarterCardInContext(c *ui.Context, contextMode string) {
	rec := s.starterRecommendation()
	if !s.hasStarterContent(rec) {
		return
	}
	if !s.starterPanelExpanded() {
		s.downloadsCompactStarterBar(c, rec)
		return
	}
	s.renderStarterRecommendationPanel(c, contextMode)
}

func (s *viewState) downloadsCompactStarterBar(c *ui.Context, rec core.StarterRecommendation) {
	t := tokens(c)
	opts := starterOptions(rec)
	hwSummary := s.starterHardwareSummary(rec)

	applyMaterial(c, ui.Row(c.Key("downloads-starter-compact-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Stack, t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			if rec.Status == "blocked" && len(opts) == 0 {
				badge(c, tr(c, "downloads.starter.badge.hw_unavailable"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				reason := deviceLabel(rec.Reason)
				if reason == "" {
					reason = tr(c, "downloads.starter.blocked_default")
				}
				ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			} else if len(opts) > 0 {
				top := opts[0]
				badge(c, tr(c, "downloads.starter.badge.hw_recommended"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, starterEngineLabel(top.Engine)+" · "+starterOptionTitle(top)).Bold().SingleLine()
				meta := []string{}
				if hwSummary != "" {
					meta = append(meta, hwSummary)
				}
				if len(opts) > 1 {
					meta = append(meta, trf(c, "downloads.starter.options_total", "count", strconv.Itoa(len(opts))))
				}
				if len(meta) > 0 {
					ui.Text(c, joinParts(meta)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
			}
		})
		outlineButton(c, tr(c, "downloads.starter.expand")).OnClick(func() {
			s.setStarterPanelExpanded(true)
		})
	})
}

func (s *viewState) renderStarterRecommendationPanel(c *ui.Context, contextMode string) {
	st := s.ensureDownloadsUI()
	rec := s.starterRecommendation()
	t := tokens(c)

	if s.device.Refreshing && !rec.FromSnapshot {
		applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.Row).Padding(t.Card.Padding), materialCard).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				heading(c, tr(c, "downloads.starter.heading"), 3)
				badge(c, tr(c, "downloads.starter.badge.detecting"), stateActive, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Text(c, tr(c, "downloads.starter.detecting_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		})
		return
	}

	opts := starterOptions(rec)
	if len(opts) == 0 && rec.Status != "blocked" {
		return
	}
	if st.starterChoice < 0 || st.starterChoice >= len(opts) {
		st.starterChoice = 0
	}

	hwSummary := s.starterHardwareSummary(rec)
	showCollapse := contextMode == "downloads" && s.starterDefaultCollapsed()

	applyMaterial(c, ui.Column(c.Key("downloads-starter-card")).FillWidth().Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		if rec.Status == "blocked" && len(opts) == 0 {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
						heading(c, tr(c, "downloads.starter.heading"), 3)
						badge(c, tr(c, "downloads.starter.badge.unavailable"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
					})
					if hwSummary != "" {
						ui.Text(c, trf(c, "downloads.starter.hw_line", "summary", hwSummary)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
					reason := deviceLabel(rec.Reason)
					if reason == "" {
						reason = tr(c, "downloads.starter.blocked_default")
					}
					ui.Text(c, reason).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if notes := localizedDeviceNotes(rec.Notes); len(notes) > 0 {
						ui.Text(c, strings.Join(notes, tr(c, "downloads.punctuation.semicolon"))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					}
				})
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					outlineButton(c, tr(c, "downloads.starter.redetect")).Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
					button(c, tr(c, "downloads.starter.view_device")).OnClick(func() {
						s.libraryHwExpanded = true
						s.selected = "library"
					})
					if showCollapse {
						ghostButton(c, tr(c, "downloads.starter.collapse")).OnClick(func() { s.setStarterPanelExpanded(false) })
					}
				})
			})
			return
		}

		// Header row: title + option count + refresh / collapse controls
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				heading(c, tr(c, "downloads.starter.heading"), 3)
				badge(c, trf(c, "downloads.starter.options_count_badge", "count", strconv.Itoa(len(opts))), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ghostButton(c, tr(c, "downloads.starter.redetect")).Disabled(s.deviceCollectionBlocked()).OnClick(func() { s.refreshDevice() })
				if showCollapse {
					outlineButton(c, tr(c, "downloads.starter.collapse")).OnClick(func() { s.setStarterPanelExpanded(false) })
				}
			})
		})

		// Hardware summary strip (omitted inside libraryView where libraryHardwareCard already sits directly above)
		if hwSummary != "" && contextMode != "library" {
			applyMaterial(c, ui.Row(c.Key("starter-hardware-strip")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Inset, t.Density.Stack), materialSunken).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
					components.Icon(c, lucide.Cpu).Size(14, 14).TextColor(t.Color.FgSecondary)
					ui.Text(c, trf(c, "downloads.starter.hw_line", "summary", hwSummary)).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).FontFeatures("tnum")
				})
				ghostButton(c, tr(c, "downloads.starter.device_details")).OnClick(func() {
					s.libraryHwExpanded = true
					s.selected = "library"
				})
			})
		}

		// Fallback reason notice (e.g. GPU unsupported -> switched to llama.cpp)
		if rec.Reason != "" && rec.Reason != "polystrata_hardware_fit" {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateInfo,
				Size:    bannerCompact,
				Message: deviceLabel(rec.Reason),
			})
		}

		// Balanced responsive grid (2x2 for 4 plans instead of 3 + 1 full-width)
		cols := min(3, max(1, len(opts)))
		if len(opts) == 4 {
			cols = 2
		}
		if compactLayout(c) {
			cols = min(2, cols)
		}
		if w, _ := c.Size(); w > 0 && w < 680 {
			cols = 1
		}
		ui.Grid(c.Key("starter-options-grid")).FillWidth().Columns(cols).Gap(t.Density.Row).Children(func() {
			for i, opt := range opts {
				idx, option := i, opt
				s.starterOptionTile(c, st, idx, option, idx == st.starterChoice)
			}
		})

		chosen := opts[st.starterChoice]

		// Inline macOS Xcode CLT notice when PolyStrata is selected
		if chosen.Engine == "polystrata" && slices.Contains(rec.Notes, "xcode_clt_missing") {
			applyMaterial(c, ui.Row(c.Key("starter-xcode-clt-hint")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
				badge(c, tr(c, "downloads.starter.xcode_badge"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
				ui.Text(c, tr(c, "downloads.starter.xcode_desc")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				ui.Text(c, "xcode-select --install").Selectable().FontSize(t.Type.Caption)
				outlineButton(c, tr(c, "downloads.action.copy_cmd")).Label(tr(c, "downloads.starter.copy_xcode_cmd_label")).OnClick(func() {
					s.copyDownloadText(c, tr(c, "downloads.copy_subject.install_cmd"), "xcode-select --install")
				})
			})
		}

		// Other hardware/environment notes
		if notes := starterCardNotes(rec.Notes, chosen.Engine == "polystrata"); len(notes) > 0 {
			ui.Text(c, trf(c, "downloads.starter.notes_line", "notes", strings.Join(notes, tr(c, "downloads.punctuation.semicolon")))).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}

		// Unified bottom bar: model source selector + primary CTA
		ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
		ui.Row(c.Key("starter-action-bar")).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			ui.Row(c.Key("starter-model-source-row")).Grow(1).MinWidth(0).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				ui.Text(c, tr(c, "downloads.starter.model_source")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				if chosen.Engine == "polystrata" {
					ui.Box(c.Key("starter-source-polystrata")).Children(func() {
						prev := st.polystrataModelSource
						segmentedControl(c, tr(c, "downloads.starter.polystrata_source_label"), &st.polystrataModelSource, []components.NativeSelectOption[string]{
							{Value: "auto", Label: tr(c, "downloads.source.auto")},
							{Value: "modelscope", Label: "ModelScope"},
							{Value: "huggingface", Label: "HuggingFace"},
						})
						if st.polystrataModelSource != prev {
							st.setPolystrataModelSource(st.polystrataModelSource)
						}
					})
				} else if chosen.Engine == "mlx-lm" {
					inlineBadge(c, "HuggingFace", stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				} else {
					ui.Box(c.Key("starter-source-direct")).Children(func() {
						prev := st.modelSource
						segmentedControl(c, tr(c, "downloads.starter.direct_source_label"), &st.modelSource, []components.NativeSelectOption[string]{
							{Value: "huggingface", Label: "HuggingFace"},
							{Value: "modelscope", Label: "ModelScope"},
						})
						if st.modelSource != prev {
							st.setDirectModelSource(st.modelSource)
						}
					})
				}
			})

			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				if chosen.Engine == "polystrata" {
					outlineButton(c, tr(c, "downloads.starter.more_config")).Label(tr(c, "downloads.starter.more_polystrata_config_label")).OnClick(func() {
						st.polystrataDrawerModelID = chosen.ModelID
						if contextMode == "dashboard" {
							s.selected = "library"
						}
						s.openPolystrataDrawerForCurrentHost()
					})
				}
				if contextMode == "dashboard" {
					outlineButton(c, tr(c, "downloads.starter.go_downloads")).OnClick(func() {
						s.openDownloadsWithStarterExpanded()
					})
				}
				s.starterPrimaryCTA(c, st, chosen, contextMode == "dashboard")
			})
		})
	})
}
