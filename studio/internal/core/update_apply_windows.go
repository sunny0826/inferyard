//go:build windows

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

const windowsDetachedFlags = 0x08000200 // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP

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
	psPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: l10n.TR("core.update.err.windows_powershell_missing"),
			cause:   err,
		}
	}

	scriptFile, err := os.CreateTemp("", "inferyard-studio-update-*.ps1")
	if err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: l10n.TR("core.update.err.windows_script_create_failed"),
			cause:   err,
		}
	}
	scriptPath := scriptFile.Name()
	if _, err := scriptFile.WriteString(windowsUpdateScriptContent()); err != nil {
		_ = scriptFile.Close()
		_ = os.Remove(scriptPath)
		return &updateCheckError{code: "update_apply_failed", message: l10n.TR("core.update.err.windows_script_write_failed"), cause: err}
	}
	if err := scriptFile.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return &updateCheckError{code: "update_apply_failed", message: l10n.TR("core.update.err.windows_script_close_failed"), cause: err}
	}

	launchExe := plan.ExePath
	if plan.SkipRelaunch {
		launchExe = ""
	}
	if plan.PreserveDir != nil {
		plan.PreserveDir()
	}

	cmd := exec.Command(
		psPath,
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-TargetPid", strconv.Itoa(os.Getpid()),
		"-InstallerPath", plan.ArtifactPath,
		"-InstallDir", installDir,
		"-StagingDir", plan.StagingDir,
		"-LaunchExe", launchExe,
		"-ErrorRecordPath", plan.ErrorRecordPath,
		"-TargetVersion", plan.TargetVersion,
		"-ReleaseURL", plan.ReleaseURL,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsDetachedFlags,
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return &updateCheckError{
			code:    "update_apply_failed",
			message: l10n.TR("core.update.err.windows_installer_launch_failed"),
			cause:   err,
		}
	}
	if plan.Shutdown != nil {
		_ = plan.Shutdown()
	}
	if plan.RequestQuit != nil {
		plan.RequestQuit()
	}
	return nil
}
