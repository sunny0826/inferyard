package core

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"inferyard/studio/internal/l10n"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxUpdateTarEntries = 500
	maxUpdateTarBytes   = 512 << 20
)

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
			message: l10n.TR("core.update.err.linux_dir_unwritable"),
			cause:   err,
		}
	}
	srcExe := filepath.Join(extractedRoot, "inferyard-studio")
	if err := atomicCopyFile(srcExe, exePath, 0o755); err != nil {
		return &updateCheckError{
			code:    "update_apply_failed",
			message: l10n.TR("core.update.err.linux_replace_binary_failed"),
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
