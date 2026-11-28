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
	"path/filepath"
	"runtime"
	"strings"
)

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
