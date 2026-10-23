package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// installPython pins the managed Python build the CLI contract requires; keep
// it in sync with cli/docs/installation.md.
const installPython = "3.15.1"

// uvRelease pins the standalone uv build downloaded when no uv is found.
const uvRelease = "0.13.5"

// Replaceable in tests.
var (
	installHTTPDo        = http.DefaultClient.Do
	installLookPath      = exec.LookPath
	installStat          = os.Stat
	installCommandOutput = func(ctx context.Context, path string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, args...)
		return cmd.CombinedOutput()
	}
)

func cliBinaryName() string {
	if runtime.GOOS == "windows" {
		return "inferyard.exe"
	}
	return "inferyard"
}

func uvBinaryName() string {
	if runtime.GOOS == "windows" {
		return "uv.exe"
	}
	return "uv"
}

// extraBinDirs covers tool locations outside the minimal PATH of GUI-launched
// desktop apps (Homebrew on Apple Silicon is the common case on macOS).
func extraBinDirs() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	}
	return nil
}

// FindCLI locates an existing inferyard executable: PATH first, then the
// default uv tools bin directory.
func FindCLI() string {
	if p, err := installLookPath(cliBinaryName()); err == nil {
		return p
	}
	candidates := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", cliBinaryName()))
	}
	for _, dir := range extraBinDirs() {
		candidates = append(candidates, filepath.Join(dir, cliBinaryName()))
	}
	for _, p := range candidates {
		if st, err := installStat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// FindUV locates a uv executable: the Studio tools directory, PATH, then the
// default per-user install locations.
func FindUV(toolsDir string) string {
	candidates := []string{filepath.Join(toolsDir, uvBinaryName())}
	if p, err := installLookPath(uvBinaryName()); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", uvBinaryName()),
			filepath.Join(home, ".cargo", "bin", uvBinaryName()),
		)
	}
	for _, dir := range extraBinDirs() {
		candidates = append(candidates, filepath.Join(dir, uvBinaryName()))
	}
	for _, p := range candidates {
		if st, err := installStat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func uvAsset() (string, *ApiError) {
	name := ""
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		name = "uv-aarch64-apple-darwin.tar.gz"
	case "darwin/amd64":
		name = "uv-x86_64-apple-darwin.tar.gz"
	case "linux/amd64":
		name = "uv-x86_64-unknown-linux-gnu.tar.gz"
	case "linux/arm64":
		name = "uv-aarch64-unknown-linux-gnu.tar.gz"
	case "windows/amd64":
		name = "uv-x86_64-pc-windows-msvc.zip"
	default:
		return "", &ApiError{Code: "install_unsupported", Message: "当前平台没有可用的自动安装包。", Limitations: []string{"platform:" + runtime.GOOS + "/" + runtime.GOARCH}}
	}
	return name, nil
}

const maxFetchBytes = 64 << 20

// httpGet opens a GET request and checks the status, so every download path
// reports the same failure for a dead or refused URL.
func httpGet(ctx context.Context, url string) (*http.Response, *ApiError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Err("internal_error")
	}
	resp, err := installHTTPDo(req)
	if err != nil {
		return nil, &ApiError{Code: "download_failed", Message: "下载失败，请检查网络后重试。", Details: map[string]any{"url": url, "error": err.Error()}}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &ApiError{Code: "download_failed", Message: "下载失败，请检查网络后重试。", Details: map[string]any{"url": url, "status": resp.StatusCode}}
	}
	return resp, nil
}

func fetchURL(ctx context.Context, url string) ([]byte, *ApiError) {
	resp, e := httpGet(ctx, url)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxFetchBytes {
		return nil, &ApiError{Code: "download_too_large", Message: "下载超出 64 MiB 上限，请检查来源。", Details: map[string]any{"url": url, "content_length": resp.ContentLength}}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, &ApiError{Code: "download_failed", Message: "下载中断，请重试。", Details: map[string]any{"url": url}}
	}
	if len(body) > maxFetchBytes {
		return nil, &ApiError{Code: "download_too_large", Message: "下载超出 64 MiB 上限，请检查来源。", Details: map[string]any{"url": url}}
	}
	return body, nil
}

// downloadUV fetches the pinned standalone uv archive plus its release
// checksum, verifies it, and unpacks the uv binary into toolsDir.
func downloadUV(ctx context.Context, toolsDir string) (string, *ApiError) {
	asset, e := uvAsset()
	if e != nil {
		return "", e
	}
	base := "https://github.com/astral-sh/uv/releases/download/" + uvRelease + "/"
	archive, e := fetchURL(ctx, base+asset)
	if e != nil {
		return "", e
	}
	sumText, e := fetchURL(ctx, base+asset+".sha256")
	if e != nil {
		return "", e
	}
	want := strings.Fields(string(sumText))
	if len(want) == 0 {
		return "", &ApiError{Code: "install_checksum_failed", Message: "校验文件内容异常，已中止安装。"}
	}
	sum := sha256.Sum256(archive)
	if !strings.EqualFold(want[0], hex.EncodeToString(sum[:])) {
		return "", &ApiError{Code: "install_checksum_failed", Message: "下载内容与官方校验和不一致，已中止安装。"}
	}
	if err := os.MkdirAll(toolsDir, 0700); err != nil {
		return "", Err("internal_error")
	}
	target := filepath.Join(toolsDir, uvBinaryName())
	if strings.HasSuffix(asset, ".zip") {
		e = unpackUVZip(archive, target)
	} else {
		e = unpackUVTarGz(archive, target)
	}
	if e != nil {
		return "", e
	}
	if err := os.Chmod(target, 0755); err != nil {
		return "", Err("internal_error")
	}
	return target, nil
}

func unpackUVTarGz(archive []byte, target string) *ApiError {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return &ApiError{Code: "install_unpack_failed", Message: "安装包损坏，已中止安装。"}
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &ApiError{Code: "install_unpack_failed", Message: "安装包损坏，已中止安装。"}
		}
		if filepath.Base(h.Name) != uvBinaryName() || h.Typeflag != tar.TypeReg {
			continue
		}
		return writeBinary(tr, target)
	}
	return &ApiError{Code: "install_unpack_failed", Message: "安装包内未找到 uv 可执行文件。"}
}

func unpackUVZip(archive []byte, target string) *ApiError {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return &ApiError{Code: "install_unpack_failed", Message: "安装包损坏，已中止安装。"}
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != uvBinaryName() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return &ApiError{Code: "install_unpack_failed", Message: "安装包损坏，已中止安装。"}
		}
		defer rc.Close()
		return writeBinary(rc, target)
	}
	return &ApiError{Code: "install_unpack_failed", Message: "安装包内未找到 uv 可执行文件。"}
}

func writeBinary(src io.Reader, target string) *ApiError {
	return writeBinaryTo(src, target, "install_unpack_failed", "写入工具目录失败。")
}

// writeBinaryTo writes a binary file into place: it writes a temporary file
// first and renames it, so a half-written file is never visible under the
// target name.
func writeBinaryTo(src io.Reader, target, code, message string) *ApiError {
	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return Err("internal_error")
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp)
		return &ApiError{Code: code, Message: message}
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return Err("internal_error")
	}
	return nil
}

// InstallCLI installs inferyard==Version through uv and records the installed
// path in the settings. A configured executable is kept; otherwise an existing
// installation is reused. When uv is missing
// a pinned standalone build is downloaded into the Studio tools directory;
// user shell configuration is never modified. onStatus receives progress lines
// for the wizard UI.
func (s *Service) InstallCLI(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	if s.paths != nil {
		if p := s.probe(s.userSettings()); p.Status != "ok" || p.Path == nil || *p.Path != s.paths.CLI {
			return "", Conflict("cli_binding_failed")
		}
		return s.paths.CLI, nil
	}
	toolsDir := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	existing := strings.TrimSpace(str(s.userSettings().InferyardPath))
	if existing != "" {
		if st, err := installStat(existing); err != nil || st.IsDir() {
			existing = ""
		}
	}
	if existing == "" {
		existing = FindCLI()
	}
	if existing != "" {
		abs, err := filepath.Abs(existing)
		if err != nil {
			return "", Err("internal_error")
		}
		return s.finishInstall(abs, onStatus)
	}
	if strings.TrimSpace(Version) == "" || Version == "dev" {
		return "", &ApiError{Code: "install_version_unknown", Message: "无法确定要安装的 CLI 版本。"}
	}
	uv := FindUV(toolsDir)
	if uv == "" {
		onStatus("未找到 uv，正在下载独立工具…")
		var e *ApiError
		uv, e = downloadUV(ctx, toolsDir)
		if e != nil {
			return "", e
		}
	}
	onStatus("正在安装 InferYard CLI " + Version + "（首次需下载 Python 运行时，可能需要几分钟）…")
	installCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := installCommandOutput(installCtx, uv, "tool", "install", "--managed-python", "--python", installPython, "inferyard=="+Version)
	if err != nil {
		tail := string(out)
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		return "", &ApiError{Code: "install_failed", Message: "CLI 自动安装失败，可改用「手动指定路径」。", Details: map[string]any{"output_tail": tail}}
	}
	binOut, err := installCommandOutput(ctx, uv, "tool", "dir", "--bin")
	if err != nil {
		return "", &ApiError{Code: "install_failed", Message: "无法定位安装结果，请改用「手动指定路径」。"}
	}
	installed := filepath.Join(strings.TrimSpace(string(binOut)), cliBinaryName())
	if st, err := installStat(installed); err != nil || st.IsDir() {
		return "", &ApiError{Code: "install_failed", Message: "安装结果缺失，请改用「手动指定路径」。"}
	}
	return s.finishInstall(installed, onStatus)
}

// finishInstall records the CLI path so readiness flips and the wizard
// advances through the settings bus event.
func (s *Service) finishInstall(path string, onStatus func(string)) (string, *ApiError) {
	raw, _ := json.Marshal(path)
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": raw}); e != nil {
		return "", e
	}
	onStatus("CLI 已就绪：" + path)
	return path, nil
}
