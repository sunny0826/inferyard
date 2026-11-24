//go:build darwin

package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ebitengine/purego"
	"inferyard/studio/internal/l10n"
)

const darwinRenameSwap uint32 = 0x00000002

type darwinSwapFunc func(appDir, stageApp string) error

type darwinRenamexFuncs struct {
	renamex  func(from, to string, flags uint32) int32
	errnoPtr func() *int32
}

var loadDarwinRenamex = sync.OnceValues(func() (darwinRenamexFuncs, error) {
	handle, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return darwinRenamexFuncs{}, err
	}
	renamexSym, err := purego.Dlsym(handle, "renamex_np")
	if err != nil {
		return darwinRenamexFuncs{}, err
	}
	errnoSym, err := purego.Dlsym(handle, "__error")
	if err != nil {
		return darwinRenamexFuncs{}, err
	}
	var fns darwinRenamexFuncs
	purego.RegisterFunc(&fns.renamex, renamexSym)
	purego.RegisterFunc(&fns.errnoPtr, errnoSym)
	return fns, nil
})

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
	return applyDarwinUpdate(ctx, plan, renamexSwapDarwin)
}

func applyDarwinUpdate(ctx context.Context, plan platformApplyPlan, swapFn darwinSwapFunc) error {
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
			message: l10n.TR("core.update.err.darwin_dir_unwritable_replace"),
			cause:   err,
		}
	}

	mountDir := filepath.Join(plan.StagingDir, "mount")
	if err := os.MkdirAll(mountDir, 0o700); err != nil {
		return &updateCheckError{code: "update_apply_failed", message: l10n.TR("core.update.err.darwin_mount_dir_failed"), cause: err}
	}
	defer func() { _ = os.RemoveAll(mountDir) }()

	if out, err := plan.RunCmd(ctx, "hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", mountDir, plan.ArtifactPath); err != nil {
		return &updateCheckError{
			code:    "update_dmg_attach_failed",
			message: l10n.TRf("core.update.err.darwin_dmg_attach_failed", "detail", stringsTrimmedTail(string(out), err)),
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
			return &updateCheckError{code: "update_dmg_invalid", message: l10n.TR("core.update.err.darwin_dmg_invalid")}
		}
	}

	suffix := uuid()[:8]
	stageApp := filepath.Join(parentDir, "."+filepath.Base(appDir)+".new-"+suffix)
	cleanupStage := true
	defer func() {
		if cleanupStage {
			_ = os.RemoveAll(stageApp)
		}
	}()

	if out, err := plan.RunCmd(ctx, "ditto", mountedApp, stageApp); err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: l10n.TRf("core.update.err.darwin_copy_bundle_failed", "detail", stringsTrimmedTail(string(out), err)),
			cause:   err,
		}
	}
	_, _ = plan.RunCmd(ctx, "xattr", "-dr", "com.apple.quarantine", stageApp)
	detachDMG()

	if err := swapFn(appDir, stageApp); err != nil {
		msg := l10n.TR("core.update.err.darwin_swap_failed")
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS) {
			msg = l10n.TR("core.update.err.darwin_atomic_swap_unsupported")
		}
		return &updateCheckError{
			code:    "update_apply_failed",
			message: msg,
			cause:   err,
		}
	}
	cleanupStage = false
	_ = os.RemoveAll(stageApp)
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
				message: l10n.TR("core.update.err.relaunch_failed_reopen"),
				cause:   err,
			}
		}
	}
	if plan.RequestQuit != nil {
		plan.RequestQuit()
	}
	return nil
}

func renamexSwapDarwin(pathA, pathB string) error {
	if strings.IndexByte(pathA, 0) >= 0 || strings.IndexByte(pathB, 0) >= 0 {
		return syscall.EINVAL
	}
	fns, err := loadDarwinRenamex()
	if err != nil {
		return syscall.ENOSYS
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	*fns.errnoPtr() = 0
	if fns.renamex(pathA, pathB, darwinRenameSwap) == 0 {
		return nil
	}
	if eno := syscall.Errno(*fns.errnoPtr()); eno != 0 {
		return eno
	}
	return syscall.EIO
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
