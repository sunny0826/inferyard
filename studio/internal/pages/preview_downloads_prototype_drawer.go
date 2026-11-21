//go:build ui_debug && !release

package pages

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (p *previewSession) downloadsPrototypeDetailDrawer(c *ui.Context) {
	st := p.ensurePrototypeState()
	c.Key("proto-engine-detail-drawer")
	if !st.drawerOpen {
		drawer(c, &st.drawerOpen, drawerOptions{Label: "引擎版本与下载地址详情", Title: "引擎版本与下载地址详情"}, nil)
		return
	}
	reg := previewPrototypeCatalog(p.protoIncludeCustomEngine)
	family, ok := reg.FamilyByID(st.drawerFamilyID)
	if !ok || len(family.Releases) == 0 {
		st.drawerOpen = false
		drawer(c, &st.drawerOpen, drawerOptions{Label: "引擎版本与下载地址详情", Title: "引擎版本与下载地址详情"}, nil)
		return
	}
	rel := protoPickReleaseByID(family.Releases, st.drawerReleaseID)
	build := protoPickBuild(rel.Builds, st.drawerBuildID)
	st.drawerReleaseID = rel.ID
	st.drawerBuildID = build.ID

	drawer(c, &st.drawerOpen, drawerOptions{
		Label: "引擎版本与下载地址详情",
		Title: family.Name + " · " + rel.Version,
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("proto-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				if len(family.Releases) > 1 {
					ui.Box(c.Key("proto-drawer-ver-" + family.ID)).Children(func() {
						chosenID := rel.ID
						sel := components.Select(c, &chosenID)
						sel.Trigger.Label("抽屉切换版本").Value(rel.Version + " · " + rel.Distribution).Children(func() {
							ui.Text(c, rel.Version+" · "+rel.Distribution).SingleLine()
						})
						sel.Popup(func(ui.Element) {
							components.ScrollView(c, components.ScrollViewOptions{}).Label("抽屉版本列表").MaxHeight(220).Children(func() {
								for _, candidate := range family.Releases {
									itemLabel := protoReleaseOptionLabel(candidate)
									sel.Item(candidate.ID).Label(itemLabel).Children(func() {
										ui.Text(c, itemLabel).SingleLine()
									})
								}
							})
						})
						if sel.Trigger.Changed() {
							st.selectedRelease[family.ID] = chosenID
							st.drawerReleaseID = chosenID
							nextRel := protoPickReleaseByID(family.Releases, chosenID)
							st.drawerBuildID = protoMatchPlatformBuild(nextRel.Builds, build.Platform).ID
						}
					})
				}

				keyValueRow(c, "版本与渠道", rel.Version+"（"+core.ChannelLabel(rel.Channel)+"）", false)
				keyValueRow(c, "发行来源", rel.Distribution, false)
				keyValueRow(c, "Revision", rel.Revision, false)
				keyValueRow(c, "平台与变体", core.PlatformLabel(build.Platform)+" · "+build.Variant, false)
				keyValueRow(c, "适用模型", core.ModelFormatLabel(family.ModelFormat), false)

				heading(c, "文件与校验清单", 3)
				for idx, art := range build.Artifacts {
					art := art
					ui.Column(c.Key(fmt.Sprintf("drawer-art-%s-%d", build.ID, idx))).FillWidth().Children(func() {
						applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Stack), materialInset).Children(func() {
							ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
								ui.Text(c, art.Label).Bold()
								badge(c, protoArtifactRoleLabel(art.Role), stateNeutral, badgeOptions{Style: badgeSoft, Size: sizeSM})
								if art.Version != "" {
									badge(c, art.Version, stateInfo, badgeOptions{Style: badgeOutline, Size: sizeSM})
								}
							})
							if art.Bytes > 0 {
								ui.Text(c, "大小："+catalogSize(art.Bytes)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
							}
							ui.Text(c, art.URL).Selectable().FontSize(t.Type.Caption)
							if art.SHA256 != "" {
								ui.Text(c, "SHA-256："+art.SHA256).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
							} else {
								ui.Text(c, "SHA-256：官方未固定单文件校验和（请在发布页核对）").FontSize(t.Type.Caption).TextColor(t.Color.FgTertiary)
							}
							ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
								outlineButton(c, "复制 URL").Label(fmt.Sprintf("复制文件 URL · %d", idx)).OnClick(func() {
									p.copyProtoText("文件地址", art.URL)
								})
								button(c, "打开链接").Label(fmt.Sprintf("打开文件链接 · %d", idx)).OnClick(func() {
									p.openProtoURL(art.Label, art.URL)
								})
								if art.SHA256 != "" {
									ghostButton(c, "复制 SHA-256").Label(fmt.Sprintf("复制校验和 · %d", idx)).OnClick(func() {
										p.copyProtoText("SHA-256", art.SHA256)
									})
								}
							})
						})
					})
				}

				heading(c, "准备与接入说明", 3)
				if build.Note != "" {
					ui.Text(c, build.Note).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				}
				if build.InstallCommand != "" {
					applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Stack), materialSunken).Children(func() {
						ui.Text(c, "参考准备命令").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
						ui.Text(c, build.InstallCommand).Selectable().FontSize(t.Type.Caption)
						outlineButton(c, "复制命令").OnClick(func() {
							p.copyProtoText("准备命令", build.InstallCommand)
						})
					})
				}
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if build.CanDirectDownloadOn(core.CurrentPlatform()) {
						primaryButton(c, "开始下载引擎").Disabled(p.state.busy || p.isProtoSourceDownloading(build.PrimaryURL())).OnClick(func() {
							p.state.acquireEngine(build.PrimaryURL(), "")
						})
					} else if build.PrimaryURL() != "" {
						button(c, "打开官方下载页").OnClick(func() {
							p.openProtoURL(family.Name, build.PrimaryURL())
						})
					}
					if family.SupportsWizard {
						outlineButton(c, "去注册本地引擎").OnClick(func() {
							st.drawerOpen = false
							p.state.openLocalEngineWizard(family.ID)
						})
					}
					ghostButton(c, "查看官方 Releases").OnClick(func() {
						p.openProtoURL(family.Name+" Releases", protoReleaseLink(family, rel))
					})
				})
			})
		})
	})
}

func (p *previewSession) copyProtoText(label, value string) {
	if value == "" {
		return
	}
	p.notice = "已模拟复制" + label + "：" + value
	p.state.enqueueStatusToast("已复制" + label + "（模拟）")
}

func (p *previewSession) openProtoURL(label, rawURL string) {
	if !strings.HasPrefix(rawURL, "https://") {
		return
	}
	p.notice = "已模拟打开链接：" + rawURL
	p.state.enqueueStatusToast("已在浏览器打开" + label + "（模拟）")
}

func (p *previewSession) isProtoFamilyInstalled(familyID string) bool {
	for _, eng := range p.state.registry.Engines {
		if eng.Family == familyID {
			return true
		}
		if familyID == "llama-cpp" && eng.Family == "" && strings.Contains(strings.ToLower(eng.Name), "llama") {
			return true
		}
	}
	return false
}

func (p *previewSession) isProtoSourceDownloading(sourceURL string) bool {
	if sourceURL == "" {
		return false
	}
	for _, task := range p.state.downloads {
		if task.State == "downloading" && task.SourceUrl == sourceURL {
			return true
		}
	}
	return false
}

func protoTargetPlatform(choice string) string {
	switch choice {
	case "", "current":
		return core.CurrentPlatform()
	case "all":
		return "all"
	default:
		return choice
	}
}

func protoPickRelease(st *downloadsProtoState, familyID string, releases []core.EngineRelease) core.EngineRelease {
	want := st.selectedRelease[familyID]
	for _, rel := range releases {
		if rel.ID == want {
			return rel
		}
	}
	st.selectedRelease[familyID] = releases[0].ID
	return releases[0]
}

func protoPickReleaseByID(releases []core.EngineRelease, id string) core.EngineRelease {
	for _, rel := range releases {
		if rel.ID == id {
			return rel
		}
	}
	return releases[0]
}

func protoPickBuild(builds []core.EnginePlatformBuild, id string) core.EnginePlatformBuild {
	for _, b := range builds {
		if b.ID == id {
			return b
		}
	}
	return builds[0]
}

func protoMatchPlatformBuild(builds []core.EnginePlatformBuild, platform string) core.EnginePlatformBuild {
	for _, b := range builds {
		if b.Platform == platform || b.Platform == "any" {
			return b
		}
	}
	return builds[0]
}

func protoReleaseOptionLabel(rel core.EngineRelease) string {
	label := rel.Version + " · " + rel.Distribution + " · " + core.ChannelLabel(rel.Channel)
	if rel.Recommended {
		label += "（推荐）"
	}
	return label
}

func protoReleaseLink(family core.EngineFamilyCatalog, rel core.EngineRelease) string {
	if rel.ReleaseURL != "" {
		return rel.ReleaseURL
	}
	if family.ReleasesURL != "" {
		return family.ReleasesURL
	}
	return family.HomepageURL
}

func protoChannelState(ch core.ReleaseChannel) stateKind {
	switch ch {
	case core.ReleaseStable, core.ReleaseLatest:
		return stateSuccess
	case core.ReleasePreRelease:
		return stateWarning
	default:
		return stateNeutral
	}
}

func protoArtifactRoleLabel(role string) string {
	switch role {
	case "engine":
		return "主程序"
	case "runtime":
		return "运行时"
	case "plugin":
		return "加速插件"
	case "source":
		return "源码归档"
	case "release_page":
		return "发布页面"
	default:
		return role
	}
}

func protoBuildMeta(rel core.EngineRelease, build core.EnginePlatformBuild) string {
	parts := []string{
		rel.Distribution,
		fmt.Sprintf("%d 个文件", len(build.Artifacts)),
	}
	if total := build.TotalBytes(); total > 0 {
		parts = append(parts, catalogHumanSize(total))
	}
	if build.HasSHA256() {
		parts = append(parts, "含 SHA-256 校验")
	}
	return joinParts(parts)
}
