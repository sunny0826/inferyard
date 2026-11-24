//go:build linux

package core

import (
	"context"
	"inferyard/studio/internal/l10n"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func inspectPlatformInstallTarget(exePath string) updateInstallTarget {
	installDir := filepath.Dir(exePath)
	if err := checkDirWritable(installDir); err != nil {
		return updateInstallTarget{
			ExePath:       exePath,
			InstallDir:    installDir,
			CanSelfUpdate: false,
			BlockCode:     "update_path_unwritable",
			BlockMessage:  l10n.TR("core.update.err.install_dir_unwritable"),
		}
	}
	return updateInstallTarget{
		ExePath:       exePath,
		InstallDir:    installDir,
		CanSelfUpdate: true,
	}
}

func applyPlatformUpdate(_ context.Context, plan platformApplyPlan) error {
	installDir := plan.InstallDir
	if installDir == "" {
		installDir = filepath.Dir(plan.ExePath)
	}
	if err := checkDirWritable(installDir); err != nil {
		return &updateCheckError{
			code:    "update_path_unwritable",
			message: l10n.TR("core.update.err.install_dir_unwritable"),
			cause:   err,
		}
	}

	extractDir := filepath.Join(plan.StagingDir, "extract")
	extractedRoot, err := extractUpdateTarGz(plan.ArtifactPath, extractDir)
	if err != nil {
		return &updateCheckError{
			code:    "update_extract_failed",
			message: l10n.TR("core.update.err.linux_extract_failed"),
			cause:   err,
		}
	}
	defer func() { _ = os.RemoveAll(extractDir) }()

	if err := applyLinuxExtracted(extractedRoot, plan.ExePath); err != nil {
		return err
	}
	if plan.StagingDir != "" {
		_ = os.RemoveAll(plan.StagingDir)
	}
	if plan.Shutdown != nil {
		_ = plan.Shutdown()
	}
	if !plan.SkipRelaunch {
		if err := spawnLinuxRelaunch(os.Getpid(), plan.ExePath); err != nil {
			return &updateCheckError{
				code:    "update_relaunch_failed",
				message: l10n.TR("core.update.err.relaunch_failed_restart"),
				cause:   err,
			}
		}
	}
	if plan.RequestQuit != nil {
		plan.RequestQuit()
	}
	return nil
}

func spawnLinuxRelaunch(pid int, exePath string) error {
	script := `pid="$1"; exe="$2"; i=0; while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 150 ]; do sleep 0.1; i=$((i+1)); done; exec "$exe"`
	cmd := exec.Command("/bin/sh", "-c", script, "relaunch", strconv.Itoa(pid), exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
