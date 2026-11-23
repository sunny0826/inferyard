//go:build linux

package core

import (
	"context"
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

	extractDir := filepath.Join(plan.StagingDir, "extract")
	extractedRoot, err := extractUpdateTarGz(plan.ArtifactPath, extractDir)
	if err != nil {
		return &updateCheckError{
			code:    "update_extract_failed",
			message: "解压更新安装包失败，原有安装保持不变。",
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
				message: "更新已完成，但自动重启失败，请手动重新启动应用。",
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
