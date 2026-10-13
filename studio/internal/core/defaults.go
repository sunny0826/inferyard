package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultModelURL is the built-in tiny test model (SmolLM2-135M Q4_K_M, about
// 100 MiB) so first-run users can pull a working model without hunting for a
// URL. The revision is pinned; bump deliberately with releases.
const DefaultModelURL = "https://huggingface.co/unsloth/SmolLM2-135M-Instruct-GGUF/blob/9e6855bc4be717fca1ef21360a1db4b29d5c559a/SmolLM2-135M-Instruct-Q4_K_M.gguf"

// engineReleaseBase is the pinned Prism llama.cpp release the CLI contracts
// reference (see cli/docs/contracts/windows-prism-batch-contract.md).
const engineReleaseBase = "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/"

// DefaultEngineURL returns the pinned Prism engine archive for the current
// platform, or "" when no CPU build is known.
func DefaultEngineURL() string {
	asset := ""
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		asset = "llama-prism-b10743-adfffbe-bin-macos-arm64.tar.gz"
	case "darwin/amd64":
		asset = "llama-prism-b10743-adfffbe-bin-macos-x64.tar.gz"
	case "linux/amd64":
		asset = "llama-prism-b10743-adfffbe-bin-ubuntu-x64.tar.gz"
	case "linux/arm64":
		asset = "llama-prism-b10743-adfffbe-bin-ubuntu-arm64.tar.gz"
	case "windows/amd64":
		asset = "llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip"
	}
	if asset == "" {
		return ""
	}
	return engineReleaseBase + asset
}

func engineBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func validEngineSource(source string) bool {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" {
		return false
	}
	for _, c := range source {
		if c < 33 || c >= 127 {
			return false
		}
	}
	return !strings.Contains(u.Path, "..")
}

// FetchEngine downloads a Prism engine archive into outDir, unpacks it keeping
// the sibling shared libraries the engine needs, and returns the llama-server
// path. Plain single-file URLs are stored unpacked. macOS has no CLI-side
// runtime prepare, so the Studio side owns archive fetching there.
func (s *Service) FetchEngine(ctx context.Context, source, outDir string, onStatus func(string)) (string, *ApiError) {
	if !validEngineSource(source) {
		return "", &ApiError{Code: "invalid_request", Message: "引擎 URL 不符合要求。"}
	}
	if !filepath.IsAbs(outDir) || strings.ContainsRune(outDir, 0) {
		return "", &ApiError{Code: "invalid_request", Message: "引擎输出目录不符合要求。"}
	}
	onStatus("正在下载引擎…")
	archive, e := fetchURL(ctx, source)
	if e != nil {
		return "", e
	}
	if st, err := os.Stat(outDir); err == nil && st.IsDir() {
		return "", &ApiError{Code: "output_exists", Message: "输出目录已存在，请选择新目录。"}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", Err("internal_error")
	}
	base := filepath.Base(source)
	var enginePath string
	switch {
	case strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz"):
		onStatus("正在解压引擎…")
		enginePath, e = unpackEngineTarGz(archive, outDir)
	case strings.HasSuffix(base, ".zip"):
		onStatus("正在解压引擎…")
		enginePath, e = unpackEngineZip(archive, outDir)
	default:
		target := filepath.Join(outDir, base)
		if e = writeBinary(bytes.NewReader(archive), target); e == nil {
			enginePath = target
		}
	}
	if e != nil {
		return "", e
	}
	if enginePath == "" {
		return "", &ApiError{Code: "engine_binary_missing", Message: "归档内未找到 llama-server，请检查引擎来源。"}
	}
	if err := os.Chmod(enginePath, 0755); err != nil {
		return "", Err("internal_error")
	}
	return enginePath, nil
}

// cleanEntryName rejects absolute paths and traversal in archive entries. The
// root "." entry is reported as skip, not as an error.
func cleanEntryName(name string) (clean string, skip, ok bool) {
	name = filepath.Clean(strings.TrimPrefix(name, "/"))
	if name == "." {
		return "", true, true
	}
	if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
		return "", false, false
	}
	return name, false, true
}

// safeLinkTarget permits only relative symlinks that stay inside the archive.
func safeLinkTarget(target string) bool {
	return target != "" && !filepath.IsAbs(target) && !strings.Contains(target, "..") && !strings.ContainsRune(target, 0)
}

func unpackEngineTarGz(archive []byte, destDir string) (string, *ApiError) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	enginePath := ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
		}
		name, skip, ok := cleanEntryName(h.Name)
		if !ok {
			return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档包含非法路径，已中止。"}
		}
		if skip {
			continue
		}
		target := filepath.Join(destDir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", Err("internal_error")
			}
		case tar.TypeSymlink:
			// Versioned shared libraries (libX.0.dylib -> libX.0.2.0.dylib)
			// are required by the engine loader; only in-archive relative
			// links are recreated.
			if !safeLinkTarget(h.Linkname) {
				return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档包含非法链接，已中止。"}
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", Err("internal_error")
			}
			os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return "", Err("internal_error")
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return "", Err("internal_error")
			}
			if e := writeBinary(tr, target); e != nil {
				return "", e
			}
			if filepath.Base(name) == engineBinaryName() {
				enginePath = target
			}
		default:
			// Device entries and other special files are not extracted.
		}
	}
	return enginePath, nil
}

func unpackEngineZip(archive []byte, destDir string) (string, *ApiError) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
	}
	enginePath := ""
	for _, f := range zr.File {
		name, skip, ok := cleanEntryName(f.Name)
		if !ok {
			return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档包含非法路径，已中止。"}
		}
		if skip {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(filepath.Join(destDir, name), 0755); err != nil {
				return "", Err("internal_error")
			}
			continue
		}
		target := filepath.Join(destDir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", Err("internal_error")
		}
		rc, err := f.Open()
		if err != nil {
			return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
		}
		e := writeBinary(rc, target)
		rc.Close()
		if e != nil {
			return "", e
		}
		if filepath.Base(name) == engineBinaryName() {
			enginePath = target
		}
	}
	return enginePath, nil
}
