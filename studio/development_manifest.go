//go:build studio_dev && !release

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"inferyard/studio/internal/core"
)

type devManifest struct {
	Kind          string `json:"kind"`
	Checkout      string `json:"checkout"`
	BuildRoot     string `json:"build_root"`
	ProfileRoot   string `json:"profile_root"`
	AssetsRoot    string `json:"assets_root"`
	Branch        string `json:"branch"`
	Commit        string `json:"commit"`
	Dirty         bool   `json:"dirty"`
	SourceSHA256  string `json:"source_sha256"`
	StudioSHA256  string `json:"studio_sha256"`
	CLISHA256     string `json:"cli_sha256"`
	PackageSHA256 string `json:"package_sha256"`
	PackageRoot   string `json:"package_root"`
	path          string
}

func loadDevManifest(path string) (*devManifest, error) {
	var m devManifest
	if err := readDevJSON(path, &m); err != nil {
		return nil, err
	}
	m.path = path
	if m.Kind != "studio_dev.v1" || m.Branch == "" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(m.Commit) {
		return nil, fmt.Errorf("invalid development manifest identity")
	}
	for _, hash := range []string{m.SourceSHA256, m.StudioSHA256, m.CLISHA256, m.PackageSHA256} {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
			return nil, fmt.Errorf("invalid manifest digest")
		}
	}
	for _, p := range []string{path, m.Checkout, m.BuildRoot, m.ProfileRoot, m.AssetsRoot, m.PackageRoot} {
		if err := canonicalDevPath(p); err != nil {
			return nil, err
		}
	}
	root := filepath.Join(m.Checkout, ".inferyard-dev")
	if !devDepth(filepath.Join(root, "builds"), m.BuildRoot, 1) || !devDepth(filepath.Join(root, "profiles"), m.ProfileRoot, 2) || m.AssetsRoot != filepath.Join(root, "assets") || !devDepth(filepath.Join(root, "launches"), path, 2) || filepath.Base(path) != "manifest.json" {
		return nil, fmt.Errorf("development paths outside launch layout")
	}
	if resolved, err := core.Resolve(filepath.Join(m.BuildRoot, "cli-env"), m.PackageRoot); err != nil || resolved != m.PackageRoot || filepath.Base(m.PackageRoot) != "inferyard" {
		return nil, fmt.Errorf("package outside CLI environment")
	}
	return &m, nil
}

func canonicalDevPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("development path must be absolute and clean: %s", path)
	}
	resolved, err := core.Resolve(path, path)
	if err != nil || resolved != path {
		return fmt.Errorf("development path is not canonical: %s", path)
	}
	return nil
}

func devDepth(root, path string, depth int) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && len(strings.Split(rel, string(filepath.Separator))) == depth
}

func (m *devManifest) cliPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(m.BuildRoot, "cli-env", "Scripts", "inferyard.exe")
	}
	return filepath.Join(m.BuildRoot, "cli-env", "bin", "inferyard")
}
func (m *devManifest) pythonPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(m.BuildRoot, "cli-env", "Scripts", "python.exe")
	}
	return filepath.Join(m.BuildRoot, "cli-env", "bin", "python")
}
func (m *devManifest) studioPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(m.BuildRoot, "InferYard Studio Dev.app", "Contents", "MacOS", "inferyard-studio")
	}
	name := "inferyard-studio"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(m.BuildRoot, name)
}
func fileDigest(path string) (string, error) {
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
func checkDigest(path, expected string) error {
	if err := canonicalDevPath(path); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	got, err := fileDigest(path)
	if err != nil {
		return err
	}
	if got != expected {
		return fmt.Errorf("digest mismatch: %s", path)
	}
	return nil
}
func (m *devManifest) checkSelf() error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if path != m.studioPath() {
		return fmt.Errorf("running Studio does not match manifest")
	}
	return checkDigest(path, m.StudioSHA256)
}
func (m *devManifest) checkCLIArtifacts() error {
	if err := checkDigest(m.cliPath(), m.CLISHA256); err != nil {
		return err
	}
	if err := canonicalDevPath(m.PackageRoot); err != nil {
		return err
	}
	hash, err := packageDigest(m.PackageRoot)
	if err != nil {
		return err
	}
	if hash != m.PackageSHA256 {
		return fmt.Errorf("installed package digest mismatch")
	}
	return nil
}
func (m *devManifest) checkCLI(ctx context.Context) error {
	if err := m.checkCLIArtifacts(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.pythonPath(), "-I", "-X", "utf8", "-c", "import inferyard,pathlib; print(pathlib.Path(inferyard.__file__).resolve().parent)")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("isolated CLI import failed: %w", err)
	}
	if strings.TrimSpace(string(out)) != m.PackageRoot {
		return fmt.Errorf("isolated CLI imported a different package")
	}
	return nil
}
func packageDigest(root string) (string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "__pycache__" {
			return filepath.SkipDir
		}
		if strings.HasSuffix(entry.Name(), ".pyc") || strings.HasSuffix(entry.Name(), ".pyo") {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular package file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		io.WriteString(h, rel+"\x00")
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (m *devManifest) boundPaths() *core.BoundPaths {
	title := "InferYard Studio · 开发环境 · " + m.Branch + " · " + m.Commit[:8]
	label := m.Branch + " · " + m.Commit[:8]
	if m.Dirty {
		title += " · dirty"
		label += " · 已修改"
	}
	workspace := filepath.Join(m.ProfileRoot, "workspace")
	return &core.BoundPaths{CLI: m.cliPath(), Workspace: workspace, Results: filepath.Join(workspace, "results"), Models: filepath.Join(m.AssetsRoot, "models"), Engines: filepath.Join(m.AssetsRoot, "engines"), Settings: filepath.Join(m.ProfileRoot, "settings.json"), Source: "当前 checkout · " + m.Checkout, WindowTitle: title, SourceLabel: label}
}
