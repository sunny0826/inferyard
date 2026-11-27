package core

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	llamaServerVersionLinePattern = regexp.MustCompile(`(?i)\bversion:\s*(\d{3,})\b`)
	urlReleaseTagPattern          = regexp.MustCompile(`/releases/download/([^/]+)/`)
)

type binaryVersionCacheEntry struct {
	key     string
	version string
}

// extractInstalledVersion resolves the installed engine version without spawning subprocesses
// unless allowProbe is true and static/metadata sources do not yield a version.
func (s *Service) extractInstalledVersion(ctx context.Context, entry registryEntry, familyID string, allowProbe bool) string {
	if familyID == "" {
		familyID = normalizeCatalogFamilyID(entry.engine.Family, entry.family, entry.engine.Name, entry.path)
	}
	if familyID == "llama-cpp" {
		if ver := readLlamaPrepareSourceVersion(entry); ver != "" {
			return ver
		}
	}
	switch familyID {
	case "mlx-lm", "vllm", "sglang":
		if ver := readVenvPackageVersion(entry, familyID); ver != "" {
			return ver
		}
	}
	if ver := installedVersionFromRegistryEntry(entry); ver != "" {
		return ver
	}
	if familyID == "llama-cpp" {
		binPath := resolveEngineExecutablePath(entry)
		if binPath != "" {
			if cached, ok := s.cachedBinaryVersion(binPath); ok {
				return cached
			}
			if allowProbe {
				return s.probeLlamaServerVersion(ctx, binPath)
			}
		}
	}
	return ""
}

func resolveEngineExecutablePath(entry registryEntry) string {
	if entry.installation != "" {
		if raw, err := installationBytes(entry.installation); err == nil {
			var decl struct {
				Executable string `json:"executable"`
			}
			if json.Unmarshal(raw, &decl) == nil && strings.TrimSpace(decl.Executable) != "" {
				return filepath.Clean(strings.TrimSpace(decl.Executable))
			}
		}
	}
	if strings.TrimSpace(entry.path) != "" {
		return filepath.Clean(strings.TrimSpace(entry.path))
	}
	return ""
}

func readLlamaPrepareSourceVersion(entry registryEntry) string {
	binPath := resolveEngineExecutablePath(entry)
	if binPath == "" {
		return ""
	}
	dir := filepath.Dir(binPath)
	for _, root := range uniqueCleanPaths([]string{dir, filepath.Dir(dir)}) {
		raw, err := os.ReadFile(filepath.Join(root, "engine-prepare.json"))
		if err != nil {
			continue
		}
		var doc struct {
			SourceURL string `json:"source_url"`
		}
		if json.Unmarshal(raw, &doc) != nil || strings.TrimSpace(doc.SourceURL) == "" {
			continue
		}
		if tag := extractTagFromReleaseURL(doc.SourceURL); tag != "" {
			return tag
		}
	}
	return ""
}

func extractTagFromReleaseURL(rawURL string) string {
	m := urlReleaseTagPattern.FindStringSubmatch(strings.TrimSpace(rawURL))
	if len(m) < 2 {
		return ""
	}
	tag := strings.TrimSpace(m[1])
	if _, ok := parseSemverVersion(tag); ok {
		return tag
	}
	if _, ok := parseBuildVersion(tag); ok {
		return tag
	}
	return ""
}

func readVenvPackageVersion(entry registryEntry, familyID string) string {
	venvRoot := venvRootFromEntry(entry)
	if venvRoot == "" {
		return ""
	}
	pkgNames := venvDistributionNames(familyID)
	if len(pkgNames) == 0 {
		return ""
	}
	siteDirs := venvSitePackagesDirs(venvRoot)
	for _, siteDir := range siteDirs {
		entries, err := os.ReadDir(siteDir)
		if err != nil {
			continue
		}
		for _, de := range entries {
			if !de.IsDir() || !strings.HasSuffix(strings.ToLower(de.Name()), ".dist-info") {
				continue
			}
			if !distInfoMatches(de.Name(), pkgNames) {
				continue
			}
			if ver := parseDistInfoMetadataVersion(filepath.Join(siteDir, de.Name(), "METADATA")); ver != "" {
				return ver
			}
		}
	}
	return ""
}

func venvDistributionNames(familyID string) []string {
	switch familyID {
	case "mlx-lm":
		return []string{"mlx_lm", "mlx-lm"}
	case "vllm":
		return []string{"vllm"}
	case "sglang":
		return []string{"sglang"}
	default:
		return nil
	}
}

func venvPackageInstallName(familyID string) string {
	switch familyID {
	case "mlx-lm":
		return "mlx-lm"
	case "vllm":
		return "vllm"
	case "sglang":
		return "sglang"
	default:
		return ""
	}
}

func distInfoMatches(dirName string, names []string) bool {
	lower := strings.ToLower(dirName)
	for _, name := range names {
		prefix := strings.ToLower(name) + "-"
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func parseDistInfoMetadataVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for i := 0; i < 64 && scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "version:") {
			return strings.TrimSpace(line[len("version:"):])
		}
	}
	return ""
}

func venvRootFromEntry(entry registryEntry) string {
	exe := resolveEngineExecutablePath(entry)
	if exe == "" {
		return ""
	}
	binDir := filepath.Dir(exe)
	base := strings.ToLower(filepath.Base(binDir))
	if base == "bin" || base == "scripts" {
		return filepath.Dir(binDir)
	}
	return ""
}

func venvPythonPath(entry registryEntry) string {
	venvRoot := venvRootFromEntry(entry)
	if venvRoot == "" {
		return ""
	}
	candidates := []string{
		filepath.Join(venvRoot, "bin", "python"),
		filepath.Join(venvRoot, "bin", "python3"),
	}
	if runtime.GOOS == "windows" {
		candidates = []string{
			filepath.Join(venvRoot, "Scripts", "python.exe"),
			filepath.Join(venvRoot, "python.exe"),
		}
	}
	for _, cand := range candidates {
		if info, err := os.Stat(cand); err == nil && !info.IsDir() {
			return cand
		}
	}
	exe := resolveEngineExecutablePath(entry)
	lower := strings.ToLower(filepath.Base(exe))
	if strings.HasPrefix(lower, "python") {
		return exe
	}
	return ""
}

func venvSitePackagesDirs(venvRoot string) []string {
	var dirs []string
	winSite := filepath.Join(venvRoot, "Lib", "site-packages")
	if info, err := os.Stat(winSite); err == nil && info.IsDir() {
		dirs = append(dirs, winSite)
	}
	matches, _ := filepath.Glob(filepath.Join(venvRoot, "lib", "python*", "site-packages"))
	for _, match := range matches {
		if info, err := os.Stat(match); err == nil && info.IsDir() {
			dirs = append(dirs, match)
		}
	}
	lib64Matches, _ := filepath.Glob(filepath.Join(venvRoot, "lib64", "python*", "site-packages"))
	for _, match := range lib64Matches {
		if info, err := os.Stat(match); err == nil && info.IsDir() {
			dirs = append(dirs, match)
		}
	}
	return uniqueCleanPaths(dirs)
}

func binaryFileFingerprint(binPath string) string {
	info, err := os.Stat(binPath)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

func (s *Service) cachedBinaryVersion(binPath string) (string, bool) {
	if s == nil || s.engineUpdater == nil {
		return "", false
	}
	fp := binaryFileFingerprint(binPath)
	if fp == "" {
		return "", false
	}
	s.engineUpdater.mu.Lock()
	defer s.engineUpdater.mu.Unlock()
	entry, ok := s.engineUpdater.binaryVersions[binPath]
	if !ok || entry.key != fp {
		return "", false
	}
	return entry.version, true
}

func (s *Service) probeLlamaServerVersion(ctx context.Context, binPath string) string {
	fp := binaryFileFingerprint(binPath)
	if fp == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binPath, "--version")
	raw, _ := cmd.CombinedOutput()
	ver := parseLlamaServerVersionOutput(string(raw))
	if s != nil && s.engineUpdater != nil {
		s.engineUpdater.mu.Lock()
		s.engineUpdater.binaryVersions[binPath] = binaryVersionCacheEntry{key: fp, version: ver}
		s.engineUpdater.mu.Unlock()
	}
	return ver
}

func parseLlamaServerVersionOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), "prism") {
			if _, ok := parseBuildVersion(trimmed); ok {
				for _, field := range strings.Fields(trimmed) {
					if _, ok2 := parseBuildVersion(field); ok2 {
						return strings.Trim(field, "(),;:")
					}
				}
			}
		}
		if m := llamaServerVersionLinePattern.FindStringSubmatch(trimmed); len(m) >= 2 {
			return "b" + m[1]
		}
		for _, field := range strings.Fields(trimmed) {
			clean := strings.Trim(field, "(),;:")
			if _, ok := parseSemverVersion(clean); ok {
				return clean
			}
			if _, ok := parseBuildVersion(clean); ok {
				return clean
			}
		}
	}
	return ""
}

func isHomebrewEnginePath(binPath string) bool {
	if strings.TrimSpace(binPath) == "" {
		return false
	}
	candidates := []string{filepath.Clean(binPath)}
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil && resolved != "" {
		candidates = append(candidates, filepath.Clean(resolved))
	}
	for _, cand := range candidates {
		slash := filepath.ToSlash(strings.ToLower(cand))
		if strings.HasPrefix(slash, "/opt/homebrew/") ||
			strings.Contains(slash, "/cellar/llama.cpp/") ||
			strings.Contains(slash, "/homebrew/cellar/") {
			return true
		}
	}
	return false
}

func detectLlamaDistribution(entry registryEntry, installedVersion, eligRelease string) string {
	for _, sample := range []string{
		installedVersion,
		eligRelease,
		entry.engine.Name,
		entry.displayName,
		entry.path,
		entry.receipt,
	} {
		if strings.Contains(strings.ToLower(sample), "prism") {
			return "PrismML"
		}
	}
	binPath := resolveEngineExecutablePath(entry)
	if binPath != "" {
		dir := filepath.Dir(binPath)
		for _, root := range uniqueCleanPaths([]string{dir, filepath.Dir(dir)}) {
			if raw, err := os.ReadFile(filepath.Join(root, "engine-prepare.json")); err == nil {
				if strings.Contains(strings.ToLower(string(raw)), "prismml") {
					return "PrismML"
				}
			}
		}
	}
	return "ggml-org"
}

func isCUDAEngineEntry(entry registryEntry) bool {
	if strings.Contains(strings.ToLower(entry.displayName), "cuda") ||
		strings.Contains(strings.ToLower(entry.engine.Name), "cuda") {
		return true
	}
	if entry.receipt != "" {
		if raw, err := os.ReadFile(entry.receipt); err == nil {
			var doc struct {
				Mode string `json:"mode"`
			}
			if json.Unmarshal(raw, &doc) == nil && strings.EqualFold(strings.TrimSpace(doc.Mode), "cuda") {
				return true
			}
		}
	}
	return false
}
