package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

const customCatalogChoice = "__custom__"

type catalogChoiceState struct {
	custom    bool
	customURL string
}

func matchesCatalogEntry(entry core.CatalogEntry, target string) bool {
	return target == entry.URL || (entry.MirrorURL != "" && target == entry.MirrorURL)
}

func (s *viewState) catalogChoices(c *ui.Context, kind core.CatalogKind, platform, customLabel string, customField func()) {
	t := tokens(c)
	target := &s.engineURL
	groupLabel := "推荐引擎"
	if kind == core.CatalogModel {
		target = &s.modelURL
		groupLabel = "推荐模型"
	}
	entries := []core.CatalogEntry{}
	var matchedEntry *core.CatalogEntry
	for _, entry := range core.CatalogForPlatform(platform) {
		if entry.Kind != kind || !entry.Downloadable() {
			continue
		}
		entries = append(entries, entry)
		if matchesCatalogEntry(entry, *target) {
			matched := entry
			matchedEntry = &matched
		}
	}
	container := ui.Column(c.Key("catalog-" + string(kind))).FillWidth().Gap(t.Density.Stack)
	state := ui.Local(container, "catalog-choice", func() catalogChoiceState { return catalogChoiceState{} })
	if matchedEntry == nil && *target != "" {
		state.custom = true
		state.customURL = *target
	}
	selected := *target
	if matchedEntry != nil {
		selected = matchedEntry.URL
	}
	if state.custom || matchedEntry == nil {
		selected = customCatalogChoice
	}
	container.Children(func() {
		if len(entries) == 0 {
			ui.Text(c, "当前平台没有可直接下载的推荐资源，请使用自定义地址或查看其他准备方式。").TextColor(c.Theme().TextMuted)
			customField()
			return
		}
		ui.RadioGroup(c, func() {
			for _, entry := range entries {
				entry := entry
				ui.Column(c.Key(entry.ID)).FillWidth().Gap(t.Density.Hairline).Children(func() {
					if components.Radio(c, &selected, entry.URL, entry.Title).Changed() {
						state.custom = false
						*target = entry.URL
					}
					ui.Column(c).Padding(0, 0, 0, c.Theme().Space(6)).Gap(t.Density.Hairline).Children(func() {
						caption := core.PlatformLabel(entry.Platform)
						if entry.Format == "directory" {
							caption += " · 目录模型"
						}
						if entry.Bytes > 0 {
							caption += " · " + catalogHumanSize(entry.Bytes)
						} else if entry.Format == "directory" {
							caption += " · 自动生成清单"
						}
						ui.Text(c, caption).
							FontSize(t.Type.Caption).TextColor(c.Theme().TextMuted)
						if entry.Note != "" {
							ui.Text(c, entry.Note).FontSize(t.Type.Caption).TextColor(stateColor(c, stateWarning))
						}
					})
				})
			}
			ui.Column(c.Key("custom")).FillWidth().Gap(t.Density.Tight).Children(func() {
				if components.Radio(c, &selected, customCatalogChoice, customLabel).Changed() {
					state.custom = true
					*target = state.customURL
				}
				if selected == customCatalogChoice {
					ui.Column(c).Padding(0, 0, 0, c.Theme().Space(6)).Children(func() {
						customField()
						state.customURL = *target
					})
				}
			})
		}).Gap(t.Density.Stack).Label(groupLabel)
	})
}

func catalogDetails(c *ui.Context, source string) {
	entry, ok := core.CatalogEntryByURL(source)
	if !ok {
		ui.Text(c, "自定义地址").TextColor(c.Theme().TextMuted)
		return
	}
	if !entry.RunsOnCurrentPlatform() {
		noticeBanner(c, noticeBannerOptions{
			Kind:    stateWarning,
			Size:    bannerCompact,
			Message: "此资源适用于 " + core.PlatformLabel(entry.Platform) + "，不能在本机运行。",
		})
	}
	componentFoldKeyed(c, "catalog-details-"+string(entry.Kind), "版本与校验 · "+entry.Title, func() {
		if entry.MirrorURL != "" && source == entry.MirrorURL {
			label := entry.MirrorLabel
			if label == "" {
				label = "镜像源"
			}
			ui.Text(c, "镜像源："+label+"（下载前自动解析最新提交版本）")
		} else {
			ui.Text(c, "版本 "+entry.Revision).Selectable()
		}
		ui.Text(c, catalogSize(entry.Bytes)).TextColor(c.Theme().TextMuted)
		if entry.SHA256 != "" {
			ui.Text(c, "SHA256 "+entry.SHA256).Selectable().TextColor(c.Theme().TextMuted)
		}
	})
}

func (s *viewState) catalogView(c *ui.Context) {
	catalogReferences(c, core.CurrentPlatform())
}

func catalogReferences(c *ui.Context, platform string) {
	t := tokens(c)
	componentFold(c, "其他准备方式", func() {
		for _, entry := range core.CatalogForPlatform(platform) {
			if entry.Downloadable() {
				continue
			}
			ui.Column(c.Key(entry.ID)).Gap(t.Density.Tight).Padding(t.Density.Row).Children(func() {
				ui.Text(c, entry.Title).Bold()
				ui.Text(c, entry.Reason).TextColor(c.Theme().TextMuted)
				ui.Text(c, entry.URL).Selectable().TextColor(c.Theme().TextMuted)
			})
		}
	})
}

func catalogSize(bytes int64) string {
	if bytes <= 0 {
		return "大小未知"
	}
	if bytes < 1024 {
		return fmt.Sprintf("%d bytes", bytes)
	}
	return fmt.Sprintf("%s（%d bytes）", catalogHumanSize(bytes), bytes)
}

func catalogHumanSize(bytes int64) string {
	switch {
	case bytes <= 0:
		return "大小未知"
	case bytes >= 1073741824:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/1073741824)
	case bytes >= 1048576:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/1048576)
	case bytes >= 1024:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%d bytes", bytes)
}
