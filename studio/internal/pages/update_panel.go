package pages

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
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
	label, kind, icon, animated := updateBadgePresentationFor(contextLocale(c), st)
	if label == "" {
		return
	}
	theme := c.Theme()
	t := tokens(c)
	color := stateColor(c, kind)
	b := components.Button(c, "", components.ButtonGhost).
		Height(max(float32(32), theme.Rem(2))).Padding(0, t.Density.Row, 0, max(4, t.Density.Row-2)).Radius(t.Radius.Control).
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
	card(c, tr(c, "update.title"), func() {
		keyValueRow(c, tr(c, "update.current_version"), "v"+strings.TrimPrefix(st.CurrentVersion, "v"), true)
		statusText, statusKind := updateSettingsStatusFor(contextLocale(c), st)
		if statusText != "" {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
				ui.Text(c, statusText).FontSize(t.Type.Caption).TextColor(stateColor(c, statusKind))
			})
		}
		checkingOrBusy := st.State == core.UpdateChecking || st.State == core.UpdateDownloading ||
			st.State == core.UpdateVerifying || st.State == core.UpdateApplying
		ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			if button(c, tr(c, "update.action.check")).Disabled(checkingOrBusy).Clicked() {
				s.updateDialogOpen = true
				s.triggerUpdateCheck()
			}
			if st.Release != nil || st.State == core.UpdateReadyToApply || st.State == core.UpdateError {
				if outlineButton(c, tr(c, "update.action.view")).Clicked() {
					s.updateDialogOpen = true
				}
			}
			if updateExternalButton(c, tr(c, "update.prompt.view_changelog")).Clicked() {
				s.openUpdateURL(c, updateChangelogURL(st))
			}
		})
	})
}

func updateExternalButton(c *ui.Context, label string) ui.Element {
	t := tokens(c)
	b := styleButtonSize(c, components.Button(c, "", components.ButtonOutline), sizeMD).Label(label)
	applyButtonOpticalPadding(b, t, sizeMD, buttonIconTrailing)
	b.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Tight).Children(func() {
			ui.Text(c, label).FontSize(t.Type.Body).TextColor(t.Color.FgPrimary).SingleLine()
			ui.Box(c).Size(14, 14).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				studioIcon(c, reicon.ArrowUpRight, t.Color.FgSecondary)
			})
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
	title := tr(c, "update.changelog.title")
	if ver != "" {
		title = trf(c, "update.changelog.title_version", "version", ver)
	}
	subtitle := tr(c, "update.changelog.subtitle")
	if published := formatReleasePublishedDate(st.Release.PublishedAt); published != "" {
		subtitle = trf(c, "update.changelog.subtitle_dated", "date", published)
	}
	applyMaterial(c, ui.Row(c.Key("update-changelog-card")).FillWidth().Wrap().
		AlignItems(ui.Center).Gap(t.Density.Row).Padding(t.Density.Stack), materialSunken).
		Children(func() {
			ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				studioIcon(c, reicon.FileText, t.Color.FgSecondary)
				ui.Column(c).Grow(1).MinWidth(0).Gap(t.Density.Hairline).Children(func() {
					ui.Text(c, title).Bold().FontSize(t.Type.Body).TextColor(t.Color.FgPrimary)
					ui.Text(c, subtitle).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				})
			})
			if updateExternalButton(c, tr(c, "update.action.open_release")).Clicked() {
				s.openUpdateURL(c, releaseURL)
			}
		})
}

func (s *viewState) updateDialog(c *ui.Context) {
	st := s.currentUpdateState()
	t := tokens(c)
	loc := contextLocale(c)
	c.Key("update-dialog")
	studioDialog(c, &s.updateDialogOpen, components.DialogOptions{
		Label:                tr(c, "update.title"),
		DisableBackdropClose: st.State == core.UpdateApplying,
		DisableEscapeClose:   st.State == core.UpdateApplying,
	}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(125)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, tr(c, "update.title"), 2).Grow(1)
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
						Icon:    reicon.TriangleAlert,
						Message: formatUpdateWorkloadWarningFor(loc, workloads),
					})
				}

				switch st.State {
				case core.UpdateIdle, core.UpdateUpToDate:
					msg := tr(c, "update.dialog.idle_msg")
					if st.State == core.UpdateUpToDate {
						msg = tr(c, "update.dialog.up_to_date_msg")
					}
					ui.Text(c, msg).TextColor(t.Color.FgSecondary)
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if primaryButton(c, tr(c, "update.action.check")).Clicked() {
							s.triggerUpdateCheck()
						}
						if updateExternalButton(c, tr(c, "update.prompt.view_changelog")).Clicked() {
							s.openUpdateURL(c, core.DefaultReleasesURL)
						}
						if button(c, tr(c, "common.close")).Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateChecking:
					progressBar(c, -1, tr(c, "update.progress.checking"))
					ui.Text(c, tr(c, "update.dialog.checking_msg")).TextColor(t.Color.FgSecondary)
					if button(c, tr(c, "common.close")).Clicked() {
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
							if primaryButton(c, tr(c, "update.action.update_now")).Clicked() {
								s.triggerUpdateStart()
							}
						}
						if button(c, tr(c, "common.close")).Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateDownloading:
					progressBar(c, progressValue(st.DownloadedBytes, st.TotalBytes), tr(c, "update.progress.downloading"))
					ui.Text(c, formatUpdateProgress(st)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if button(c, tr(c, "update.action.cancel_download")).Clicked() {
							s.triggerUpdateCancel()
						}
						if button(c, tr(c, "common.close")).Clicked() {
							s.updateDialogOpen = false
						}
					})
				case core.UpdateVerifying:
					progressBar(c, -1, tr(c, "update.progress.verifying"))
					ui.Text(c, tr(c, "update.dialog.verifying_msg")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					if button(c, tr(c, "common.cancel")).Clicked() {
						s.triggerUpdateCancel()
					}
				case core.UpdateReadyToApply:
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if workloads.Any() {
							if dangerButton(c, tr(c, "update.action.stop_and_install")).Clicked() {
								s.triggerUpdateApply(true)
							}
						} else {
							if primaryButton(c, tr(c, "update.action.update_and_restart")).Clicked() {
								s.triggerUpdateApply(false)
							}
						}
						if button(c, tr(c, "update.action.later")).Clicked() {
							s.dismissUpdateReadyPrompt(st)
						}
					})
				case core.UpdateApplying:
					progressBar(c, -1, tr(c, "update.progress.applying"))
					ui.Text(c, tr(c, "update.status.applying")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				case core.UpdateError:
					errMsg := st.ErrorMessage
					if errMsg == "" {
						errMsg = tr(c, "update.dialog.error_fallback")
					}
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateDanger,
						Size:    bannerCompact,
						Icon:    reicon.CircleAlert,
						Message: errMsg,
					})
					ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
						if primaryButton(c, tr(c, "common.retry")).Clicked() {
							s.triggerUpdateRetry()
						}
						if st.FallbackPath != "" {
							if outlineButton(c, tr(c, "update.action.open_package")).Clicked() {
								s.openUpdatePath(st.FallbackPath)
							}
						}
						if button(c, tr(c, "common.close")).Clicked() {
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
		ui.Box(c.Key("update-ready-prompt-overlay")).Absolute().Left(0).Top(0).Right(0).Bottom(0).PassThrough().
			AlignItems(ui.End).Justify(ui.End).Padding(t.Density.Section).Children(func() {
			card := applyShadow(
				ui.Column(c.Key("update-ready-prompt")).
					Width(340).MaxWidthPercent(90).
					Padding(promptPad).Gap(t.Density.Stack).Radius(t.Radius.Modal).
					Background(t.Color.Elevated).
					Border(1, t.Color.BorderDefault),
				t.Elevation.LG,
			)
			card.Children(func() {
				ui.Text(c, tr(c, "update.badge.available")).Bold().FontSize(t.Type.CardTitle).TextColor(t.Color.FgPrimary)
				ui.Text(c, trf(c, "update.prompt.ready_body", "version", ver)).
					FontSize(t.Type.Body).TextColor(t.Color.FgSecondary)
				if releaseURL := updateReleaseURL(st.Release); releaseURL != "" {
					notesLabel := tr(c, "update.prompt.view_changelog")
					notesBtn := components.Button(c, "", components.ButtonGhost).
						Padding(0).Radius(promptBtnRadius).AlignSelf(ui.Start).Label(notesLabel)
					notesBtn.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Hairline).Children(func() {
							ui.Text(c, notesLabel).FontSize(t.Type.Body).
								TextColor(t.Color.Accent).Underline().UnderlineOffset(2)
							studioIcon(c, reicon.ArrowUpRight, t.Color.Accent)
						})
					})
					if notesBtn.Clicked() {
						s.openUpdateURL(c, releaseURL)
					}
				}
				ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Stack).Padding(t.Density.Row, 0, 0, 0).Children(func() {
					laterLabel := tr(c, "update.action.later")
					laterBtn := styleButtonSize(c, components.Button(c, "", components.ButtonGhost), sizeMD).Radius(promptBtnRadius).Label(laterLabel)
					laterBtn.Children(func() {
						ui.Text(c, laterLabel).SingleLine().TextColor(t.Color.FgSecondary)
					})
					if laterBtn.Clicked() {
						s.dismissUpdateReadyPrompt(st)
					}
					restartBtn := styleButtonSize(c, components.Button(c, tr(c, "update.action.update_and_restart"), components.ButtonPrimary), sizeMD).Radius(promptBtnRadius)
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
		s.enqueueStatusToast(tr(c, "update.toast.release_opened_preview"))
		return
	}
	c.OpenURLThen(rawURL, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("update.toast.release_open_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("update.toast.release_opened"))
	})
}

func (s *viewState) openUpdatePath(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(l10n.TR("update.toast.package_opened_preview"))
		return
	}
	if err := openUpdatePathFunc(path); err != nil {
		s.enqueueStatusToast(l10n.TR("update.toast.package_open_failed"))
		return
	}
	s.enqueueStatusToast(l10n.TR("update.toast.package_opened"))
}
