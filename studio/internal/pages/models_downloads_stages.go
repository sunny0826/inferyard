package pages

import (
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func downloadStageLabel(stage string) string {
	switch stage {
	case "downloading":
		return l10n.TR("downloads.stage.downloading")
	case "verifying":
		return l10n.TR("downloads.stage.verifying")
	case "extracting":
		return l10n.TR("downloads.stage.extracting")
	case "preparing":
		return l10n.TR("downloads.stage.preparing")
	case "preflight":
		return l10n.TR("downloads.stage.preflight")
	case "downloading_runtime":
		return l10n.TR("downloads.stage.downloading_runtime")
	case "setup_engine":
		return l10n.TR("downloads.stage.setup_engine")
	case "upgrading":
		return l10n.TR("downloads.stage.upgrading")
	case "downloading_model":
		return l10n.TR("downloads.stage.downloading_model")
	case "verifying_installation":
		return l10n.TR("downloads.stage.verifying_installation")
	case "preparing_uv":
		return l10n.TR("downloads.stage.preparing_uv")
	case "creating_venv":
		return l10n.TR("downloads.stage.creating_venv")
	case "installing_packages":
		return l10n.TR("downloads.stage.installing_packages")
	case "registering_engine":
		return l10n.TR("downloads.stage.registering_engine")
	}
	return ""
}

func engineTaskStatus(task core.DownloadTask) string {
	if task.Kind == "engine_update" {
		switch task.State {
		case "downloading":
			return l10n.TR("downloads.engine_update.downloading")
		case "done":
			return l10n.TR("downloads.engine_update.done")
		case "failed":
			return l10n.TR("downloads.engine_update.failed")
		case "cancelled":
			return l10n.TR("downloads.engine_update.cancelled")
		}
		return l10n.TR("downloads.engine_update.unknown")
	}
	switch task.State {
	case "downloading":
		return l10n.TR("downloads.engine_archive.downloading")
	case "done":
		return l10n.TR("downloads.engine_archive.done")
	case "failed":
		return l10n.TR("downloads.engine_archive.failed")
	case "cancelled":
		return l10n.TR("downloads.engine_archive.cancelled")
	}
	return l10n.TR("downloads.engine_archive.unknown")
}
