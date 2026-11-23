//go:build darwin

package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func inspectPlatformInstallTarget(exePath string) updateInstallTarget {
	appDir, blockCode, blockMsg := resolveDarwinAppBundle(exePath)
	return updateInstallTarget{
		ExePath:       exePath,
		AppPath:       appDir,
		InstallDir:    filepath.Dir(appDir),
		CanSelfUpdate: blockCode == "",
		BlockCode:     blockCode,
		BlockMessage:  blockMsg,
	}
}

func applyPlatformUpdate(ctx context.Context, plan platformApplyPlan) error {
	return applyDarwinUpdate(ctx, plan, os.Rename)
}

func applyDarwinUpdate(ctx context.Context, plan platformApplyPlan, renameFn func(string, string) error) error {
	appDir := plan.AppPath
	if appDir == "" {
		var code, msg string
		appDir, code, msg = resolveDarwinAppBundle(plan.ExePath)
		if code != "" {
			return &updateCheckError{code: code, message: msg}
		}
	}
	parentDir := filepath.Dir(appDir)
	if err := checkDirWritable(parentDir); err != nil {
		return &updateCheckError{
			code:    "update_path_unwritable",
			message: "当前应用所在目录没有写入权限，可打开安装包手动替换或前往发布页下载。",
			cause:   err,
		}
	}

	mountDir := filepath.Join(plan.StagingDir, "mount")
	if err := os.MkdirAll(mountDir, 0o700); err != nil {
		return &updateCheckError{code: "update_apply_failed", message: "创建镜像挂载目录失败。", cause: err}
	}
	defer func() { _ = os.RemoveAll(mountDir) }()

	if out, err := plan.RunCmd(ctx, "hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", mountDir, plan.ArtifactPath); err != nil {
		return &updateCheckError{
			code:    "update_dmg_attach_failed",
			message: "挂载更新镜像失败：" + stringsTrimmedTail(string(out), err),
			cause:   err,
		}
	}
	mounted := true
	detachDMG := func() {
		if !mounted {
			return
		}
		mounted = false
		detachCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := plan.RunCmd(detachCtx, "hdiutil", "detach", mountDir); err != nil {
			_, _ = plan.RunCmd(detachCtx, "hdiutil", "detach", "-force", mountDir)
		}
	}
	defer detachDMG()

	mountedApp := filepath.Join(mountDir, "InferYard Studio.app")
	exeName := filepath.Base(plan.ExePath)
	if exeName == "" || exeName == "." {
		exeName = "inferyard-studio"
	}
	mountedExe := filepath.Join(mountedApp, "Contents", "MacOS", exeName)
	if st, err := os.Stat(mountedExe); err != nil || !st.Mode().IsRegular() {
		fallbackExe := filepath.Join(mountedApp, "Contents", "MacOS", "inferyard-studio")
		if st2, err2 := os.Stat(fallbackExe); err2 != nil || !st2.Mode().IsRegular() {
			return &updateCheckError{code: "update_dmg_invalid", message: "更新镜像中缺少有效的 InferYard Studio.app。"}
		}
	}

	suffix := uuid()[:8]
	stageApp := filepath.Join(parentDir, "."+filepath.Base(appDir)+".new-"+suffix)
	backupApp := filepath.Join(parentDir, "."+filepath.Base(appDir)+".bak-"+suffix)
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageApp)
		}
	}()

	if out, err := plan.RunCmd(ctx, "ditto", mountedApp, stageApp); err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: "复制新版本应用包失败：" + stringsTrimmedTail(string(out), err),
			cause:   err,
		}
	}
	_, _ = plan.RunCmd(ctx, "xattr", "-dr", "com.apple.quarantine", stageApp)
	detachDMG()

	if err := swapDarwinBundles(appDir, stageApp, backupApp, renameFn); err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: "替换应用包失败，原有安装已保留。",
			cause:   err,
		}
	}
	cleanupStage = false
	_ = os.RemoveAll(backupApp)
	if plan.StagingDir != "" {
		_ = os.RemoveAll(plan.StagingDir)
	}

	if plan.Shutdown != nil {
		_ = plan.Shutdown()
	}

	if !plan.SkipRelaunch {
		if err := spawnDarwinRelaunch(os.Getpid(), appDir); err != nil {
			return &updateCheckError{
				code:    "update_relaunch_failed",
				message: "更新已完成，但自动重启失败，请手动重新打开应用。",
				cause:   err,
			}
		}
	}
	if plan.RequestQuit != nil {
		plan.RequestQuit()
	}
	return nil
}

func swapDarwinBundles(appDir, stageApp, backupApp string, renameFn func(string, string) error) error {
	if err := renameFn(appDir, backupApp); err != nil {
		return err
	}
	if err := renameFn(stageApp, appDir); err != nil {
		_ = renameFn(backupApp, appDir)
		return err
	}
	return nil
}

func spawnDarwinRelaunch(pid int, appDir string) error {
	script := `pid="$1"; app="$2"; i=0; while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 150 ]; do sleep 0.1; i=$((i+1)); done; exec open -n "$app"`
	cmd := exec.Command("/bin/sh", "-c", script, "relaunch", strconv.Itoa(pid), appDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func stringsTrimmedTail(out string, err error) string {
	for _, s := range []string{out, err.Error()} {
		if len(s) > 0 {
			if len(s) > 160 {
				return s[len(s)-160:]
			}
			return s
		}
	}
	return "unknown error"
}
