//go:build ui_debug && !release

package pages

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func (p *previewSession) startPrototypeInstall(family core.EngineFamilyCatalog, rel core.EngineRelease, build core.EnginePlatformBuild) {
	if p.isProtoFamilyInstalling(family.ID) {
		return
	}
	s := p.state
	total := build.TotalBytes()
	if total <= 0 {
		total = 48 << 20
	}
	taskID := p.nextID("proto-" + family.ID)
	task := core.DownloadTask{
		TaskId:          taskID,
		Kind:            "engine",
		DisplayName:     family.Name + " · " + rel.Version,
		State:           "downloading",
		Stage:           "downloading",
		SourceUrl:       build.PrimaryURL(),
		OutName:         family.ID + "-" + rel.Version,
		OutDir:          "/tmp/preview/engines",
		TotalBytes:      previewValue(total),
		DownloadedBytes: total / 4,
		BytesPerSecond:  previewValue(18.5 * 1024 * 1024),
		StartedAt:       p.timestamp(),
	}
	s.downloads = append(s.downloads, task)
	fail := p.takeFailure()

	nextStage := "preparing"
	if build.AcquireMode == core.EngineAcquireDirect {
		nextStage = "extracting"
	}
	group := "proto-install-" + taskID
	p.after(group, 350*time.Millisecond, func() {
		idx := p.findProtoTaskIndex(taskID)
		if idx < 0 || terminal(s.downloads[idx].State) {
			return
		}
		if fail {
			s.downloads[idx].State = "failed"
			s.downloads[idx].Stage = ""
			s.downloads[idx].BytesPerSecond = nil
			s.downloads[idx].Error = &core.ApiError{Code: "download_failed", Message: "模拟安装失败，请重试。"}
			s.downloads[idx].FinishedAt = previewValue(p.timestamp())
			p.notice = "模拟安装 " + family.Name + " 失败。"
			s.enqueueStatusToast(family.Name + " 安装失败")
			return
		}
		s.downloads[idx].Stage = nextStage
		s.downloads[idx].DownloadedBytes = total * 4 / 5
		s.downloads[idx].BytesPerSecond = nil

		p.after(group, 350*time.Millisecond, func() {
			doneIdx := p.findProtoTaskIndex(taskID)
			if doneIdx < 0 || terminal(s.downloads[doneIdx].State) {
				return
			}
			s.downloads[doneIdx].State = "done"
			s.downloads[doneIdx].Stage = ""
			s.downloads[doneIdx].DownloadedBytes = total
			s.downloads[doneIdx].BytesPerSecond = nil
			s.downloads[doneIdx].FinishedAt = previewValue(p.timestamp())
			p.registerProtoInstalledEngine(family, rel)
			p.notice = "已模拟安装 " + family.Name + "（" + rel.Version + "）。"
			s.enqueueStatusToast(family.Name + " 已安装")
		})
	})
}

func (p *previewSession) findProtoTaskIndex(taskID string) int {
	for i, task := range p.state.downloads {
		if task.TaskId == taskID {
			return i
		}
	}
	return -1
}

func (p *previewSession) registerProtoInstalledEngine(family core.EngineFamilyCatalog, rel core.EngineRelease) {
	s := p.state
	engineName := rel.Version + " · " + family.Name
	exists := false
	for _, eng := range s.registry.Engines {
		if eng.Family == family.ID && eng.Name == engineName {
			exists = true
			break
		}
	}
	if !exists {
		s.registry.Engines = append(s.registry.Engines, core.EngineAsset{
			Name:   engineName,
			Family: family.ID,
			Source: "模拟安装",
		})
	}
	if s.checks.Engines == nil {
		s.checks.Engines = map[string]core.EngineEligibility{}
	}
	s.checks.Engines[engineName] = core.EngineEligibility{
		State:   core.EngineCheckEligible,
		Family:  family.ID,
		Version: rel.Version,
	}
}

func (p *previewSession) downloadsPrototypeDetailDrawer(c *ui.Context) {
	st := p.ensurePrototypeState()
	c.Key("proto-engine-detail-drawer")
	if !st.drawerOpen {
		drawer(c, &st.drawerOpen, drawerOptions{Label: "引擎详情", Title: "引擎详情"}, nil)
		return
	}
	reg := previewPrototypeCatalog(p.protoIncludeCustomEngine)
	family, ok := reg.FamilyByID(st.drawerFamilyID)
	if !ok || len(family.Releases) == 0 {
		st.drawerOpen = false
		drawer(c, &st.drawerOpen, drawerOptions{Label: "引擎详情", Title: "引擎详情"}, nil)
		return
	}

	hostPlatform := st.effectiveHostPlatform()
	hasStable, hasNonStable := protoChannelMix(family.Releases)
	releases := protoFilterReleases(family.Releases, st.drawerStableOnly && hasStable)
	rel := protoPickRelease(st, family.ID, releases)
	platforms := rel.Platforms()
	activePlatform := protoResolvePlatform(st.selectedPlatform[family.ID], hostPlatform, platforms)
	builds := rel.BuildsForPlatform(activePlatform)
	build := protoPickBuildForID(st.selectedBuild[family.ID], builds)

	drawer(c, &st.drawerOpen, drawerOptions{
		Label: "引擎详情",
		Title: family.Name,
		Width: c.Theme().Space(145),
	}, func(ui.Element) {
		t := tokens(c)
		ui.Scroll(c.Key("proto-drawer-scroll")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				p.drawerConfigSection(c, st, family, releases, rel, platforms, activePlatform, builds, build, hostPlatform, hasStable && hasNonStable)
				p.drawerArtifactsSection(c, build)
				p.drawerInstallSection(c, st, family, rel, build)
			})
		})
	})
}

func (p *previewSession) drawerConfigSection(
	c *ui.Context,
	st *downloadsProtoState,
	family core.EngineFamilyCatalog,
	releases []core.EngineRelease,
	rel core.EngineRelease,
	platforms []string,
	activePlatform string,
	builds []core.EnginePlatformBuild,
	build core.EnginePlatformBuild,
	hostPlatform string,
	showStableToggle bool,
) {
	t := tokens(c)
	ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
		if showStableToggle {
			components.Checkbox(c, &st.drawerStableOnly, "仅正式版")
		}
		if len(releases) > 1 {
			ui.Box(c.Key("proto-drawer-ver-" + family.ID)).Children(func() {
				chosenID := rel.ID
				sel := components.Select(c, &chosenID)
				sel.Trigger.Label("切换版本").Value(rel.Version + " · " + rel.Distribution).Children(func() {
					ui.Text(c, rel.Version+" · "+rel.Distribution).SingleLine()
				})
				sel.Popup(func(ui.Element) {
					components.ScrollView(c, components.ScrollViewOptions{}).Label("版本列表").MaxHeight(220).Children(func() {
						for _, candidate := range releases {
							itemLabel := protoReleaseOptionLabel(candidate)
							sel.Item(candidate.ID).Label(itemLabel).Children(func() {
								ui.Text(c, itemLabel).SingleLine()
							})
						}
					})
				})
				if sel.Trigger.Changed() {
					st.selectedRelease[family.ID] = chosenID
					st.selectedBuild[family.ID] = ""
				}
			})
		}
		if len(platforms) > 1 {
			ui.Box(c.Key("proto-drawer-plat-" + family.ID)).Children(func() {
				chosenPlat := activePlatform
				sel := components.Select(c, &chosenPlat)
				sel.Trigger.Label("选择平台").Value(core.PlatformLabel(activePlatform)).Children(func() {
					ui.Text(c, core.PlatformLabel(activePlatform)).SingleLine()
				})
				sel.Popup(func(ui.Element) {
					components.ScrollView(c, components.ScrollViewOptions{}).Label("平台列表").MaxHeight(220).Children(func() {
						for _, plat := range platforms {
							platLabel := core.PlatformLabel(plat)
							sel.Item(plat).Label(platLabel).Children(func() {
								ui.Text(c, platLabel).SingleLine()
							})
						}
					})
				})
				if sel.Trigger.Changed() {
					st.selectedPlatform[family.ID] = chosenPlat
					st.selectedBuild[family.ID] = ""
				}
			})
		}
		if len(builds) > 1 {
			opts := make([]components.NativeSelectOption[string], 0, len(builds))
			for _, candidate := range builds {
				label := candidate.Variant
				if label == "" {
					label = candidate.ID
				}
				opts = append(opts, components.NativeSelectOption[string]{Value: candidate.ID, Label: label})
			}
			chosenBuild := build.ID
			segmentedControl(c, "选择变体", &chosenBuild, opts)
			if chosenBuild != build.ID {
				st.selectedBuild[family.ID] = chosenBuild
			}
		}

		keyValueRow(c, "版本状态", core.ChannelLabel(rel.Channel), false)
		keyValueRow(c, "发行来源", rel.Distribution, false)
		keyValueRow(c, "目标平台", core.PlatformLabel(build.Platform), false)
		if build.Variant != "" {
			keyValueRow(c, "构建变体", build.Variant, false)
		}
		keyValueRow(c, "获取方式", core.AcquireModeLabel(build.AcquireMode), false)
		if total := build.TotalBytes(); total > 0 {
			keyValueRow(c, "总大小", catalogHumanSize(total), true)
		}
		if build.Platform != "any" && build.Platform != hostPlatform {
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: "当前所选平台与本机（" + core.PlatformLabel(hostPlatform) + "）不同，无法在本机直接运行。",
			})
		}
	})
}

func (p *previewSession) drawerArtifactsSection(c *ui.Context, build core.EnginePlatformBuild) {
	t := tokens(c)
	sectionTitle := "下载文件"
	if len(build.Artifacts) == 1 && build.Artifacts[0].Role == "release_page" {
		sectionTitle = "下载地址"
	}
	heading(c, sectionTitle, 3)

	for i, art := range build.Artifacts {
		index, artifact := i, art
		title := artifact.Label
		if title == "" {
			title = artifact.Filename
		}
		applyMaterial(c, ui.Column(c.Key(fmt.Sprintf("proto-art-%s-%d", build.ID, index))).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
			ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, title).Bold()
				if artifact.Bytes > 0 {
					badge(c, catalogHumanSize(artifact.Bytes), stateNeutral, badgeOptions{Style: badgeOutline, Size: sizeSM})
				}
			})
			ui.Text(c, artifact.URL).Selectable().FontSize(t.Type.Caption)
			if artifact.Role != "release_page" {
				shaLine := "SHA-256：未提供"
				if sha := strings.TrimSpace(artifact.SHA256); sha != "" {
					shaLine = "SHA-256：" + sha
				}
				ui.Text(c, shaLine).Selectable().FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
			}
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				outlineButton(c, "复制地址").Label(fmt.Sprintf("复制地址 · %s · %d", build.ID, index)).OnClick(func() {
					p.copyProtoText("下载地址", artifact.URL)
				})
				if sha := strings.TrimSpace(artifact.SHA256); sha != "" {
					outlineButton(c, "复制 SHA-256").Label(fmt.Sprintf("复制 SHA-256 · %s · %d", build.ID, index)).OnClick(func() {
						p.copyProtoText("SHA-256", sha)
					})
				}
				outlineButton(c, "打开链接").Label(fmt.Sprintf("打开链接 · %s · %d", build.ID, index)).OnClick(func() {
					p.openProtoURL(title, artifact.URL)
				})
			})
		})
	}
}

func (p *previewSession) drawerInstallSection(
	c *ui.Context,
	st *downloadsProtoState,
	family core.EngineFamilyCatalog,
	rel core.EngineRelease,
	build core.EnginePlatformBuild,
) {
	t := tokens(c)
	note := strings.TrimSpace(build.Note)
	cmd := strings.TrimSpace(build.InstallCommand)
	if note != "" || cmd != "" {
		heading(c, "安装说明", 3)
		if note != "" {
			ui.Text(c, note).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
		}
		if cmd != "" {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Inset), materialInset).Children(func() {
				ui.Text(c, cmd).Selectable().FontSize(t.Type.Caption)
				outlineButton(c, "复制命令").OnClick(func() {
					p.copyProtoText("安装命令", cmd)
				})
			})
		}
	}

	ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		if !family.ExternalApp {
			if p.isProtoFamilyInstalling(family.ID) {
				button(c, "安装中…").Label("安装此版本 · " + family.ID).Disabled(true)
			} else {
				primaryButton(c, "安装此版本").Label("安装此版本 · " + family.ID).Disabled(p.state.busy).OnClick(func() {
					p.startPrototypeInstall(family, rel, build)
				})
			}
		} else {
			primaryButton(c, "打开下载页").Label("打开下载页 · " + family.ID).OnClick(func() {
				p.openProtoURL(family.Name, build.PrimaryURL())
			})
		}
		if family.SupportsWizard {
			outlineButton(c, "注册本地引擎").Label("注册本地引擎 · " + family.ID).OnClick(func() {
				st.drawerOpen = false
				p.state.openLocalEngineWizard(family.ID)
			})
		}
		if relURL := protoReleaseLink(family, rel); relURL != "" && relURL != build.PrimaryURL() {
			ghostButton(c, "打开发布页").OnClick(func() {
				p.openProtoURL(family.Name+" "+rel.Version, relURL)
			})
		}
	})
}

func (p *previewSession) copyProtoText(label, value string) {
	if value == "" {
		return
	}
	p.notice = "已复制 " + label + "（模拟）：" + value
	p.state.enqueueStatusToast("已复制 " + label + "（模拟）")
}

func (p *previewSession) openProtoURL(label, rawURL string) {
	if !strings.HasPrefix(rawURL, "https://") {
		return
	}
	p.notice = "已在浏览器打开 " + label + "（模拟）：" + rawURL
	p.state.enqueueStatusToast("已在浏览器打开 " + label + "（模拟）")
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

func (p *previewSession) isProtoFamilyInstalling(familyID string) bool {
	prefix := "preview-proto-" + familyID + "-"
	for _, task := range p.state.downloads {
		if task.State != "downloading" {
			continue
		}
		if strings.HasPrefix(task.TaskId, prefix) {
			return true
		}
		if familyID == "llama-cpp" && task.Kind == "engine" && !strings.HasPrefix(task.TaskId, "preview-proto-") {
			return true
		}
	}
	return false
}

func protoChannelMix(releases []core.EngineRelease) (hasStable, hasNonStable bool) {
	for _, rel := range releases {
		if rel.Channel == core.ReleaseStable || rel.Channel == core.ReleaseLatest {
			hasStable = true
		} else {
			hasNonStable = true
		}
	}
	return hasStable, hasNonStable
}

func protoFilterReleases(releases []core.EngineRelease, stableOnly bool) []core.EngineRelease {
	if !stableOnly {
		return releases
	}
	var out []core.EngineRelease
	for _, rel := range releases {
		if rel.Channel == core.ReleaseStable || rel.Channel == core.ReleaseLatest {
			out = append(out, rel)
		}
	}
	if len(out) == 0 {
		return releases
	}
	return out
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

func protoResolvePlatform(saved, host string, platforms []string) string {
	for _, candidate := range []string{saved, host, "any"} {
		if candidate != "" && slices.Contains(platforms, candidate) {
			return candidate
		}
	}
	return platforms[0]
}

func protoPickBuildForPlatform(st *downloadsProtoState, familyID string, rel core.EngineRelease, platform string) core.EnginePlatformBuild {
	builds := rel.BuildsForPlatform(platform)
	if len(builds) == 0 {
		return rel.Builds[0]
	}
	return protoPickBuildForID(st.selectedBuild[familyID], builds)
}

func protoPickBuildForID(wantID string, builds []core.EnginePlatformBuild) core.EnginePlatformBuild {
	for _, b := range builds {
		if b.ID == wantID {
			return b
		}
	}
	return builds[0]
}

func protoReleaseOptionLabel(rel core.EngineRelease) string {
	label := rel.Version + " · " + rel.Distribution + " · " + core.ChannelLabel(rel.Channel)
	if rel.Recommended {
		label += " · 推荐"
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
