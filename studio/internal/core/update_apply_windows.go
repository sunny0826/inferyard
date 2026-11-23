//go:build windows

package core

import (
	"context"
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
			BlockMessage:  "当前安装目录没有写入权限，可打开已下载安装包手动安装或前往发布页下载。",
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
			message: "当前安装目录没有写入权限，可打开已下载安装包手动安装或前往发布页下载。",
			cause:   err,
		}
	}
	psPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: "未找到 PowerShell，无法启动静默安装，可打开已下载安装包手动安装。",
			cause:   err,
		}
	}

	scriptFile, err := os.CreateTemp("", "inferyard-studio-update-*.ps1")
	if err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: "创建安装辅助脚本失败。",
			cause:   err,
		}
	}
	scriptPath := scriptFile.Name()
	if _, err := scriptFile.WriteString(windowsUpdateScriptContent()); err != nil {
		_ = scriptFile.Close()
		_ = os.Remove(scriptPath)
		return &updateCheckError{code: "update_apply_failed", message: "写入安装辅助脚本失败。", cause: err}
	}
	if err := scriptFile.Close(); err != nil {
		_ = os.Remove(scriptPath)
		return &updateCheckError{code: "update_apply_failed", message: "关闭安装辅助脚本失败。", cause: err}
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
			message: "启动 Windows 安装程序失败，可打开已下载安装包手动安装。",
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
