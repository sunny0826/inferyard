package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxUpdateTarEntries = 500
	maxUpdateTarBytes   = 512 << 20
)

func (m *updateManager) applyUpdate(ctx context.Context, confirmStopWorkloads bool) (UpdateState, error) {
	if ctx == nil {
		if m.service != nil && m.service.ctx != nil {
			ctx = m.service.ctx
		} else {
			ctx = context.Background()
		}
	}

	m.mu.Lock()
	if m.state.State == UpdateApplying {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_busy", message: "正在安装更新，请稍候。"}
	}
	if m.state.State != UpdateReadyToApply || m.stagedArtifact == "" {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_not_ready", message: "更新安装包尚未准备就绪。"}
	}
	artifact := m.stagedArtifact
	stagingDir := m.stagedDir
	quitFn := m.requestQuit
	runCmd := m.opts.CommandRunner
	applyFn := m.opts.ApplyFunc
	skipRelaunch := m.opts.SkipRelaunch
	m.mu.Unlock()

	if st, err := os.Stat(artifact); err != nil || !st.Mode().IsRegular() {
		m.mu.Lock()
		m.removeStagedLocked()
		m.state.State = UpdateError
		m.state.ErrorCode = "update_artifact_missing"
		m.state.ErrorMessage = "已下载的更新安装包不存在，请重新下载。"
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: m.state.ErrorCode, message: m.state.ErrorMessage}
	}

	var workloads UpdateActiveWorkloads
	if m.service != nil {
		workloads = m.service.ActiveUpdateWorkloads()
	}
	if workloads.Any() && !confirmStopWorkloads {
		m.mu.Lock()
		if m.state.State == UpdateReadyToApply {
			m.state.Workloads = workloads
			m.publishLocked()
		}
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, ErrUpdateWorkloadActive
	}

	target := m.inspectTarget()
	if !target.CanSelfUpdate || target.BlockCode != "" {
		msg := target.BlockMessage
		if msg == "" {
			msg = "当前安装路径不可写，无法完成自动替换。"
		}
		code := target.BlockCode
		if code == "" {
			code = "update_path_unwritable"
		}
		m.mu.Lock()
		m.state.State = UpdateError
		m.state.CanSelfUpdate = false
		m.state.SelfUpdateBlock = msg
		m.state.ErrorCode = code
		m.state.ErrorMessage = msg
		m.state.Workloads = workloads
		m.state.FallbackPath = artifact
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: code, message: msg}
	}

	m.mu.Lock()
	if m.state.State != UpdateReadyToApply || m.stagedArtifact != artifact {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_busy", message: "更新状态已变更，请重试。"}
	}
	m.opSeq++
	m.state.State = UpdateApplying
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""
	m.state.Workloads = UpdateActiveWorkloads{}
	m.state.CanSelfUpdate = true
	m.state.SelfUpdateBlock = ""
	m.publishLocked()
	m.mu.Unlock()

	if runCmd == nil {
		runCmd = func(cmdCtx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(cmdCtx, name, args...).CombinedOutput()
		}
	}

	plan := platformApplyPlan{
		ArtifactPath: artifact,
		StagingDir:   stagingDir,
		ExePath:      target.ExePath,
		AppPath:      target.AppPath,
		InstallDir:   target.InstallDir,
		SkipRelaunch: skipRelaunch,
		Shutdown: func() error {
			if m.service == nil {
				return nil
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			return m.service.Shutdown(shutdownCtx)
		},
		RequestQuit: func() {
			if quitFn != nil {
				quitFn()
			}
		},
		PreserveDir: func() {
			m.mu.Lock()
			m.preserveStagingOnExit = true
			m.mu.Unlock()
		},
		RunCmd: runCmd,
	}

	var applyErr error
	if applyFn != nil {
		applyErr = applyFn(ctx, plan)
	} else {
		applyErr = applyPlatformUpdate(ctx, plan)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if applyErr != nil {
		m.preserveStagingOnExit = false
		code, msg := "update_apply_failed", "安装更新失败，现有安装未受影响。"
		var ce *updateCheckError
		if errors.As(applyErr, &ce) {
			code, msg = ce.code, ce.message
		}
		if st, statErr := os.Stat(artifact); statErr == nil && st.Mode().IsRegular() && retainFallbackOnApplyError(code) {
			m.state.FallbackPath = artifact
		} else {
			m.removeStagedLocked()
		}
		m.state.State = UpdateError
		m.state.ErrorCode = code
		m.state.ErrorMessage = msg
		m.publishLocked()
		return m.cloneStateLocked(), applyErr
	}
	return m.cloneStateLocked(), nil
}

func retainFallbackOnApplyError(code string) bool {
	switch code {
	case "update_dmg_invalid", "update_dmg_attach_failed", "update_extract_failed", "update_artifact_missing":
		return false
	default:
		return true
	}
}

func checkDirWritable(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("empty directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	probe, err := os.CreateTemp(dir, ".inferyard-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func resolveDarwinAppBundle(exePath string) (appBundlePath, blockCode, blockMsg string) {
	if strings.Contains(exePath, "/AppTranslocation/") {
		return "", "update_app_translocated", "应用正处于 macOS 隔离路径，请移至「应用程序」目录后再更新。"
	}
	resolved := exePath
	if eval, err := filepath.EvalSymlinks(exePath); err == nil {
		resolved = eval
	}
	if strings.Contains(resolved, "/AppTranslocation/") {
		return "", "update_app_translocated", "应用正处于 macOS 隔离路径，请移至「应用程序」目录后再更新。"
	}
	macosDir := filepath.Dir(resolved)
	contentsDir := filepath.Dir(macosDir)
	appDir := filepath.Dir(contentsDir)
	if filepath.Base(macosDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || !strings.HasSuffix(filepath.Base(appDir), ".app") {
		return "", "update_not_app_bundle", "当前未从 .app 应用包运行，请前往发布页手动更新。"
	}
	if err := checkDirWritable(filepath.Dir(appDir)); err != nil {
		return "", "update_path_unwritable", "当前应用所在目录没有写入权限，可打开安装包手动安装或前往发布页下载。"
	}
	if err := checkDirWritable(appDir); err != nil {
		return "", "update_path_unwritable", "当前应用包没有写入权限，可打开安装包手动安装或前往发布页下载。"
	}
	return appDir, "", ""
}

func extractUpdateTarGz(archivePath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(destDir)
	var entries int
	var totalBytes int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		entries++
		if entries > maxUpdateTarEntries {
			return "", errors.New("archive contains too many entries")
		}

		cleanName := filepath.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if cleanName == "." || cleanName == "" {
			continue
		}
		if filepath.IsAbs(cleanName) || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || strings.ContainsRune(cleanName, 0) {
			return "", fmt.Errorf("unsafe archive entry path: %s", hdr.Name)
		}
		target := filepath.Join(cleanDest, cleanName)
		rel, err := filepath.Rel(cleanDest, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("archive path escapes destination: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if hdr.Size < 0 || totalBytes+hdr.Size > maxUpdateTarBytes {
				return "", errors.New("archive exceeds maximum allowed size")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			mode := os.FileMode(hdr.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return "", err
			}
			written, copyErr := io.Copy(out, io.LimitReader(tr, hdr.Size+1))
			closeErr := out.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			if written != hdr.Size {
				return "", fmt.Errorf("truncated archive entry: %s", hdr.Name)
			}
			totalBytes += written
		default:
			return "", fmt.Errorf("unsupported archive entry type for %s", hdr.Name)
		}
	}

	if info, err := os.Stat(filepath.Join(cleanDest, "inferyard-studio")); err == nil && info.Mode().IsRegular() {
		return cleanDest, nil
	}
	dirEntries, err := os.ReadDir(cleanDest)
	if err != nil {
		return "", err
	}
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(cleanDest, entry.Name())
		if info, err := os.Stat(filepath.Join(candidate, "inferyard-studio")); err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", errors.New("extracted archive does not contain inferyard-studio executable")
}

func applyLinuxExtracted(extractedRoot, exePath string) error {
	targetDir := filepath.Dir(exePath)
	if err := checkDirWritable(targetDir); err != nil {
		return &updateCheckError{
			code:    "update_path_unwritable",
			message: "当前安装目录没有写入权限，可打开安装包手动更新或前往发布页下载。",
			cause:   err,
		}
	}
	srcExe := filepath.Join(extractedRoot, "inferyard-studio")
	if err := atomicCopyFile(srcExe, exePath, 0o755); err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: "替换可执行文件失败，原有安装保持不变。",
			cause:   err,
		}
	}
	for _, companion := range []string{"LICENSE", "README.txt", "icon.png"} {
		src := filepath.Join(extractedRoot, companion)
		if info, err := os.Stat(src); err == nil && info.Mode().IsRegular() {
			_ = atomicCopyFile(src, filepath.Join(targetDir, companion), 0o644)
		}
	}
	return nil
}

func atomicCopyFile(srcPath, dstPath string, mode os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dstDir := filepath.Dir(dstPath)
	tmp, err := os.CreateTemp(dstDir, "."+filepath.Base(dstPath)+".update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func windowsInstallerArgs(installDir string) []string {
	return []string{
		"/SP-",
		"/VERYSILENT",
		"/SUPPRESSMSGBOXES",
		"/NORESTART",
		"/DIR=" + installDir,
	}
}

func windowsUpdateScriptContent() string {
	return `param(
  [int]$TargetPid,
  [string]$InstallerPath,
  [string]$InstallDir,
  [string]$StagingDir,
  [string]$LaunchExe
)
$deadline = [DateTime]::UtcNow.AddSeconds(60)
while ([DateTime]::UtcNow -lt $deadline) {
  $proc = Get-Process -Id $TargetPid -ErrorAction SilentlyContinue
  if (-not $proc) { break }
  Start-Sleep -Milliseconds 150
}
try {
  $dirArg = '/DIR="' + $InstallDir + '"'
  $argList = @('/SP-', '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', $dirArg)
  $res = Start-Process -FilePath $InstallerPath -ArgumentList $argList -Wait -PassThru
  if ($res.ExitCode -eq 0 -and (Test-Path -LiteralPath $LaunchExe -PathType Leaf)) {
    Start-Process -FilePath $LaunchExe | Out-Null
  }
} finally {
  if ($StagingDir -and (Test-Path -LiteralPath $StagingDir)) {
    Remove-Item -LiteralPath $StagingDir -Recurse -Force -ErrorAction SilentlyContinue
  }
  if ($PSCommandPath -and (Test-Path -LiteralPath $PSCommandPath)) {
    Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
  }
}
`
}
