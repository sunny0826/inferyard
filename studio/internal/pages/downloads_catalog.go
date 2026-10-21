package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

// catalogSelectState keeps the picked built-in asset per kind, and which
// choice was last written into the download form. A built-in address only
// replaces the form value when the user picks it, so a hand-edited URL is
// not overwritten by the next frame.
type catalogSelectState struct {
	engineID, modelID string
	appliedEngineID   string
	appliedModelID    string
}

// catalogView renders the read-only built-in address section. Only assets
// the current download channel can really fetch are offered as choices; the
// rest stay visible as reference entries with the reason and the preparation
// document, never as a prefilled URL that is bound to fail.
func (s *viewState) catalogView(c *ui.Context) {
	entries := core.Catalog()
	card(c, "内置地址", func() {
		ui.Text(c, "Studio 内置一份固定版本的下载地址目录。只有现有下载通道真能取到的资产可以直接下载；其余只作参考地址与准备说明。")
		s.catalogKindView(c, core.CatalogEngine, entries)
		s.catalogKindView(c, core.CatalogModel, entries)
	})
}

// catalogKindView renders one kind of built-in address: the choices the
// download channel accepts, then the reference-only entries.
func (s *viewState) catalogKindView(c *ui.Context, kind core.CatalogKind, entries []core.CatalogEntry) {
	title, label, placeholder := "引擎地址", "内置引擎", "请选择内置引擎"
	target := &s.engineURL
	selected, applied := &s.catalog.engineID, &s.catalog.appliedEngineID
	if kind == core.CatalogModel {
		title, label, placeholder = "模型地址", "内置模型", "请选择内置模型"
		target = &s.modelURL
		selected, applied = &s.catalog.modelID, &s.catalog.appliedModelID
	}
	section(c, title, func() {
		choices := []pickerOption{}
		byID := map[string]core.CatalogEntry{}
		for _, entry := range entries {
			if entry.Kind != kind {
				continue
			}
			byID[entry.ID] = entry
			if !entry.Downloadable() {
				continue
			}
			note := core.PlatformLabel(entry.Platform) + " · " + catalogSize(entry.Bytes)
			choices = append(choices, pickerOption{value: entry.ID, label: entry.Title, note: note})
		}
		if len(choices) == 0 {
			ui.Text(c, "当前平台没有可直接下载的内置地址。")
			return
		}
		if *selected == "" {
			*selected = s.catalogDefault(kind, choices)
		}
		pickerSelect(c, label, placeholder, selected, s.catalogQuery(kind), choices, "没有匹配的内置地址。")
		entry, ok := byID[*selected]
		if !ok {
			return
		}
		if *applied != *selected {
			*target = entry.URL
			*applied = *selected
		}
		ui.Text(c, catalogEntrySummary(entry)).FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
		ui.Text(c, "SHA256 "+entry.SHA256).FontSize(max(12, c.Theme().Rem(.8))).TextColor(c.Theme().TextMuted).Selectable()
	})
	references := []core.CatalogEntry{}
	for _, entry := range entries {
		if entry.Kind == kind && !entry.Downloadable() {
			references = append(references, entry)
		}
	}
	if len(references) == 0 {
		return
	}
	componentFoldOpen(c, "catalog-reference-"+string(kind), "参考地址（不可直接下载）", func() {
		for _, entry := range references {
			ui.Column(c).Gap(c.Theme().Space(0.5)).Children(func() {
				ui.Text(c, entry.Title+" · "+core.PlatformLabel(entry.Platform)).Bold()
				ui.Text(c, entry.Reason).FontSize(max(12, c.Theme().Rem(.85))).TextColor(c.Theme().TextMuted)
				ui.Text(c, entry.URL).FontSize(max(12, c.Theme().Rem(.8))).TextColor(c.Theme().TextMuted).Selectable()
				if entry.Bytes > 0 {
					ui.Text(c, catalogSize(entry.Bytes)).FontSize(max(12, c.Theme().Rem(.8))).TextColor(c.Theme().TextMuted)
				}
				ui.Text(c, "准备说明："+entry.Doc).FontSize(max(12, c.Theme().Rem(.8))).TextColor(c.Theme().TextMuted)
			})
		}
	})
}

// catalogDefault picks the entry the current platform prefers: the running
// platform's own build for engines, and the smallest measured model for
// models, so the prefilled URL is the one that works here.
func (s *viewState) catalogDefault(kind core.CatalogKind, choices []pickerOption) string {
	if kind == core.CatalogEngine {
		if id := core.CatalogEngineForCurrentPlatform(); id != "" {
			return id
		}
	}
	best := ""
	bestBytes := int64(-1)
	for _, choice := range choices {
		entry, ok := core.CatalogEntryByID(choice.value)
		if !ok {
			continue
		}
		if bestBytes < 0 || entry.Bytes < bestBytes {
			best, bestBytes = entry.ID, entry.Bytes
		}
	}
	return best
}

// catalogQuery returns the search box state of one built-in address picker.
func (s *viewState) catalogQuery(kind core.CatalogKind) *string {
	if kind == core.CatalogModel {
		return &s.catalogModelQuery
	}
	return &s.catalogEngineQuery
}

// catalogSize renders the measured size of an asset.
func catalogSize(bytes int64) string {
	if bytes <= 0 {
		return "大小未知"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	return fmt.Sprintf("%s（%d bytes）", catalogHumanSize(bytes), bytes)
}

// catalogHumanSize renders a byte count in the unit the studio uses.
func catalogHumanSize(bytes int64) string {
	switch {
	case bytes >= 1073741824:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/1073741824)
	case bytes >= 1048576:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/1048576)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%d bytes", bytes)
}

// catalogEntrySummary describes one built-in entry in a single line.
func catalogEntrySummary(entry core.CatalogEntry) string {
	parts := []string{entry.Title, core.PlatformLabel(entry.Platform)}
	if entry.Revision != "" {
		parts = append(parts, "版本 "+entry.Revision)
	}
	if entry.Bytes > 0 {
		parts = append(parts, catalogHumanSize(entry.Bytes))
	}
	if entry.Reason != "" {
		parts = append(parts, "仅参考")
	}
	return strings.Join(parts, " · ")
}
