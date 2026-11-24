package core

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"inferyard/studio/internal/l10n"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const updateErrorRecordFile = "last-apply-error.json"

type pendingApplyErrorRecord struct {
	Version    string `json:"version"`
	ReleaseURL string `json:"release_url"`
	ExitCode   int    `json:"exit_code"`
}

func (m *updateManager) applyUpdate(ctx context.Context, confirmStopWorkloads bool) (UpdateState, error) {
	if ctx == nil {
		if m.service != nil && m.service.ctx != nil {
			ctx = m.service.ctx
		} else {
			ctx = context.Background()
		}
	}

	m.mu.Lock()
	if m.state.State == UpdateApplying || m.applyInFlight {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_busy", message: l10n.TR("core.update.err.applying_busy")}
	}
	if m.state.State != UpdateReadyToApply || m.stagedArtifact == "" {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_not_ready", message: l10n.TR("core.update.err.not_ready")}
	}
	m.applyInFlight = true
	artifact := m.stagedArtifact
	stagingDir := m.stagedDir
	expectedSHA := m.stagedSHA256
	var targetVer, releaseURL string
	if m.state.Release != nil {
		targetVer = m.state.Release.Version
		releaseURL = m.state.Release.ReleaseURL
	}
	quitFn := m.requestQuit
	runCmd := m.opts.CommandRunner
	applyFn := m.opts.ApplyFunc
	skipRelaunch := m.opts.SkipRelaunch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.applyInFlight = false
		m.mu.Unlock()
	}()

	if st, err := os.Stat(artifact); err != nil || !st.Mode().IsRegular() {
		m.mu.Lock()
		m.removeStagedLocked()
		m.state.State = UpdateError
		m.state.ErrorCode = "update_artifact_missing"
		m.state.ErrorMessage = l10n.TR("core.update.err.artifact_missing")
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: m.state.ErrorCode, message: m.state.ErrorMessage}
	}

	actualSHA, hashErr := sha256File(artifact)
	if expectedSHA == "" || hashErr != nil || subtle.ConstantTimeCompare([]byte(actualSHA), []byte(expectedSHA)) != 1 {
		m.mu.Lock()
		m.removeStagedLocked()
		m.state.State = UpdateError
		m.state.ErrorCode = "update_checksum_mismatch"
		m.state.ErrorMessage = l10n.TR("core.update.err.apply_checksum_mismatch")
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: m.state.ErrorCode, message: m.state.ErrorMessage}
	}

	workloads, apiErr, admitted := m.service.beginUpdateApplyAdmission(confirmStopWorkloads)
	if apiErr != nil {
		m.mu.Lock()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, apiErr
	}
	if !admitted {
		m.mu.Lock()
		if m.state.State == UpdateReadyToApply {
			m.state.Workloads = workloads
			m.publishLocked()
		}
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, ErrUpdateWorkloadActive
	}

	keepAdmissionFrozen := false
	defer func() {
		if !keepAdmissionFrozen && m.service != nil {
			m.service.endUpdateApplyAdmission()
		}
	}()

	target := m.inspectTarget()
	if !target.CanSelfUpdate || target.BlockCode != "" {
		msg := target.BlockMessage
		if msg == "" {
			msg = l10n.TR("core.update.err.path_unwritable_generic")
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
		return snap, &updateCheckError{code: "update_busy", message: l10n.TR("core.update.err.state_changed")}
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

	errRecordPath := m.pendingErrorRecordPath()
	if stagingDir != "" {
		errRecordPath = filepath.Join(filepath.Dir(stagingDir), updateErrorRecordFile)
	}

	plan := platformApplyPlan{
		ArtifactPath:    artifact,
		StagingDir:      stagingDir,
		ExePath:         target.ExePath,
		AppPath:         target.AppPath,
		InstallDir:      target.InstallDir,
		TargetVersion:   targetVer,
		ReleaseURL:      releaseURL,
		ErrorRecordPath: errRecordPath,
		SkipRelaunch:    skipRelaunch,
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
		code, msg := "update_apply_failed", l10n.TR("core.update.err.apply_failed")
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
	keepAdmissionFrozen = true
	return m.cloneStateLocked(), nil
}

func (m *updateManager) pendingErrorRecordPath() string {
	if m.opts.StagingRoot != "" {
		return filepath.Join(m.opts.StagingRoot, updateErrorRecordFile)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDir) == "" {
		cacheDir = os.TempDir()
	}
	return filepath.Join(cacheDir, "InferYard Studio", "updates", updateErrorRecordFile)
}

func (m *updateManager) consumePendingApplyError() bool {
	path := m.pendingErrorRecordPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	raw = bytes.TrimPrefix(raw, []byte("\xEF\xBB\xBF"))
	var rec pendingApplyErrorRecord
	if json.Unmarshal(raw, &rec) != nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.State != UpdateIdle {
		return false
	}
	ver := strings.TrimPrefix(strings.TrimSpace(rec.Version), "v")
	if ver != "" || validateUpdateURL(rec.ReleaseURL, m.opts.AllowInsecureHTTP) {
		rel := UpdateRelease{
			Version: ver,
			TagName: "v" + ver,
			Name:    "v" + ver,
		}
		if validateUpdateURL(rec.ReleaseURL, m.opts.AllowInsecureHTTP) {
			rel.ReleaseURL = rec.ReleaseURL
		}
		if ver == "" {
			rel.TagName = ""
			rel.Name = ""
		}
		m.state.Release = &rel
	}
	m.state.State = UpdateError
	m.state.ErrorCode = "update_windows_install_failed"
	m.state.ErrorMessage = l10n.TR("core.update.err.windows_install_incomplete")
	m.publishLocked()
	return true
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
		return "", "update_app_translocated", l10n.TR("core.update.err.darwin_translocated")
	}
	resolved := exePath
	if eval, err := filepath.EvalSymlinks(exePath); err == nil {
		resolved = eval
	}
	if strings.Contains(resolved, "/AppTranslocation/") {
		return "", "update_app_translocated", l10n.TR("core.update.err.darwin_translocated")
	}
	macosDir := filepath.Dir(resolved)
	contentsDir := filepath.Dir(macosDir)
	appDir := filepath.Dir(contentsDir)
	if filepath.Base(macosDir) != "MacOS" || filepath.Base(contentsDir) != "Contents" || !strings.HasSuffix(filepath.Base(appDir), ".app") {
		return "", "update_not_app_bundle", l10n.TR("core.update.err.darwin_not_app_bundle")
	}
	if err := checkDirWritable(filepath.Dir(appDir)); err != nil {
		return "", "update_path_unwritable", l10n.TR("core.update.err.darwin_dir_unwritable")
	}
	if err := checkDirWritable(appDir); err != nil {
		return "", "update_path_unwritable", l10n.TR("core.update.err.darwin_bundle_unwritable")
	}
	return appDir, "", ""
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
  [string]$LaunchExe,
  [string]$ErrorRecordPath,
  [string]$TargetVersion,
  [string]$ReleaseURL
)
$deadline = [DateTime]::UtcNow.AddSeconds(60)
while ([DateTime]::UtcNow -lt $deadline) {
  $proc = Get-Process -Id $TargetPid -ErrorAction SilentlyContinue
  if (-not $proc) { break }
  Start-Sleep -Milliseconds 150
}
$exitCode = -1
try {
  $dirArg = '/DIR="' + $InstallDir + '"'
  $argList = @('/SP-', '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', $dirArg)
  $res = Start-Process -FilePath $InstallerPath -ArgumentList $argList -Wait -PassThru
  if ($null -ne $res) { $exitCode = [int]$res.ExitCode }
} catch {
  $exitCode = -1
} finally {
  if ($exitCode -ne 0 -and $ErrorRecordPath) {
    try {
      $payload = @{ version = $TargetVersion; release_url = $ReleaseURL; exit_code = $exitCode } | ConvertTo-Json -Compress
      [System.IO.File]::WriteAllText($ErrorRecordPath, $payload, [System.Text.UTF8Encoding]::new($false))
    } catch {}
  }
  if ($StagingDir -and (Test-Path -LiteralPath $StagingDir)) {
    Remove-Item -LiteralPath $StagingDir -Recurse -Force -ErrorAction SilentlyContinue
  }
  if ($LaunchExe -and (Test-Path -LiteralPath $LaunchExe -PathType Leaf)) {
    Start-Process -FilePath $LaunchExe | Out-Null
  }
  if ($PSCommandPath -and (Test-Path -LiteralPath $PSCommandPath)) {
    Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
  }
}
`
}
