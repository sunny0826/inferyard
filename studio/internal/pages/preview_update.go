//go:build ui_debug && !release

package pages

import (
	"time"

	"inferyard/studio/internal/core"
)

const previewUpdateVersion = "0.10.0"

func previewUpdateRelease() core.UpdateRelease {
	return core.UpdateRelease{
		Version:      previewUpdateVersion,
		TagName:      "v" + previewUpdateVersion,
		Name:         "v" + previewUpdateVersion,
		Notes:        "## Highlights\n\n- feat(studio): add automatic update discovery and one-click in-app update\n- fix(studio): improve download stability and verification",
		ReleaseURL:   "https://github.com/sunny0826/inferyard/releases/tag/v" + previewUpdateVersion,
		AssetName:    "inferyard-studio-v" + previewUpdateVersion + "-darwin-arm64.dmg",
		AssetURL:     "https://github.com/sunny0826/inferyard/releases/download/v" + previewUpdateVersion + "/inferyard-studio-v" + previewUpdateVersion + "-darwin-arm64.dmg",
		AssetSize:    15 << 20,
		ChecksumsURL: "https://github.com/sunny0826/inferyard/releases/download/v" + previewUpdateVersion + "/SHA256SUMS",
	}
}

func previewUpdateAvailable(s *viewState) {
	rel := previewUpdateRelease()
	s.updateDialogOpen = true
	s.updateState = core.UpdateState{
		State:          core.UpdateAvailable,
		CurrentVersion: "0.0.8",
		Release:        &rel,
		TotalBytes:     previewValue(rel.AssetSize),
		CanSelfUpdate:  true,
	}
}

func previewUpdateDownloading(s *viewState) {
	rel := previewUpdateRelease()
	s.updateDialogOpen = true
	s.updateState = core.UpdateState{
		State:           core.UpdateDownloading,
		CurrentVersion:  "0.0.8",
		Release:         &rel,
		DownloadedBytes: 6 << 20,
		TotalBytes:      previewValue(rel.AssetSize),
		BytesPerSecond:  previewValue(3.0 * (1 << 20)),
		CanSelfUpdate:   true,
	}
}

func previewUpdateError(s *viewState) {
	rel := previewUpdateRelease()
	s.updateDialogOpen = true
	s.updateState = core.UpdateState{
		State:          core.UpdateError,
		CurrentVersion: "0.0.8",
		Release:        &rel,
		ErrorCode:      "update_path_unwritable",
		ErrorMessage:   "当前应用所在目录没有写入权限，可打开安装包手动安装或前往发布页下载。",
		FallbackPath:   "/preview/updates/" + rel.AssetName,
		CanSelfUpdate:  false,
	}
}

func (p *previewSession) checkUpdate() {
	s := p.state
	st := s.currentUpdateState()
	if st.State == core.UpdateChecking || st.State == core.UpdateDownloading ||
		st.State == core.UpdateVerifying || st.State == core.UpdateApplying {
		return
	}
	p.cancelEvents("update")
	fail := p.takeFailure()
	st.State = core.UpdateChecking
	st.ManualCheck = true
	st.ErrorCode = ""
	st.ErrorMessage = ""
	s.updateState = st
	p.after("update", 400*time.Millisecond, func() {
		if fail {
			s.updateState.State = core.UpdateError
			s.updateState.ErrorCode = "update_network_error"
			s.updateState.ErrorMessage = "无法连接更新服务，请检查网络或代理设置。"
			return
		}
		rel := previewUpdateRelease()
		s.updateState.State = core.UpdateAvailable
		s.updateState.Release = &rel
		s.updateState.TotalBytes = previewValue(rel.AssetSize)
		s.updateState.DownloadedBytes = 0
		s.updateState.BytesPerSecond = nil
		s.updateState.ErrorCode = ""
		s.updateState.ErrorMessage = ""
		s.updateState.FallbackPath = ""
		s.updateState.CanSelfUpdate = true
		s.updateState.SelfUpdateBlock = ""
	})
}

func (p *previewSession) startUpdateDownload(oneClick bool) {
	s := p.state
	if s.updateState.Release == nil {
		return
	}
	p.cancelEvents("update")
	fail := p.takeFailure()
	rel := *s.updateState.Release
	s.updateState.State = core.UpdateDownloading
	s.updateState.DownloadedBytes = 0
	s.updateState.TotalBytes = previewValue(rel.AssetSize)
	s.updateState.BytesPerSecond = previewValue(3.0 * (1 << 20))
	s.updateState.ErrorCode = ""
	s.updateState.ErrorMessage = ""
	s.updateState.FallbackPath = ""
	p.tickUpdateDownload(fail, 1, oneClick)
}

func (p *previewSession) tickUpdateDownload(fail bool, step int, oneClick bool) {
	p.after("update", 300*time.Millisecond, func() {
		s := p.state
		if s.updateState.State != core.UpdateDownloading || s.updateState.Release == nil {
			return
		}
		total := s.updateState.Release.AssetSize
		if total <= 0 {
			total = 15 << 20
		}
		if step < 5 {
			done := total * int64(step) / 5
			s.updateState.DownloadedBytes = done
			s.updateState.TotalBytes = previewValue(total)
			s.updateState.BytesPerSecond = previewValue(3.0 * (1 << 20))
			p.tickUpdateDownload(fail, step+1, oneClick)
			return
		}
		s.updateState.State = core.UpdateVerifying
		s.updateState.DownloadedBytes = total
		s.updateState.TotalBytes = previewValue(total)
		s.updateState.BytesPerSecond = nil
		p.after("update", 400*time.Millisecond, func() {
			if s.updateState.State != core.UpdateVerifying {
				return
			}
			if fail {
				s.updateState.State = core.UpdateError
				s.updateState.DownloadedBytes = 0
				s.updateState.ErrorCode = "update_checksum_mismatch"
				s.updateState.ErrorMessage = "安装包 SHA-256 校验未通过，已删除损坏文件。"
				s.updateState.FallbackPath = ""
				return
			}
			s.updateState.State = core.UpdateReadyToApply
			s.updateState.Workloads = s.effectiveUpdateWorkloads()
			if oneClick && !s.updateState.Workloads.Any() {
				p.applyUpdate(false)
			}
		})
	})
}

func (p *previewSession) cancelUpdate() {
	p.cancelEvents("update")
	s := p.state
	s.updateState.DownloadedBytes = 0
	s.updateState.BytesPerSecond = nil
	s.updateState.ErrorCode = ""
	s.updateState.ErrorMessage = ""
	s.updateState.FallbackPath = ""
	if s.updateState.Release != nil {
		s.updateState.State = core.UpdateAvailable
		s.updateState.TotalBytes = previewValue(s.updateState.Release.AssetSize)
	} else {
		s.updateState.State = core.UpdateIdle
		s.updateState.TotalBytes = nil
	}
}

func (p *previewSession) applyUpdate(confirmStop bool) {
	s := p.state
	if s.updateState.State != core.UpdateReadyToApply || s.updateState.Release == nil {
		return
	}
	workloads := s.effectiveUpdateWorkloads()
	if workloads.Any() && !confirmStop {
		s.updateState.Workloads = workloads
		return
	}
	fail := p.takeFailure()
	p.cancelEvents("update")
	s.updateState.State = core.UpdateApplying
	s.updateState.ErrorCode = ""
	s.updateState.ErrorMessage = ""
	p.after("update", 500*time.Millisecond, func() {
		if s.updateState.State != core.UpdateApplying || s.updateState.Release == nil {
			return
		}
		if fail {
			s.updateState.State = core.UpdateError
			s.updateState.ErrorCode = "update_path_unwritable"
			s.updateState.ErrorMessage = "当前安装目录没有写入权限，可打开安装包手动安装或前往发布页下载。"
			s.updateState.FallbackPath = "/preview/updates/" + s.updateState.Release.AssetName
			s.updateState.CanSelfUpdate = false
			return
		}
		ver := s.updateState.Release.Version
		s.updateState = core.UpdateState{
			State:          core.UpdateUpToDate,
			CurrentVersion: ver,
			CanSelfUpdate:  true,
		}
		s.updateDialogOpen = false
		s.enqueueStatusToast("模拟更新已完成并重启到 v" + ver)
	})
}

func (p *previewSession) retryUpdate() {
	s := p.state
	if s.updateState.Release != nil && s.updateState.Release.AssetURL != "" &&
		s.updateState.ErrorCode != "update_network_error" && s.updateState.ErrorCode != "update_check_failed" {
		p.startUpdateDownload(true)
		return
	}
	p.checkUpdate()
}
