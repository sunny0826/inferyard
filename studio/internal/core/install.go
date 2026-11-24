package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"inferyard/studio/internal/l10n"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const installPython = "3.15.2"

const uvRelease = "0.13.6"

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

func extraBinDirs() []string {
	if runtime.GOOS == "darwin" {
		return []string{"/opt/homebrew/bin", "/usr/local/bin"}
	}
	return nil
}

func cliCandidatePaths(configured string) []string {
	var raw []string
	if trimmed := strings.TrimSpace(configured); trimmed != "" {
		if st, err := installStat(trimmed); err == nil && !st.IsDir() {
			raw = append(raw, trimmed)
		}
	}
	if p, err := installLookPath(cliBinaryName()); err == nil {
		raw = append(raw, p)
	}
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin", cliBinaryName()))
	}
	for _, dir := range extraBinDirs() {
		dirs = append(dirs, filepath.Join(dir, cliBinaryName()))
	}
	for _, p := range dirs {
		if st, err := installStat(p); err == nil && !st.IsDir() {
			raw = append(raw, p)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, cand := range raw {
		abs, err := filepath.Abs(cand)
		if err != nil {
			abs = cand
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	return out
}

func FindCLI() string {
	if paths := cliCandidatePaths(""); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func uvCandidatePaths(toolsDir string) []string {
	raw := []string{}
	if p, err := installLookPath(uvBinaryName()); err == nil {
		raw = append(raw, p)
	}
	if strings.TrimSpace(toolsDir) != "" {
		raw = append(raw, filepath.Join(toolsDir, uvBinaryName()))
	}
	if home, err := os.UserHomeDir(); err == nil {
		raw = append(raw,
			filepath.Join(home, ".local", "bin", uvBinaryName()),
			filepath.Join(home, ".cargo", "bin", uvBinaryName()),
		)
	}
	for _, dir := range extraBinDirs() {
		raw = append(raw, filepath.Join(dir, uvBinaryName()))
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range raw {
		st, err := installStat(p)
		if err != nil || st.IsDir() {
			continue
		}
		key := p
		if abs, err := filepath.Abs(p); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func FindUV(toolsDir string) string {
	if paths := uvCandidatePaths(toolsDir); len(paths) > 0 {
		return paths[0]
	}
	return ""
}

func parseUVVersion(raw string) (semver, bool) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) < 2 {
		return semver{}, false
	}
	exe := strings.ToLower(fields[0])
	if exe != "uv" && exe != "uv.exe" {
		return semver{}, false
	}
	verToken := fields[1]
	if base, _, ok := strings.Cut(verToken, "+"); ok {
		verToken = base
	}
	return parseSemver(verToken)
}

func probeUVVersion(ctx context.Context, path string) (semver, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := installCommandOutput(probeCtx, path, "--version")
	if err != nil {
		return semver{}, false
	}
	return parseUVVersion(string(out))
}

func findCompatibleUV(ctx context.Context, toolsDir string) (string, bool) {
	minVer, hasMin := parseSemver(uvRelease)
	candidates := uvCandidatePaths(toolsDir)
	sawIncompatible := false
	for _, cand := range candidates {
		if !hasMin {
			return cand, false
		}
		ver, ok := probeUVVersion(ctx, cand)
		if ok && compareSemver(ver, minVer) >= 0 {
			return cand, sawIncompatible
		}
		sawIncompatible = true
	}
	return "", sawIncompatible
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
		return "", &ApiError{Code: "install_unsupported", Message: l10n.TR("core.install.err.unsupported_platform"), Limitations: []string{"platform:" + runtime.GOOS + "/" + runtime.GOARCH}}
	}
	return name, nil
}

const maxFetchBytes = 64 << 20

func httpGet(ctx context.Context, url string) (*http.Response, *ApiError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, Err("internal_error")
	}
	resp, err := installHTTPDo(req)
	if err != nil {
		return nil, &ApiError{Code: "download_failed", Message: l10n.TR("core.install.err.download_failed"), Details: map[string]any{"url": url, "error": err.Error()}}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &ApiError{Code: "download_failed", Message: l10n.TR("core.install.err.download_failed"), Details: map[string]any{"url": url, "status": resp.StatusCode}}
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
		return nil, &ApiError{Code: "download_too_large", Message: l10n.TR("core.install.err.download_too_large"), Details: map[string]any{"url": url, "content_length": resp.ContentLength}}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, &ApiError{Code: "download_failed", Message: l10n.TR("core.install.err.download_interrupted"), Details: map[string]any{"url": url}}
	}
	if len(body) > maxFetchBytes {
		return nil, &ApiError{Code: "download_too_large", Message: l10n.TR("core.install.err.download_too_large"), Details: map[string]any{"url": url}}
	}
	return body, nil
}

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
		return "", &ApiError{Code: "install_checksum_failed", Message: l10n.TR("core.install.err.checksum_file_invalid")}
	}
	sum := sha256.Sum256(archive)
	if !strings.EqualFold(want[0], hex.EncodeToString(sum[:])) {
		return "", &ApiError{Code: "install_checksum_failed", Message: l10n.TR("core.install.err.checksum_mismatch")}
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
		return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.archive_corrupted")}
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.archive_corrupted")}
		}
		if filepath.Base(h.Name) != uvBinaryName() || h.Typeflag != tar.TypeReg {
			continue
		}
		return writeBinary(tr, target)
	}
	return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.uv_binary_missing")}
}

func unpackUVZip(archive []byte, target string) *ApiError {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.archive_corrupted")}
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) != uvBinaryName() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.archive_corrupted")}
		}
		defer rc.Close()
		return writeBinary(rc, target)
	}
	return &ApiError{Code: "install_unpack_failed", Message: l10n.TR("core.install.err.uv_binary_missing")}
}

func writeBinary(src io.Reader, target string) *ApiError {
	return writeBinaryTo(src, target, "install_unpack_failed", l10n.TR("core.install.err.write_tools_failed"))
}

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

func (s *Service) inspectCLIForInstall(u UserSettings) (string, *InferyardProbe) {
	var outdated *InferyardProbe
	for _, cand := range cliCandidatePaths(str(u.InferyardPath)) {
		p := s.probeCLI(cand)
		if p.Status == "ok" && p.Path != nil {
			return *p.Path, nil
		}
		if p.Status == "outdated" && outdated == nil {
			copyP := copyProbe(p)
			outdated = &copyP
		}
	}
	return "", outdated
}

func (s *Service) InstallCLI(ctx context.Context, onStatus func(string)) (string, *ApiError) {
	if s.paths != nil {
		p := s.probe(s.userSettings())
		if p.Status == "outdated" {
			return "", cliOutdatedError(p.Version)
		}
		if p.Status != "ok" || p.Path == nil || *p.Path != s.paths.CLI {
			return "", Conflict("cli_binding_failed")
		}
		return s.paths.CLI, nil
	}
	report := func(message string) {
		if onStatus != nil {
			onStatus(message)
		}
	}
	toolsDir := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	reusable, outdated := s.inspectCLIForInstall(s.userSettings())
	if reusable != "" {
		return s.finishInstall(reusable, report)
	}
	targetVersion := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if _, ok := parseSemver(targetVersion); !ok {
		return "", &ApiError{Code: "install_version_unknown", Message: l10n.TR("core.install.err.version_unknown")}
	}
	uv, sawIncompatibleUV := findCompatibleUV(ctx, toolsDir)
	if uv == "" {
		if sawIncompatibleUV {
			report(l10n.TR("core.install.status.uv_outdated_downloading"))
		} else {
			report(l10n.TR("core.install.status.uv_missing_downloading"))
		}
		var e *ApiError
		uv, e = downloadUV(ctx, toolsDir)
		if e != nil {
			return "", e
		}
	}
	if outdated != nil {
		report(l10n.TRf("core.install.status.upgrading_cli", "version", targetVersion))
	} else {
		report(l10n.TRf("core.install.status.installing_cli", "version", targetVersion))
	}
	installCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	out, err := installCommandOutput(installCtx, uv, "tool", "install", "--upgrade", "--force", "--managed-python", "--python", installPython, "inferyard=="+targetVersion)
	if err != nil {
		tail := string(out)
		if len(tail) > 600 {
			tail = tail[len(tail)-600:]
		}
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.auto_install_failed"), Details: map[string]any{"output_tail": tail}}
	}
	binOut, err := installCommandOutput(ctx, uv, "tool", "dir", "--bin")
	if err != nil {
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.locate_result_failed")}
	}
	installed := filepath.Join(strings.TrimSpace(string(binOut)), cliBinaryName())
	if st, err := installStat(installed); err != nil || st.IsDir() {
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.result_missing")}
	}
	s.clearProbeCache()
	if p := s.probeCLI(installed); p.Status != "ok" {
		if p.Status == "outdated" {
			return "", cliOutdatedError(p.Version)
		}
		return "", &ApiError{Code: "install_failed", Message: l10n.TR("core.install.err.post_install_broken")}
	}
	return s.finishInstall(installed, report)
}

func (s *Service) saveInstalledCLIPath(path string) (SettingsResponse, *ApiError) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	u := s.userSettings()
	u.InferyardPath = ptr(path)
	return s.persistSettings(u, true)
}

func (s *Service) finishInstall(path string, onStatus func(string)) (string, *ApiError) {
	if _, e := s.saveInstalledCLIPath(path); e != nil {
		return "", e
	}
	if onStatus != nil {
		onStatus(l10n.TRf("core.install.status.cli_ready", "path", path))
	}
	return path, nil
}
