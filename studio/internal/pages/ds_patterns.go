package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

func errorRecoveryBanner(c *ui.Context, e *core.ApiError, actions func()) {
	if e == nil {
		return
	}
	message := e.Message
	if message == "" {
		message = human(e.Code)
	}
	if e.Code == "conflict" {
		if reason, ok := e.Details["reason"].(string); ok {
			if label := conflictReasonLabel(reason); label != "" {
				message = label
			}
		}
	}
	noticeBanner(c, noticeBannerOptions{
		Kind:    stateDanger,
		Icon:    lucide.CircleAlert,
		Message: message,
		Actions: actions,
		Details: func() {
			scale := stateScale(c, stateDanger)
			for _, v := range e.Limitations {
				ui.Text(c, human(v)).TextColor(scale.Fg)
			}
			if e.Code != "" || len(e.Limitations) > 0 || len(e.Details) > 0 {
				componentFoldKeyed(c, "error-details-"+e.Code, tr(c, "common.error.details"), func() {
					if e.Code != "" {
						ui.Text(c, trf(c, "common.error.code", "code", e.Code)).TextColor(scale.Fg)
					}
					if len(e.Limitations) > 0 {
						ui.Text(c, trf(c, "common.error.limitations", "items", strings.Join(e.Limitations, tr(c, "common.list_separator")))).TextColor(scale.Fg)
					}
					if len(e.Details) > 0 {
						kvview(c, "kv-error-details", e.Details)
					}
				})
			}
		},
	})
}

type emptyGuideOptions struct {
	Icon        func() *ui.SVG
	Title       string
	Description string
	Surface     emptyStateSurface
	Action      func()
}

func emptyGuideCard(c *ui.Context, opts emptyGuideOptions) {
	emptyState(c, emptyStateOptions{
		Icon:        opts.Icon,
		Title:       opts.Title,
		Description: opts.Description,
		Surface:     opts.Surface,
		Action:      opts.Action,
	})
}

type kpiItem struct {
	Label  string
	Value  string
	Detail string
	Note   string
}

type kpiOverviewOptions struct {
	Title         string
	Items         []kpiItem
	EvidenceKey   string
	EvidenceTitle string
	Evidence      func()
}

func kpiOverviewSection(c *ui.Context, opts kpiOverviewOptions) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		if opts.Title != "" {
			heading(c, opts.Title, 2)
		}
		if len(opts.Items) > 0 {
			cols := min(4, len(opts.Items))
			if compactLayout(c) {
				cols = 1
			}
			ui.Grid(c).Columns(cols).Gap(t.Density.Stack).Children(func() {
				for _, item := range opts.Items {
					metricCard(c, item.Label, item.Value, item.Detail, item.Note)
				}
			})
		}
		if opts.Evidence != nil {
			title := opts.EvidenceTitle
			if title == "" {
				title = tr(c, "common.kpi.evidence_title")
			}
			key := opts.EvidenceKey
			if key == "" {
				key = title
			}
			componentFoldKeyed(c, key, title, opts.Evidence)
		}
	})
}

type filterableTableOptions struct {
	SearchPlaceholder string
	Query             *string
	Filters           func()
	Actions           func()
	HasActiveFilters  bool
	OnClearFilters    func()
	TotalCount        int
	FilteredCount     int
	EmptyTitle        string
	EmptyDescription  string
	EmptyAction       func()
	Table             func()
}

func filterableTable(c *ui.Context, opts filterableTableOptions) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(func() {
		hasQuery := opts.Query != nil && strings.TrimSpace(*opts.Query) != ""
		activeFilter := hasQuery || opts.HasActiveFilters
		if opts.Query != nil || opts.Filters != nil || opts.Actions != nil {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				if opts.Query != nil {
					box := components.TextBox(c, opts.Query, components.TextBoxOptions{}).Width(240).MaxWidthPercent(100)
					if opts.SearchPlaceholder != "" {
						box.Placeholder(opts.SearchPlaceholder).Label(opts.SearchPlaceholder)
					}
				}
				if opts.Filters != nil {
					opts.Filters()
				}
				if activeFilter && opts.OnClearFilters != nil {
					ghostButton(c, tr(c, "common.table.clear_filters")).OnClick(opts.OnClearFilters)
				}
				if opts.Actions != nil {
					opts.Actions()
				}
			})
		}
		switch {
		case opts.TotalCount == 0:
			title := opts.EmptyTitle
			if title == "" {
				title = tr(c, "common.table.empty_title")
			}
			emptyGuideCard(c, emptyGuideOptions{
				Icon:        lucide.Inbox,
				Title:       title,
				Description: opts.EmptyDescription,
				Surface:     emptySunken,
				Action:      opts.EmptyAction,
			})
		case opts.FilteredCount == 0:
			emptyGuideCard(c, emptyGuideOptions{
				Icon:        lucide.FileSearch,
				Title:       tr(c, "common.table.no_match_title"),
				Description: tr(c, "common.table.no_match_desc"),
				Surface:     emptySunken,
				Action: func() {
					if opts.OnClearFilters != nil {
						ghostButton(c, tr(c, "common.table.clear_filters")).OnClick(opts.OnClearFilters)
					}
				},
			})
		default:
			if opts.Table != nil {
				opts.Table()
			}
			summary := trf(c, "common.table.total_count", "count", opts.TotalCount)
			if opts.FilteredCount != opts.TotalCount {
				summary = trf(c, "common.table.filtered_count", "filtered", opts.FilteredCount, "total", opts.TotalCount)
			}
			ui.Text(c, summary).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).FontFeatures("tnum")
		}
	})
}

type settingsFormOptions struct {
	Title       string
	Description string
	Dirty       bool
	Fields      func()
	Actions     func()
}

func settingsFormSection(c *ui.Context, opts settingsFormOptions) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().MinWidth(0).Gap(t.Density.CardGap).Padding(t.Card.Padding), materialCard).Children(func() {
		ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			heading(c, opts.Title, 2).Grow(1)
			if opts.Dirty {
				badge(c, tr(c, "common.unsaved_changes"), stateWarning, badgeOptions{Style: badgeSoft, Size: sizeSM})
			}
		})
		if opts.Description != "" {
			ui.Text(c, opts.Description).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if opts.Fields != nil {
			ui.Column(c).FillWidth().Gap(t.Density.Stack).Children(opts.Fields)
		}
		if opts.Actions != nil {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(opts.Actions)
		}
	})
}
