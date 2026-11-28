package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
	"inferyard/studio/internal/l10n"
	"inferyard/studio/internal/remote"
)

func (s *viewState) nodesDialogs(c *ui.Context) {
	s.addNodeDialog(c)
	s.hostKeyDialog(c)
	s.remoteLogDialog(c)
	s.nodeCleanupDialogs(c)
}

func (s *viewState) addNodeDialog(c *ui.Context) {
	t := tokens(c)
	studioDialog(c.Key("add-node-dialog"), &s.nodes.addOpen, components.DialogOptions{Label: tr(c, "nodes.dialog.title")}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(150)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				heading(c, tr(c, "nodes.dialog.title"), 2)
				studioUnderlineTabs(c, &s.nodes.addMode, tr(c, "nodes.dialog.pair_tab"), tr(c, "nodes.dialog.ssh_tab"))
				if s.nodes.addMode == 1 {
					s.sshNodeForm(c)
				} else {
					s.pairingNodeForm(c)
				}
			})
		})
	})
}

func (s *viewState) pairingNodeForm(c *ui.Context) {
	t := tokens(c)
	if len(s.nodes.pairingRequests) > 0 {
		s.pairingConfirmationCard(c, s.nodes.pairingRequests[0])
		return
	}
	ui.Text(c, tr(c, "nodes.pairing.instructions")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	segmentedControl(c, tr(c, "nodes.pairing.os_label"), &s.nodes.pairOS, []components.NativeSelectOption[int]{
		{Value: 0, Label: "Windows"}, {Value: 1, Label: "macOS / Linux"},
	})
	command := ""
	if session := s.nodes.pairing; session != nil {
		command = session.WindowsCommand
		if s.nodes.pairOS == 1 {
			command = session.UnixCommand
		}
	}
	applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
		Padding(t.Density.Row, t.Density.Stack).Radius(t.Radius.Control), materialSunken).Children(func() {
		cmdDisplay := command
		cmdColor := t.Color.FgPrimary
		if cmdDisplay == "" {
			cmdDisplay = "—"
			cmdColor = t.Color.FgTertiary
		}
		ui.Text(c, cmdDisplay).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(cmdColor).Selectable().Grow(1).MinWidth(0)
		if outlineIconButton(c, reicon.Copy, tr(c, "nodes.pairing.copy")).Disabled(command == "").Clicked() {
			s.copyNodeText(c, command)
		}
	})
	errorView(c, s.nodes.pairingErr)
	if session := s.nodes.pairing; session != nil {
		applyMaterial(c, ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).
			Padding(t.Density.Row, t.Density.Stack).Radius(t.Radius.Control), materialSunken).Children(func() {
			components.Icon(c, reicon.Activity).Size(15, 15).Shrink(0).TextColor(stateColor(c, stateActive))
			ui.Text(c, trf(c, "nodes.pairing.waiting", "code", session.FormattedCode)).FontSize(t.Type.Caption).FontFeatures("tnum").Grow(1).MinWidth(0)
		})
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
		if button(c, tr(c, "common.close")).Clicked() {
			s.nodes.addOpen = false
		}
	})
}

func (s *viewState) pairingConfirmationCard(c *ui.Context, request remote.PairingRequest) {
	t := tokens(c)
	applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Density.Stack).
		Radius(t.Radius.Control).Border(1, stateColor(c, stateSuccess)), materialSunken).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
			components.Icon(c, reicon.Monitor).Size(18, 18).Shrink(0).TextColor(stateColor(c, stateSuccess))
			ui.Text(c, request.Hostname).Bold().FontSize(t.Type.CardTitle).Grow(1).MinWidth(0).SingleLine()
			inlineBadge(c, request.FormattedCode, stateInfo, badgeOptions{Size: sizeSM})
		})
		ui.Text(c, request.OS+" "+request.Arch+" · "+request.HardwareSummary).FontSize(t.Type.Caption)
		ui.Text(c, trf(c, "nodes.pairing.source", "ip", request.SourceIP, "network", request.SourceHint)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary)
	})
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
		if dangerButton(c, tr(c, "nodes.pairing.reject")).Clicked() {
			s.submitPairingDecision(request, remote.PairingReject)
		}
		if primaryButton(c, tr(c, "nodes.pairing.approve")).Clicked() {
			s.submitPairingDecision(request, remote.PairingApprove)
		}
	})
}

func (s *viewState) sshNodeForm(c *ui.Context) {
	t := tokens(c)
	ui.Text(c, tr(c, "nodes.ssh.instructions")).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
	monoField(c, tr(c, "nodes.ssh.command_label"), "", "ssh -p 38291 root@connect.example.com", "", &s.nodes.sshCommand)
	s.parseSSHInput()
	if parsed := s.nodes.sshParsed; parsed != nil {
		ui.Text(c, fmt.Sprintf("%s@%s:%d", parsed.User, parsed.Host, parsed.Port)).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(stateColor(c, stateSuccess))
	}
	errorView(c, s.nodes.sshErr)
	ui.Field(c, tr(c, "nodes.ssh.password_label"), func() {
		studioTextBox(c, &s.nodes.sshPassword).FillWidth().Password().Label(tr(c, "nodes.ssh.password_label")).Placeholder(tr(c, "nodes.ssh.password_placeholder"))
	})
	ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
	ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
		if button(c, tr(c, "common.cancel")).Clicked() {
			s.nodes.addOpen = false
		}
		if primaryActionButton(c, tr(c, "nodes.ssh.connect"), s.actionBusy("connect-ssh-node")).
			Disabled(s.busy || s.nodes.sshParsed == nil).Clicked() {
			s.connectSSHNode()
		}
	})
}

func (s *viewState) hostKeyDialog(c *ui.Context) {
	if len(s.nodes.hostKeyRequests) == 0 {
		return
	}
	request := s.nodes.hostKeyRequests[0]
	open := true
	studioDialog(c.Key("hostkey-dialog"), &open, components.DialogOptions{
		Label:                tr(c, "nodes.hostkey.title"),
		DisableBackdropClose: true,
		DisableEscapeClose:   true,
	}, func(panel ui.Element) {
		t := tokens(c)
		panel.Width(c.Theme().Space(135)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				heading(c, tr(c, "nodes.hostkey.title"), 2)
				applyMaterial(c, ui.Column(c).FillWidth().Gap(0).Padding(t.Density.Row, t.Density.Stack).Radius(t.Radius.Inset), materialSunken).Children(func() {
					keyValueRow(c, tr(c, "nodes.hostkey.host_port"), fmt.Sprintf("%s:%d", request.Host, request.Port), true)
					reportKVDivider(c)
					keyValueRow(c, tr(c, "nodes.hostkey.key_type"), request.KeyType, true)
					reportKVDivider(c)
					keyValueRow(c, tr(c, "nodes.hostkey.fingerprint"), request.FingerprintSHA256, true)
				})
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				ui.Row(c).FillWidth().Wrap().Justify(ui.End).AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					if dangerButton(c, tr(c, "nodes.hostkey.reject")).Clicked() {
						s.submitHostKeyDecision(request, remote.HostKeyReject)
					}
					if button(c, tr(c, "nodes.hostkey.trust_once")).Clicked() {
						s.submitHostKeyDecision(request, remote.HostKeyTrustOnce)
					}
					if primaryButton(c, tr(c, "nodes.hostkey.trust_save")).Clicked() {
						s.submitHostKeyDecision(request, remote.HostKeyTrustAndSave)
					}
				})
			})
		})
	})
}

func (s *viewState) remoteLogDialog(c *ui.Context) {
	t := tokens(c)
	studioDialog(c.Key("remote-log-dialog"), &s.nodes.logOpen, components.DialogOptions{Label: s.nodes.logTitle}, func(panel ui.Element) {
		panel.Width(c.Theme().Space(150)).MaxWidthPercent(92).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Density.CardGap).Children(func() {
				heading(c, s.nodes.logTitle, 2)
				applyMaterial(c, ui.Scroll(c).FillWidth().MaxHeight(360), materialSunken).Children(func() {
					ui.Column(c).FillWidth().Padding(t.Density.Stack).Children(func() {
						ui.Text(c, s.nodes.logText).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
					})
				})
				ui.Box(c).FillWidth().Height(1).Background(t.Color.BorderSubtle)
				ui.Row(c).FillWidth().Justify(ui.End).Gap(t.Density.Row).Children(func() {
					if outlineButton(c, tr(c, "services.usage.copy")).Disabled(s.nodes.logText == "").Clicked() {
						s.copyNodeText(c, s.nodes.logText)
					}
					if button(c, tr(c, "common.close")).Clicked() {
						s.nodes.logOpen = false
					}
				})
			})
		})
	})
}

func (s *viewState) copyNodeText(c *ui.Context, value string) {
	if value == "" {
		return
	}
	if s.preview != nil {
		s.enqueueStatusToast(tr(c, "nodes.copy_preview"))
		return
	}
	c.WriteClipboardThen(value, func(err error) {
		if err != nil {
			s.enqueueStatusToast(l10n.TR("nodes.copy_failed"))
			return
		}
		s.enqueueStatusToast(l10n.TR("nodes.copied"))
	})
}
