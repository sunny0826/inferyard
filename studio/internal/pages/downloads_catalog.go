package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func (s *viewState) catalogChoices(c *ui.Context, kind core.CatalogKind, platform string) {
	target := &s.engineURL
	if kind == core.CatalogModel {
		target = &s.modelURL
	}
	count := 0
	for _, entry := range core.CatalogForPlatform(platform) {
		if entry.Kind != kind || !entry.Downloadable() {
			continue
		}
		count++
		selected := *target == entry.URL
		border := c.Theme().Border
		if selected {
			border = c.Theme().Accent
		}
		ui.Row(c.Key(entry.ID)).FillWidth().AlignItems(ui.Center).Gap(c.Theme().Space(3)).Padding(c.Theme().Space(3)).Radius(c.Theme().Space(2)).Border(1, border).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(c.Theme().Space(1)).Children(func() {
				ui.Text(c, entry.Title).Bold()
				ui.Text(c, core.PlatformLabel(entry.Platform)+" · "+catalogHumanSize(entry.Bytes)).TextColor(c.Theme().TextMuted)
				if entry.Note != "" {
					ui.Text(c, entry.Note).TextColor(c.Theme().Warning)
				}
			})
			label := "选择"
			if selected {
				label = "已选择"
			}
			button(c, label).Label(label + " · " + entry.DisplayName()).Shrink(0).Disabled(selected).OnClick(func() { *target = entry.URL })
		})
	}
	if count == 0 {
		ui.Text(c, "当前平台没有可直接下载的推荐资源，请使用自定义地址或查看其他准备方式。").TextColor(c.Theme().TextMuted)
	}
}

func catalogDetails(c *ui.Context, source string) {
	entry, ok := core.CatalogEntryByURL(source)
	if !ok {
		ui.Text(c, "自定义地址").TextColor(c.Theme().TextMuted)
		return
	}
	if !entry.RunsOnCurrentPlatform() {
		ui.Text(c, "此资源适用于 "+core.PlatformLabel(entry.Platform)+"，不能在本机运行。").TextColor(c.Theme().Warning)
	}
	componentFoldKeyed(c, "catalog-details-"+string(entry.Kind), "版本与校验 · "+entry.Title, func() {
		ui.Text(c, "版本 "+entry.Revision).Selectable()
		ui.Text(c, catalogSize(entry.Bytes)).TextColor(c.Theme().TextMuted)
		ui.Text(c, "SHA256 "+entry.SHA256).Selectable().TextColor(c.Theme().TextMuted)
	})
}

func (s *viewState) catalogView(c *ui.Context) {
	catalogReferences(c, core.CurrentPlatform())
}

func catalogReferences(c *ui.Context, platform string) {
	componentFold(c, "其他准备方式", func() {
		for _, entry := range core.CatalogForPlatform(platform) {
			if entry.Downloadable() {
				continue
			}
			ui.Column(c.Key(entry.ID)).Gap(c.Theme().Space(1)).Padding(c.Theme().Space(2)).Children(func() {
				ui.Text(c, entry.Title).Bold()
				ui.Text(c, entry.Reason).TextColor(c.Theme().TextMuted)
				ui.Text(c, entry.URL).Selectable().TextColor(c.Theme().TextMuted)
				if entry.Doc != "" {
					ui.Text(c, "准备说明："+entry.Doc).Selectable().TextColor(c.Theme().TextMuted)
				}
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
