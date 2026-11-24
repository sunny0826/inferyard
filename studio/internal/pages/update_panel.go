package pages

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
)

var openUpdatePathFunc = core.OpenPathInOS

func (s *viewState) currentUpdateState() core.UpdateState {
	st := s.updateState
	if st.CurrentVersion == "" {
		st.CurrentVersion = core.Version
	}
	if st.State == "" {
		st.State = core.UpdateIdle
	}
	return st
}

func (s *viewState) effectiveUpdateWorkloads() core.UpdateActiveWorkloads {
	w := s.updateState.Workloads
	q := s.queueActivity()
	if q.active() {
		active := q.running + q.waiting
		if active > w.ActiveRuns {
			w.ActiveRuns = active
		}
	}
	if n := s.activeDownloadCount(); n > w.ActiveDownloads {
		w.ActiveDownloads = n
	}
	return w
}

func (s *viewState) updateHeaderBadge(c *ui.Context) {
	st := s.currentUpdateState()
	label, kind, icon, animated := updateBadgePresentation(st)
	if label == "" {
		return
	}
	theme := c.Theme()
	t := tokens(c)
	color := stateColor(c, kind)
	b := components.Button(c, "", components.ButtonGhost).
		Padding(t.Density.Tight, t.Density.Row).Radius(t.Radius.Full).
		Label(label)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			el := components.Icon(c, icon).Size(theme.Space(4), theme.Space(4)).TextColor(color)
			if animated && !c.Preferences().ReduceMotion {
				phase := el.Loop("update-activity", 1400*time.Millisecond, ui.Linear)
				el.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
			}
			ui.Text(c, label).FontSize(t.Type.Caption).SingleLine().TextColor(color)
		})
	})
	if b.Clicked() {
		s.updateDialogOpen = true
	}
}

func (s *viewState) updateSettingsView(c *ui.Context) {
	st := s.currentUpdateState()
	t := tokens(c)
	card(c, "软件更新", func() {
		keyValueRow(c, "当前版本", "v"+strings.TrimPrefix(st.CurrentVersion, "v"), true)
		statusText, statusKind := updateSettingsStatus(st)
		if statusText != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, statusText).FontSize(t.Type.Caption).TextColor(stateColor(c, statusKind))
			})
		}
		checkingOrBusy := st.State == core.UpdateChecking || st.State == core.UpdateDownloading ||
			st.State == core.UpdateVerifying || st.State == core.UpdateApplying
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if button(c, "检查更新").Disabled(checkingOrBusy).Clicked() {
				s.updateDialogOpen = true
				s.triggerUpdateCheck()
			}
			if st.Release != nil || st.State == core.UpdateReadyToApply || st.State == core.UpdateError {
				if outlineButton(c, "查看更新").Clicked() {
					s.updateDialogOpen = true
				}
			}
			if updateExternalButton(c, "查看更新日志").Clicked() {
				s.openUpdateURL(c, updateChangelogURL(st))
			}
		})
	})
}

func updateExternalButton(c *ui.Context, label string) ui.Element {
	t := tokens(c)
	b := components.Button(c, "", components.ButtonOutline).
		Padding(t.Density.Row, t.Density.Stack).Radius(t.Radius.Control).
		MaxWidthPercent(100).AlignSelf(ui.Start).Label(label)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
			components.Icon(c, lucide.ArrowUpRight).Size(13, 13).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
		})
	})
	return b
}

func (s *viewState) updateChangelogCard(c *ui.Context, st core.UpdateState) {
	releaseURL := updateReleaseURL(st.Release)
	if releaseURL == "" {
		return
	}
	t := tokens(c)
	ver := strings.TrimPrefix(strings.TrimSpace(st.Release.Version), "v")
	title := "更新日志"
	if ver != "" {
		title = "v" + ver + " 更新日志"
	}
	subtitle := "在 GitHub Release 查看完整变更说明"
	if published := formatReleasePublishedDate(st.Release.PublishedAt); published != "" {
		subtitle = "发布于 " + published + " · 在 GitHub Release 查看"
	}
	applyMaterial(c, ui.Row(c.Key("update-changelog-card")).FillWidth().Wrap().
		AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Stack), materialSunken).
		Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				components.Icon(c, lucide.FileText).Size(15, 15).TextColor(t.Color.FgSecondary).Role(ui.RoleNone)
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Text(c, title).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
					ui.Text(c, subtitle).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				})
			})
			if updateExternalButton(c, "前往 Release").Clicked() {
				s.openUpdateURL(c, releaseURL)
			}
		})
}

func (s *viewState) updateDialog(c *ui.Context) {
	st := s.currentUpdateState()
	t := tokens(c)
	c.Key("update-dialog")
	studioDialog(c, &s.updateDialogOpen, components.DialogOptions{
		Label:                "软件更新",
		DisableBackdropClose: st.State == core.UpdateApplying,
		DisableEscapeClose:   st.State == core.UpdateApplying,
	}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(125)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, "软件更新", 2).Grow(1)
					if st.Release != nil {
						badge(c, "v"+strings.TrimPrefix(st.CurrentVersion, "v")+" → v"+st.Release.Version, stateActive, badgeOptions{Size: sizeSM})
					} else {
						badge(c, "v"+strings.TrimPrefix(st.CurrentVersion, "v"), stateNeutral, badgeOptions{Size: sizeSM})
					}
				})

				s.updateChangelogCard(c, st)

				workloads := s.effectiveUpdateWorkloads()
				if workloads.Any() && (st.State == core.UpdateAvailable || st.State == core.UpdateDownloading || st.State == core.UpdateVerifying || st.State == core.UpdateReadyToApply) {
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateWarning,
						Size:    bannerCompact,
						Icon:    lucide.TriangleAlert,
						Message: formatUpdateWorkloadWarning(workloads),
					})
				}

				switch st.State {
				case core.UpdateIdle, core.UpdateUpToDate:
					msg := "点击检查是否有新版本可用。"
					if st.State == core.UpdateUpToDate {
						msg = "当前已是最新版本。"
					}
					ui.Text(c, msg).TextColor(t.Color.FgSecondary)
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if primaryButton(c, "检查更新").Clicked() {
							s.triggerUpdateCheck()
						}
						if updateExternalButton(c, "查看更新日志").Clicked() {
							s.openUpdateURL(c, core.DefaultReleasesURL)
						}
						if button(c, "关闭").Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateChecking:
					progressBar(c, -1, "检查更新进度")
					ui.Text(c, "正在检查新版本…").TextColor(t.Color.FgSecondary)
					if button(c, "关闭").Clicked() {
						s.updateDialogOpen = false
					}
				case core.UpdateAvailable:
					if !st.CanSelfUpdate && st.SelfUpdateBlock != "" {
						noticeBanner(c, noticeBannerOptions{
							Kind:    stateInfo,
							Size:    bannerCompact,
							Message: st.SelfUpdateBlock,
						})
					}
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if st.CanSelfUpdate && st.Release != nil && st.Release.AssetURL != "" && st.Release.ChecksumsURL != "" {
							if primaryButton(c, "立即更新").Clicked() {
								s.triggerUpdateStart()
							}
						}
						if button(c, "关闭").Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateDownloading:
					progressBar(c, progressValue(st.DownloadedBytes, st.TotalBytes), "更新下载进度")
					ui.Text(c, formatUpdateProgress(st)).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if button(c, "取消下载").Clicked() {
							s.triggerUpdateCancel()
						}
						if button(c, "关闭").Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateVerifying:
					progressBar(c, -1, "校验更新安装包")
					ui.Text(c, "正在校验 SHA-256 校验和…").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if button(c, "取消").Clicked() {
						s.triggerUpdateCancel()
					}
				case core.UpdateReadyToApply:
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if workloads.Any() {
							if dangerButton(c, "停止任务并安装").Clicked() {
								s.triggerUpdateApply(true)
							}
						} else {
							if primaryButton(c, "更新并重启").Clicked() {
								s.triggerUpdateApply(false)
							}
						}
						if button(c, "稍后").Clicked() {
							s.dismissUpdateReadyPrompt(st)
						}
					})
				case core.UpdateApplying:
					progressBar(c, -1, "安装更新进度")
					ui.Text(c, "正在安装更新并准备重启…").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				case core.UpdateError:
					errMsg := st.ErrorMessage
					if errMsg == "" {
						errMsg = "更新失败，请重试或前往发布页下载。"
					}
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateDanger,
						Size:    bannerCompact,
						Icon:    lucide.CircleAlert,
						Message: errMsg,
					})
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if primaryButton(c, "重试").Clicked() {
							s.triggerUpdateRetry()
						}
						if st.FallbackPath != "" {
							if outlineButton(c, "打开安装包").Clicked() {
								s.openUpdatePath(st.FallbackPath)
							}
						}
						if button(c, "关闭").Clicked() {
							s.updateDialogOpen = false
						}
					})
				}
			})
		})
	})
}

func (s *viewState) updateReadyPrompt(c *ui.Context) {
	st := s.currentUpdateState()
	if st.State != core.UpdateReadyToApply || st.Release == nil || s.updateDialogOpen {
		return
	}
	ver := strings.TrimPrefix(st.Release.Version, "v")
	if ver == "" || s.updateReadyPromptDismissedVersion == ver {
		return
	}
	t := tokens(c)
	promptPad := t.Density.Section
	promptBtnRadius := concentricRadius(t.Radius.Modal, promptPad)
	ui.Overlay(c, func() {
		ui.Box(c).Fill().PassThrough().AlignItems(ui.End).Justify(ui.End).Padding(t.Density.Section).Children(func() {
			card := applyShadow(
				ui.Column(c.Key("update-ready-prompt")).
					Width(340).MaxWidthPercent(90).
					Padding(promptPad).Gap(t.Density.Stack).Radius(t.Radius.Modal).
					Background(t.Color.Elevated).
					Border(1, t.Color.BorderDefault),
				t.Elevation.LG,
			)
			card.Children(func() {
				ui.Text(c, "发现新版本").Bold().FontSize(t.Type.CardTitle).TextColor(t.Color.FgPrimary)
				ui.Text(c, fmt.Sprintf("版本 %s 已可更新，重启后生效。", ver)).
					FontSize(t.Type.Body).TextColor(t.Color.FgSecondary)
				if releaseURL := updateReleaseURL(st.Release); releaseURL != "" {
					notesBtn := components.Button(c, "", components.ButtonGhost).
						Padding(0).Radius(promptBtnRadius).AlignSelf(ui.Start).Label("查看更新日志")
					notesBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Hairline).Children(func() {
							ui.Text(c, "查看更新日志").FontSize(t.Type.Body).
								TextColor(t.Color.Accent).Underline().UnderlineOffset(2)
							components.Icon(c, lucide.ArrowUpRight).Size(13, 13).TextColor(t.Color.Accent).Role(ui.RoleNone)
						})
					})
					if notesBtn.Clicked() {
						s.openUpdateURL(c, releaseURL)
					}
				}
				ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Stack).Padding(t.Density.Row, 0, 0, 0).Children(func() {
					laterBtn := components.Button(c, "", components.ButtonGhost).
						Padding(t.Density.Row, t.Density.Stack).Radius(promptBtnRadius).Label("稍后")
					laterBtn.Children(func() {
						ui.Text(c, "稍后").FontSize(t.Type.Body).TextColor(t.Color.FgSecondary)
					})
					if laterBtn.Clicked() {
						s.dismissUpdateReadyPrompt(st)
					}
					restartBtn := components.Button(c, "更新并重启", components.ButtonPrimary).
						Padding(t.Density.Row, t.Density.Section).Radius(promptBtnRadius)
					if restartBtn.Clicked() {
						if s.effectiveUpdateWorkloads().Any() {
							s.updateDialogOpen = true
						} else {
							s.triggerUpdateApply(false)
						}
					}
				})
			})
		})
	})
}

func (s *viewState) dismissUpdateReadyPrompt(st core.UpdateState) {
	if st.Release != nil {
		if ver := strings.TrimPrefix(st.Release.Version, "v"); ver != "" {
			s.updateReadyPromptDismissedVersion = ver
		}
	}
	s.updateDialogOpen = false
}

func (s *viewState) applyUpdateState(next core.UpdateState) {
	if next.State == "" {
		return
	}
	if s.updateState.State == "" || next.Seq >= s.updateState.Seq {
		s.updateState = next
	}
}

func (s *viewState) triggerUpdateCheck() {
	s.updateReadyPromptDismissedVersion = ""
	if s.simulate(previewUpdateCheck{}) {
		return
	}
	checkFn := s.updateCheckFn
	if checkFn == nil && s.service != nil {
		checkFn = s.service.CheckForUpdate
	}
	if checkFn == nil {
		return
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		st, _ := checkFn(ctx)
		s.runOnUI(func() {
			s.applyUpdateState(st)
		})
	}()
}

func (s *viewState) triggerUpdateStart() {
	s.updateReadyPromptDismissedVersion = ""
	if s.simulate(previewUpdateStart{}) {
		return
	}
	startFn := s.updateStartFn
	if startFn == nil && s.service != nil {
		startFn = s.service.StartOneClickUpdate
	}
	if startFn != nil {
		startFn()
	}
}

func (s *viewState) triggerUpdateCancel() {
	if s.simulate(previewUpdateCancel{}) {
		return
	}
	cancelFn := s.updateCancelFn
	if cancelFn == nil && s.service != nil {
		cancelFn = s.service.CancelUpdate
	}
	if cancelFn != nil {
		s.applyUpdateState(cancelFn())
	}
}

func (s *viewState) triggerUpdateApply(confirmStop bool) {
	if s.simulate(previewUpdateApply{confirmStopWorkloads: confirmStop}) {
		return
	}
	applyFn := s.updateApplyFn
	if applyFn == nil && s.service != nil {
		applyFn = s.service.ApplyUpdate
	}
	if applyFn == nil {
		return
	}
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		st, err := applyFn(ctx, confirmStop)
		s.runOnUI(func() {
			s.applyUpdateState(st)
			if errors.Is(err, core.ErrUpdateWorkloadActive) || st.State == core.UpdateError {
				s.updateDialogOpen = true
			}
		})
	}()
}

func (s *viewState) triggerUpdateRetry() {
	if s.simulate(previewUpdateRetry{}) {
		return
	}
	st := s.currentUpdateState()
	if st.Release != nil && st.Release.AssetURL != "" && st.Release.ChecksumsURL != "" && !isUpdateCheckError(st.ErrorCode) {
		s.triggerUpdateStart()
		return
	}
	s.triggerUpdateCheck()
}

func (s *viewState) openUpdateURL(c *ui.Context, rawURL string) {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "https://") {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast("已在浏览器打开 Release 更新日志（模拟）")
		return
	}
	c.OpenURLThen(rawURL, func(err error) {
		if err != nil {
			s.enqueueStatusToast("无法打开 Release 更新日志")
			return
		}
		s.enqueueStatusToast("已在浏览器打开 Release 更新日志")
	})
}

func (s *viewState) openUpdatePath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast("已打开安装包（模拟）")
		return
	}
	if err := openUpdatePathFunc(path); err != nil {
		s.enqueueStatusToast("无法打开安装包")
		return
	}
	s.enqueueStatusToast("已打开安装包")
}
