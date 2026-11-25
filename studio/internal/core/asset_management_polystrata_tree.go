package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
)

const (
	polystrataPinnedSampleBytes  = int64(9150)
	polystrataPinnedSampleSHA256 = "27c7f41b753d89d170cd2d1ed7af449e3cf1e8794c6ca6bb937a6ec3734540e5"
)

var polystrataKnownSampleRelPaths = []string{
	"bench/results/2026-10-09-community-gfx1151-ud-iq4xs-0.1.41/raw/events.jsonl",
	"docs/bench-results/2026-10-09-community-gfx1151-ud-iq4xs-0.1.41/raw/events.jsonl",
	"terminal/bench/results/2026-10-09-community-gfx1151-ud-iq4xs-0.1.41/raw/events.jsonl",
}

func isKnownPolystrataArchiveSample(archiveSHA, canonRoot, fullPath string, d fs.DirEntry) bool {
	if archiveSHA == "" || d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
		return false
	}
	rel, err := filepath.Rel(canonRoot, fullPath)
	if err != nil {
		return false
	}
	slashRel := filepath.ToSlash(rel)
	if !slices.Contains(polystrataKnownSampleRelPaths, slashRel) {
		return false
	}
	info, err := d.Info()
	if err != nil || info.Size() != polystrataPinnedSampleBytes {
		return false
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return false
	}
	h := sha256.New()
	n, copyErr := io.Copy(h, io.LimitReader(f, polystrataPinnedSampleBytes+1))
	_ = f.Close()
	return copyErr == nil && n == polystrataPinnedSampleBytes && hex.EncodeToString(h.Sum(nil)) == polystrataPinnedSampleSHA256
}

// isAllowedPolystrataVenvSymlink permits standard python* interpreter symlinks inside a genuine
// non-symlink .venv/bin or .venv/Scripts directory of a managed PolyStrata runtime (at either
// root/.venv or root/terminal/.venv), while rejecting unrelated or arbitrary external targets.
func isAllowedPolystrataVenvSymlink(canonInstallDir, linkPath string, resultsRoot *string) bool {
	matchedVenv := false
	for _, venvDir := range []string{
		filepath.Join(canonInstallDir, ".venv"),
		filepath.Join(canonInstallDir, "terminal", ".venv"),
	} {
		if venvDir != canonInstallDir {
			outer := filepath.Dir(venvDir)
			if outer != canonInstallDir {
				outerInfo, oerr := os.Lstat(outer)
				if oerr != nil || outerInfo.Mode()&os.ModeSymlink != 0 || !outerInfo.IsDir() {
					continue
				}
			}
		}
		venvInfo, err := os.Lstat(venvDir)
		if err != nil || venvInfo.Mode()&os.ModeSymlink != 0 || !venvInfo.IsDir() {
			continue
		}
		if !isRegularNonSymlink(filepath.Join(venvDir, "pyvenv.cfg")) {
			continue
		}
		parent := filepath.Dir(linkPath)
		if parent != filepath.Join(venvDir, "bin") && parent != filepath.Join(venvDir, "Scripts") {
			continue
		}
		parentInfo, err := os.Lstat(parent)
		if err != nil || parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
			continue
		}
		matchedVenv = true
		break
	}
	if !matchedVenv || !polystrataVenvPythonName.MatchString(filepath.Base(linkPath)) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		rawTarget, rerr := os.Readlink(linkPath)
		if rerr != nil {
			return false
		}
		targetBase := filepath.Base(rawTarget)
		return polystrataPythonTargetName.MatchString(targetBase) && !isSealedEvidenceMarker(targetBase)
	}
	if isSealedEvidenceMarker(filepath.Base(resolved)) || overlapsResultsRoot(resolved, resultsRoot) {
		return false
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil || !info.Mode().IsRegular() {
		return false
	}
	return polystrataPythonTargetName.MatchString(filepath.Base(resolved))
}

func verifyPolystrataRuntimeTreeSafe(canonInstallDir string, resultsRoot *string) error {
	canonRoot, err := canonical(canonInstallDir)
	if err != nil {
		return errUnsafeAssetTree
	}
	archiveSHA, _ := readPolystrataRuntimeReceiptSHA256(canonRoot)
	return filepath.WalkDir(canonRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && isSealedEvidenceMarker(d.Name()) {
			if d.Type()&os.ModeSymlink == 0 && isKnownPolystrataArchiveSample(archiveSHA, canonRoot, path, d) {
				return nil
			}
			return errUnsafeAssetTree
		}
		if d.Type()&os.ModeSymlink != 0 {
			if isAllowedPolystrataVenvSymlink(canonRoot, path, resultsRoot) {
				return nil
			}
			resolved, linkErr := filepath.EvalSymlinks(path)
			if linkErr != nil || !withinCanonicalDir(canonRoot, resolved) || isSealedEvidenceMarker(filepath.Base(resolved)) || overlapsResultsRoot(resolved, resultsRoot) {
				return errUnsafeAssetTree
			}
			return nil
		}
		return nil
	})
}

func checkDirTreeWritable(root string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if info.Mode().Perm()&0o300 != 0o300 {
				return errUnsafeAssetTree
			}
		}
		return nil
	})
}

func dirDiskUsageNoSymlinks(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.Type()&os.ModeSymlink != 0 {
			if d != nil && d.Type()&os.ModeSymlink != 0 && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func expandSplitGGUFShardsLenient(canonPath string) []assetFileSize {
	m := splitGGUFPattern.FindStringSubmatch(filepath.Base(canonPath))
	if m != nil {
		stem, idxStr, totalStr, ext := m[1], m[2], m[3], m[4]
		index, err1 := strconv.Atoi(idxStr)
		total, err2 := strconv.Atoi(totalStr)
		if err1 == nil && err2 == nil && total >= 2 && total <= 256 && index >= 1 && index <= total {
			dir := filepath.Dir(canonPath)
			var shards []assetFileSize
			for i := 1; i <= total; i++ {
				shardPath := filepath.Join(dir, fmt.Sprintf("%s-%05d-of-%05d%s", stem, i, total, ext))
				info, err := os.Lstat(shardPath)
				if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
					continue
				}
				canonShard, cerr := canonical(shardPath)
				if cerr != nil {
					canonShard = shardPath
				}
				appendUniqueAssetFile(&shards, assetFileSize{path: canonShard, size: info.Size()})
			}
			return shards
		}
	}
	info, err := os.Lstat(canonPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil
	}
	return []assetFileSize{{path: canonPath, size: info.Size()}}
}

func collectBrokenSplitGGUFShards(ref string, bases []string) []assetFileSize {
	baseName := filepath.Base(ref)
	if !splitGGUFPattern.MatchString(baseName) {
		return nil
	}
	var candidateDirs []string
	if filepath.IsAbs(ref) {
		candidateDirs = []string{filepath.Dir(ref)}
	} else {
		relDir := filepath.Dir(ref)
		for _, b := range bases {
			candidateDirs = append(candidateDirs, filepath.Join(b, relDir))
		}
	}
	var shards []assetFileSize
	for _, dir := range candidateDirs {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		canonDir, cerr := canonical(dir)
		if cerr != nil {
			continue
		}
		for _, sh := range expandSplitGGUFShardsLenient(filepath.Join(canonDir, baseName)) {
			appendUniqueAssetFile(&shards, sh)
		}
	}
	return shards
}
