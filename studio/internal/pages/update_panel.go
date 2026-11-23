package pages

import (
	"context"
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
	t := c.Theme()
	color := stateColor(c, kind)
	b := components.Button(c, "", components.ButtonGhost).
		Padding(t.Space(.75), t.Space(1.5)).Radius(t.Space(4.5)).
		Label(label)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Space(.75)).Children(func() {
			el := components.Icon(c, icon).Size(t.Space(4), t.Space(4)).TextColor(color)
			if animated && !c.Preferences().ReduceMotion {
				phase := el.Loop("update-activity", 1400*time.Millisecond, ui.Linear)
				el.Opacity(.65 + .35*float32((math.Sin(float64(phase)*2*math.Pi)+1)/2))
			}
			ui.Text(c, label).FontSize(tokens(c).Type.Caption).SingleLine().TextColor(color)
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
		})
	})
}

func (s *viewState) updateDialog(c *ui.Context) {
	st := s.currentUpdateState()
	t := tokens(c)
	c.Key("update-dialog")
	components.Dialog(c, &s.updateDialogOpen, components.DialogOptions{
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

				if st.Release != nil {
					notes := summarizeReleaseNotes(st.Release.Notes)
					ui.Scroll(c.Key("update-release-notes")).FillWidth().MaxHeight(160).
						Padding(t.Density.Row).Radius(t.Radius.Inset).
						Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).
						Children(func() {
							ui.Text(c, notes).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
						})
				}

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
						if st.Release != nil && st.Release.ReleaseURL != "" {
							if outlineButton(c, "打开发布页").Clicked() {
								s.openUpdateURL(c, st.Release.ReleaseURL)
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
							if primaryButton(c, "安装并重启").Clicked() {
								s.triggerUpdateApply(false)
							}
						}
						if button(c, "稍后安装").Clicked() {
							s.updateDialogOpen = false
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
						if st.Release != nil && st.Release.ReleaseURL != "" {
							if outlineButton(c, "打开发布页").Clicked() {
								s.openUpdateURL(c, st.Release.ReleaseURL)
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

func (s *viewState) applyUpdateState(next core.UpdateState) {
	if next.State == "" {
		return
	}
	if s.updateState.State == "" || next.Seq >= s.updateState.Seq {
		s.updateState = next
	}
}

func (s *viewState) triggerUpdateCheck() {
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
		st, _ := applyFn(ctx, confirmStop)
		s.runOnUI(func() {
			s.applyUpdateState(st)
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
	if !strings.HasPrefix(rawURL, "https://") {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast("已在浏览器打开发布页（模拟）")
		return
	}
	c.OpenURLThen(rawURL, func(err error) {
		if err != nil {
			s.enqueueStatusToast("无法打开发布页")
			return
		}
		s.enqueueStatusToast("已在浏览器打开发布页")
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
