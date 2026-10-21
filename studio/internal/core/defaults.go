package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The built-in address list lives in catalog.go; these two helpers keep the
// prefilled download URLs pointing at it, so one pinned asset is described
// in exactly one place.

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
//
// The archive is streamed to disk and unpacked from there, so a large pinned
// archive costs disk space rather than the whole file in memory.
func (s *Service) FetchEngine(ctx context.Context, source, outDir string, onStatus func(string)) (string, *ApiError) {
	if !validEngineSource(source) {
		return "", &ApiError{Code: "invalid_request", Message: "引擎 URL 不符合要求。"}
	}
	if !filepath.IsAbs(outDir) || strings.ContainsRune(outDir, 0) {
		return "", &ApiError{Code: "invalid_request", Message: "引擎输出目录不符合要求。"}
	}
	if ctx.Err() != nil {
		return "", Err("cancelled")
	}
	if _, err := os.Lstat(outDir); !os.IsNotExist(err) {
		if err != nil {
			return "", Err("internal_error")
		}
		return "", &ApiError{Code: "output_exists", Message: "输出目录已存在，请选择新目录。"}
	}
	if err := os.MkdirAll(filepath.Dir(outDir), 0755); err != nil {
		return "", Err("internal_error")
	}
	base := filepath.Base(source)
	// A plain file is only an engine when it is the engine binary itself.
	// Anything else — a Python wheel, a runtime archive — is not an engine,
	// and fetching it would only leave a file that cannot be registered.
	if !isEngineArchive(base) && base != engineBinaryName() {
		return "", &ApiError{
			Code:    "engine_binary_missing",
			Message: "该地址不是引擎可执行文件或 llama.cpp 归档；请按内置地址的说明自行准备。",
		}
	}
	onStatus("正在下载引擎…")
	// The archive cannot live inside outDir: that directory must not exist
	// until the download succeeded. It sits in a hidden sibling instead, so
	// it never mixes with the engines the registry scans.
	staging, mkErr := os.MkdirTemp(filepath.Dir(outDir), ".engine-download-")
	if mkErr != nil {
		return "", Err("internal_error")
	}
	defer os.RemoveAll(staging)
	archivePath, e := fetchURLToFile(ctx, source, staging)
	if e != nil {
		return "", e
	}
	defer os.Remove(archivePath)
	if ctx.Err() != nil {
		return "", Err("cancelled")
	}
	if err := os.Mkdir(outDir, 0755); err != nil {
		if os.IsExist(err) {
			return "", &ApiError{Code: "output_exists", Message: "输出目录已存在，请选择新目录。"}
		}
		return "", Err("internal_error")
	}
	var enginePath string
	switch {
	case strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz"):
		onStatus("正在解压引擎…")
		enginePath, e = unpackEngineTarGz(ctx, archivePath, outDir)
	case strings.HasSuffix(base, ".zip"):
		onStatus("正在解压引擎…")
		enginePath, e = unpackEngineZip(ctx, archivePath, outDir)
	default:
		target := filepath.Join(outDir, base)
		file, openErr := os.Open(archivePath)
		if openErr != nil {
			return "", Err("internal_error")
		}
		e = writeBinaryTo(engineContextReader{ctx, file}, target, "engine_unpack_failed", "引擎文件写入失败，请检查来源后重试。")
		file.Close()
		if e == nil {
			enginePath = target
		}
	}
	if e != nil {
		return "", e
	}
	if ctx.Err() != nil {
		return "", Err("cancelled")
	}
	if enginePath == "" {
		return "", &ApiError{Code: "engine_binary_missing", Message: "归档内未找到 llama-server，请检查引擎来源。"}
	}
	if err := os.Chmod(enginePath, 0755); err != nil {
		return "", Err("internal_error")
	}
	return enginePath, nil
}

// isEngineArchive reports whether a file name is an archive shape the engine
// unpack path understands.
func isEngineArchive(base string) bool {
	return strings.HasSuffix(base, ".tar.gz") || strings.HasSuffix(base, ".tgz") || strings.HasSuffix(base, ".zip")
}

// EngineRoots returns the directories that may hold fetched engines, most
// specific first: the workspace engines directory (FetchEngine's target),
// then the user-level InferYard engine directory.
func EngineRoots(workspace string) []string {
	roots := []string{}
	if workspace != "" {
		roots = append(roots, filepath.Join(workspace, "engines"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".inferyard", "engine"))
	}
	return roots
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

// unpackEngineTarGz unpacks a gzipped tar archive from disk, keeping the
// sibling shared libraries the engine needs.
func unpackEngineTarGz(ctx context.Context, archivePath, destDir string) (string, *ApiError) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", Err("internal_error")
	}
	defer file.Close()
	zr, err := gzip.NewReader(engineContextReader{ctx, file})
	if err != nil {
		return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	enginePath := ""
	for {
		if ctx.Err() != nil {
			return "", Err("cancelled")
		}
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
			if e := writeBinaryTo(engineContextReader{ctx, tr}, target, "engine_unpack_failed", "引擎归档写入失败，请检查来源后重试。"); e != nil {
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

// unpackEngineZip unpacks a zip archive from disk.
func unpackEngineZip(ctx context.Context, archivePath, destDir string) (string, *ApiError) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", &ApiError{Code: "engine_unpack_failed", Message: "引擎归档损坏，请检查来源后重试。"}
	}
	defer zr.Close()
	enginePath := ""
	for _, f := range zr.File {
		if ctx.Err() != nil {
			return "", Err("cancelled")
		}
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
		e := writeBinaryTo(engineContextReader{ctx, rc}, target, "engine_unpack_failed", "引擎归档写入失败，请检查来源后重试。")
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

type engineContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r engineContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// fetchURLToFile streams a URL into a temporary file in dir and returns its
// path. The archive is never held in memory, so a large pinned archive costs
// disk space instead.
func fetchURLToFile(ctx context.Context, source, dir string) (string, *ApiError) {
	resp, e := httpGet(ctx, source)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", Err("internal_error")
	}
	handle, err := os.CreateTemp(dir, "archive-*")
	if err != nil {
		return "", Err("internal_error")
	}
	path := handle.Name()
	_, copyErr := io.Copy(handle, engineContextReader{ctx, resp.Body})
	closeErr := handle.Close()
	if copyErr != nil {
		os.Remove(path)
		if ctx.Err() != nil {
			return "", Err("cancelled")
		}
		return "", &ApiError{Code: "download_failed", Message: "下载中断，请重试。", Details: map[string]any{"url": source}}
	}
	if closeErr != nil {
		os.Remove(path)
		return "", Err("internal_error")
	}
	return path, nil
}
