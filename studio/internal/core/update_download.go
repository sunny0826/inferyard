package core

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	updateProgressInterval = 250 * time.Millisecond
	updateDownloadTimeout  = 15 * time.Minute
	updateVerifyTimeout    = 30 * time.Second
)

func parseSHA256Sums(content, targetName string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		name = strings.TrimPrefix(name, "./")
		if name == targetName {
			return sum, true
		}
	}
	return "", false
}

func (m *updateManager) downloadUpdate(ctx context.Context) (UpdateState, error) {
	if ctx == nil {
		if m.service != nil && m.service.ctx != nil {
			ctx = m.service.ctx
		} else {
			ctx = context.Background()
		}
	}

	m.mu.Lock()
	if m.state.State == UpdateReadyToApply && m.stagedArtifact != "" {
		if st, err := os.Stat(m.stagedArtifact); err == nil && st.Mode().IsRegular() {
			snap := m.cloneStateLocked()
			m.mu.Unlock()
			return snap, nil
		}
	}
	switch m.state.State {
	case UpdateDownloading, UpdateVerifying, UpdateApplying:
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_busy", message: "更新正在处理中，请稍候。"}
	}
	if m.state.Release == nil {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: "update_not_available", message: "当前没有可用的更新版本。"}
	}
	rel := *m.state.Release
	if rel.AssetName == "" || !validateUpdateURL(rel.AssetURL, m.opts.AllowInsecureHTTP) {
		m.state.State = UpdateError
		m.state.ErrorCode = "update_asset_missing"
		m.state.ErrorMessage = "未找到适配当前平台的更新安装包，请前往发布页下载。"
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: m.state.ErrorCode, message: m.state.ErrorMessage}
	}
	if !validateUpdateURL(rel.ChecksumsURL, m.opts.AllowInsecureHTTP) {
		m.state.State = UpdateError
		m.state.ErrorCode = "update_checksums_missing"
		m.state.ErrorMessage = "当前发布缺少 SHA256SUMS 校验文件，无法安全更新。"
		m.publishLocked()
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		return snap, &updateCheckError{code: m.state.ErrorCode, message: m.state.ErrorMessage}
	}

	m.removeStagedLocked()
	m.opSeq++
	seq := m.opSeq
	opCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	currentVer := m.currentVersionLocked()
	m.state.State = UpdateDownloading
	m.state.DownloadedBytes = 0
	if rel.AssetSize > 0 {
		m.state.TotalBytes = ptr(rel.AssetSize)
	} else {
		m.state.TotalBytes = nil
	}
	m.state.BytesPerSecond = nil
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""
	m.publishLocked()
	m.mu.Unlock()
	defer cancel()

	proxy := ""
	if m.service != nil {
		proxy = m.service.downloadProxy()
	}

	baseDir, err := m.stagingBaseDir()
	if err != nil {
		return m.failDownload(seq, opCtx, "", &updateCheckError{code: "update_staging_failed", message: "创建临时下载目录失败。", cause: err})
	}
	stagingDir, err := os.MkdirTemp(baseDir, "update-v"+rel.Version+"-*")
	if err != nil {
		return m.failDownload(seq, opCtx, "", &updateCheckError{code: "update_staging_failed", message: "创建临时下载目录失败。", cause: err})
	}

	m.mu.Lock()
	if seq != m.opSeq {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		_ = os.RemoveAll(stagingDir)
		return snap, context.Canceled
	}
	m.stagedDir = stagingDir
	m.mu.Unlock()

	partPath := filepath.Join(stagingDir, rel.AssetName+".part")
	finalPath := filepath.Join(stagingDir, rel.AssetName)

	downloaded, actualSum, dlErr := m.streamDownloadArtifact(opCtx, seq, rel, currentVer, proxy, partPath)
	if dlErr != nil {
		return m.failDownload(seq, opCtx, stagingDir, dlErr)
	}

	m.mu.Lock()
	if seq != m.opSeq {
		snap := m.cloneStateLocked()
		m.mu.Unlock()
		_ = os.RemoveAll(stagingDir)
		return snap, context.Canceled
	}
	m.state.State = UpdateVerifying
	m.state.DownloadedBytes = downloaded
	if downloaded > 0 {
		m.state.TotalBytes = ptr(downloaded)
	}
	m.state.BytesPerSecond = nil
	m.publishLocked()
	m.mu.Unlock()

	expectedHex, err := m.verifyArtifactChecksum(opCtx, rel, currentVer, proxy, actualSum)
	if err != nil {
		return m.failDownload(seq, opCtx, stagingDir, err)
	}

	if err := os.Rename(partPath, finalPath); err != nil {
		return m.failDownload(seq, opCtx, stagingDir, &updateCheckError{
			code:    "update_staging_failed",
			message: "整理更新安装包失败，请重试。",
			cause:   err,
		})
	}

	var workloads UpdateActiveWorkloads
	if m.service != nil {
		workloads = m.service.ActiveUpdateWorkloads()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if seq != m.opSeq {
		_ = os.RemoveAll(stagingDir)
		return m.cloneStateLocked(), context.Canceled
	}
	m.cancel = nil
	m.stagedArtifact = finalPath
	m.stagedSHA256 = expectedHex
	m.state.State = UpdateReadyToApply
	m.state.DownloadedBytes = downloaded
	m.state.TotalBytes = ptr(downloaded)
	m.state.BytesPerSecond = nil
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""
	m.state.Workloads = workloads
	m.publishLocked()
	return m.cloneStateLocked(), nil
}

func (m *updateManager) streamDownloadArtifact(ctx context.Context, seq uint64, rel UpdateRelease, currentVer, proxy, partPath string) (int64, string, error) {
	client, err := m.httpClient(proxy, updateDownloadTimeout)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return 0, "", &updateCheckError{code: "update_download_failed", message: "创建更新下载请求失败。", cause: err}
	}
	req.Header.Set("User-Agent", "InferYard-Studio/"+strings.TrimPrefix(currentVer, "v"))

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return 0, "", context.Canceled
		}
		return 0, "", &updateCheckError{code: "update_download_failed", message: "下载更新安装包失败，请检查网络后重试。", cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", &updateCheckError{
			code:    "update_download_failed",
			message: fmt.Sprintf("下载更新失败（HTTP %d），请重试。", resp.StatusCode),
		}
	}

	var totalBytes *int64
	if resp.ContentLength > 0 {
		totalBytes = ptr(resp.ContentLength)
	} else if rel.AssetSize > 0 {
		totalBytes = ptr(rel.AssetSize)
	}
	if totalBytes != nil {
		m.mu.Lock()
		if seq == m.opSeq {
			m.state.TotalBytes = totalBytes
			m.publishLocked()
		}
		m.mu.Unlock()
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", &updateCheckError{code: "update_staging_failed", message: "无法写入临时更新文件。", cause: err}
	}
	defer f.Close()

	hasher := sha256.New()
	buf := make([]byte, 32*1024)
	var downloaded int64
	started := time.Now()
	lastEmit := started

	for {
		if ctx.Err() != nil {
			return downloaded, "", context.Canceled
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return downloaded, "", &updateCheckError{code: "update_staging_failed", message: "写入更新文件失败，请检查磁盘空间。", cause: writeErr}
			}
			_, _ = hasher.Write(buf[:n])
			downloaded += int64(n)

			nowTime := time.Now()
			if nowTime.Sub(lastEmit) >= updateProgressInterval {
				elapsed := nowTime.Sub(started).Seconds()
				var rate *float64
				if elapsed > 0.05 {
					rate = ptr(float64(downloaded) / elapsed)
				}
				m.mu.Lock()
				if seq == m.opSeq {
					m.state.DownloadedBytes = downloaded
					if totalBytes != nil {
						m.state.TotalBytes = ptr(*totalBytes)
					}
					m.state.BytesPerSecond = rate
					m.publishLocked()
				}
				m.mu.Unlock()
				lastEmit = nowTime
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if errors.Is(readErr, context.Canceled) || ctx.Err() != nil {
				return downloaded, "", context.Canceled
			}
			return downloaded, "", &updateCheckError{code: "update_download_failed", message: "下载更新中断，请重试。", cause: readErr}
		}
	}

	if err := f.Sync(); err != nil {
		return downloaded, "", &updateCheckError{code: "update_staging_failed", message: "刷新更新文件到磁盘失败。", cause: err}
	}
	return downloaded, hex.EncodeToString(hasher.Sum(nil)), nil
}

func (m *updateManager) verifyArtifactChecksum(ctx context.Context, rel UpdateRelease, currentVer, proxy, actualHex string) (string, error) {
	client, err := m.httpClient(proxy, updateVerifyTimeout)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.ChecksumsURL, nil)
	if err != nil {
		return "", &updateCheckError{code: "update_verify_failed", message: "请求校验文件失败。", cause: err}
	}
	req.Header.Set("User-Agent", "InferYard-Studio/"+strings.TrimPrefix(currentVer, "v"))

	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return "", context.Canceled
		}
		return "", &updateCheckError{code: "update_verify_failed", message: "下载 SHA256SUMS 校验文件失败，请重试。", cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &updateCheckError{
			code:    "update_verify_failed",
			message: fmt.Sprintf("获取校验文件失败（HTTP %d），请重试。", resp.StatusCode),
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return "", context.Canceled
		}
		return "", &updateCheckError{code: "update_verify_failed", message: "读取 SHA256SUMS 校验文件失败。", cause: err}
	}
	expectedHex, ok := parseSHA256Sums(string(raw), rel.AssetName)
	if !ok {
		return "", &updateCheckError{code: "update_checksum_missing", message: "SHA256SUMS 中未找到对应安装包的校验和。"}
	}
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(actualHex)), []byte(expectedHex)) != 1 {
		return "", &updateCheckError{code: "update_checksum_mismatch", message: "安装包 SHA-256 校验未通过，已删除损坏文件。"}
	}
	return expectedHex, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *updateManager) failDownload(seq uint64, ctx context.Context, stagingDir string, err error) (UpdateState, error) {
	if stagingDir != "" {
		_ = os.RemoveAll(stagingDir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if seq != m.opSeq {
		return m.cloneStateLocked(), err
	}
	m.cancel = nil
	m.removeStagedLocked()

	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		if m.state.Release != nil {
			m.state.State = UpdateAvailable
			if m.state.Release.AssetSize > 0 {
				m.state.TotalBytes = ptr(m.state.Release.AssetSize)
			} else {
				m.state.TotalBytes = nil
			}
		} else {
			m.state.State = UpdateIdle
			m.state.TotalBytes = nil
		}
		m.state.DownloadedBytes = 0
		m.state.BytesPerSecond = nil
		m.state.ErrorCode = ""
		m.state.ErrorMessage = ""
		m.publishLocked()
		return m.cloneStateLocked(), err
	}

	var ce *updateCheckError
	code, msg := "update_download_failed", "更新下载失败，请重试。"
	if errors.As(err, &ce) {
		code, msg = ce.code, ce.message
	}
	m.state.State = UpdateError
	m.state.DownloadedBytes = 0
	m.state.BytesPerSecond = nil
	m.state.ErrorCode = code
	m.state.ErrorMessage = msg
	m.publishLocked()
	return m.cloneStateLocked(), err
}

func (m *updateManager) cancelUpdate() UpdateState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.State == UpdateApplying {
		return m.cloneStateLocked()
	}
	m.opSeq++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.removeStagedLocked()
	m.state.DownloadedBytes = 0
	m.state.BytesPerSecond = nil
	m.state.ErrorCode = ""
	m.state.ErrorMessage = ""
	if m.state.Release != nil {
		m.state.State = UpdateAvailable
		if m.state.Release.AssetSize > 0 {
			m.state.TotalBytes = ptr(m.state.Release.AssetSize)
		} else {
			m.state.TotalBytes = nil
		}
	} else {
		m.state.State = UpdateIdle
		m.state.TotalBytes = nil
	}
	m.publishLocked()
	return m.cloneStateLocked()
}
