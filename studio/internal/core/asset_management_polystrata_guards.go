package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

var polystrataSecretEnvSubstrings = []string{"API_KEY", "TOKEN", "PASSWORD", "SECRET", "AUTH"}

type polystrataCfgContext struct {
	cfg      string
	workDir  string
	dataDir  string
	checkExe bool
}

func seenCfgInContext(list []polystrataCfgContext, cfg, workDir, dataDir string, checkExe bool) bool {
	nw := filepath.Clean(strings.TrimSpace(workDir))
	nd := filepath.Clean(strings.TrimSpace(dataDir))
	for _, item := range list {
		if item.workDir == nw && item.dataDir == nd && item.checkExe == checkExe && sameDirectoryEntry(item.cfg, cfg) {
			return true
		}
	}
	return false
}

type polystrataCredentialError struct {
	configName string
	field      string
}

func (e *polystrataCredentialError) Error() string {
	return fmt.Sprintf("plaintext credential in %s (%s)", e.configName, e.field)
}

func polystrataCredentialApiError(err error) (*ApiError, bool) {
	var credErr *polystrataCredentialError
	if !errors.As(err, &credErr) {
		return nil, false
	}
	apiErr := errAssetNotDeletable()
	apiErr.Message = fmt.Sprintf("配置「%s」的 %s 字段包含明文凭据，无法安全迁移并保留到模型目录，请改用环境变量引用后再卸载。", credErr.configName, credErr.field)
	return apiErr, true
}

func findPolystrataPlaintextCredentialField(rawMap map[string]any, args []string) (string, bool) {
	if apiKey, exists := rawMap["api_key"]; exists {
		if s, ok := apiKey.(string); ok {
			if strings.TrimSpace(s) != "" {
				return "api_key", true
			}
		} else if apiKey != nil {
			return "api_key", true
		}
	}
	for _, item := range args {
		flag := strings.ToLower(strings.TrimSpace(strings.SplitN(item, "=", 2)[0]))
		switch flag {
		case "--api-key", "--api-key-file", "--token", "--hf-token":
			return "args", true
		}
	}
	hasSecretKey := func(m map[string]any) (string, bool) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := m[k]
			upper := strings.ToUpper(k)
			matched := slices.ContainsFunc(polystrataSecretEnvSubstrings, func(sub string) bool {
				return strings.Contains(upper, sub)
			})
			if matched {
				if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
					continue
				}
				return k, true
			}
		}
		return "", false
	}
	if envMap, ok := rawMap["env"].(map[string]any); ok {
		if key, found := hasSecretKey(envMap); found {
			return "env." + key, true
		}
	}
	for _, mcpKey := range []string{"mcp_servers", "mcpServers"} {
		mcpMap, ok := rawMap[mcpKey].(map[string]any)
		if !ok {
			continue
		}
		srvKeys := make([]string, 0, len(mcpMap))
		for srv := range mcpMap {
			srvKeys = append(srvKeys, srv)
		}
		sort.Strings(srvKeys)
		for _, srv := range srvKeys {
			entry, ok := mcpMap[srv].(map[string]any)
			if !ok {
				continue
			}
			for _, subKey := range []string{"headers", "env"} {
				if subMap, ok := entry[subKey].(map[string]any); ok {
					if key, found := hasSecretKey(subMap); found {
						return mcpKey + "." + subKey + "." + key, true
					}
				}
			}
		}
	}
	return "", false
}

func rawPathTraversesDir(rawRef string, bases []string, targetDir string) bool {
	trimmed := strings.TrimSpace(rawRef)
	if trimmed == "" || !slices.Contains(strings.Split(filepath.ToSlash(trimmed), "/"), "..") {
		return false
	}
	var candidates []string
	if filepath.IsAbs(trimmed) {
		candidates = []string{trimmed}
	} else {
		for _, base := range bases {
			if strings.TrimSpace(base) != "" {
				candidates = append(candidates, strings.TrimRight(base, "/\\")+"/"+trimmed)
			}
		}
	}
	for _, cand := range candidates {
		slash := filepath.ToSlash(cand)
		hasLeadingSlash := strings.HasPrefix(slash, "/")
		var stack []string
		for _, seg := range strings.Split(slash, "/") {
			if seg == "" || seg == "." {
				continue
			}
			if seg == ".." {
				cur := strings.Join(stack, "/")
				if hasLeadingSlash {
					cur = "/" + cur
				}
				if cur != "" {
					curNative := filepath.FromSlash(cur)
					if pathWithinDirSameFS(targetDir, curNative) {
						return true
					}
					if c, cerr := canonical(curNative); cerr == nil && pathWithinDirSameFS(targetDir, c) {
						return true
					}
				}
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			stack = append(stack, seg)
		}
	}
	return false
}

func exeDependsOnInstallDir(exeRaw, cwdRaw, declWorkDir, cfgDir, installDir string) bool {
	trimmedExe := strings.TrimSpace(exeRaw)
	if trimmedExe == "" {
		return false
	}
	outBase := strings.TrimSpace(declWorkDir)
	if outBase == "" {
		outBase = cfgDir
	}
	baseExpr := outBase
	if trimmedCwd := strings.TrimSpace(cwdRaw); trimmedCwd != "" {
		if filepath.IsAbs(trimmedCwd) {
			baseExpr = trimmedCwd
		} else {
			baseExpr = strings.TrimRight(outBase, "/\\") + "/" + trimmedCwd
		}
	}
	exeCand := trimmedExe
	if !filepath.IsAbs(exeCand) {
		exeCand = strings.TrimRight(baseExpr, "/\\") + "/" + exeCand
	}
	if rawPathTraversesDir(exeCand, nil, installDir) {
		return true
	}
	cleanExe := filepath.Clean(filepath.FromSlash(exeCand))
	if pathWithinDirSameFS(installDir, cleanExe) {
		return true
	}
	if canonExe, err := canonical(cleanExe); err == nil && pathWithinDirSameFS(installDir, canonExe) {
		return true
	}
	return false
}

func winningRefBase(ref string, bases []string) []string {
	if filepath.IsAbs(strings.TrimSpace(ref)) {
		return nil
	}
	for _, b := range bases {
		if _, _, ok := resolvePolystrataAssetRef(ref, []string{b}); ok {
			return []string{b}
		}
	}
	for _, b := range bases {
		if len(collectBrokenSplitGGUFShards(ref, []string{b})) > 0 {
			return []string{b}
		}
	}
	return bases
}

func survivingPolystrataConfigDependsOnInstallDir(cfgPath, declWorkDir, declDataDir, installDir string, checkExe bool) bool {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return false
	}
	var cfg struct {
		Exe  string   `json:"exe"`
		Args []string `json:"args"`
		Cwd  string   `json:"cwd"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return false
	}
	var rawMap map[string]any
	_ = json.Unmarshal(raw, &rawMap)

	cfgDir := filepath.Dir(cfgPath)
	if checkExe && exeDependsOnInstallDir(cfg.Exe, cfg.Cwd, declWorkDir, cfgDir, installDir) {
		return true
	}

	origBases := polystrataConfigBases(cfg.Cwd, declWorkDir, cfgDir, declDataDir)
	refs := extractPolystrataAssetRefs(cfg.Args, rawMap)
	cwdBase := strings.TrimSpace(declWorkDir)
	if cwdBase == "" {
		cwdBase = cfgDir
	}
	if rawPathTraversesDir(cfg.Cwd, []string{cwdBase}, installDir) {
		return true
	}
	for _, ref := range refs {
		winBase := winningRefBase(ref, origBases)
		if rawPathTraversesDir(ref, winBase, installDir) {
			return true
		}
	}
	files, dirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, origBases)
	for _, f := range files {
		if pathWithinDirSameFS(installDir, f.path) {
			return true
		}
	}
	for _, d := range dirs {
		if sameFileOrPath(d, installDir) || pathWithinDirSameFS(installDir, d) {
			return true
		}
	}

	pointsInsideInstall := func(rawDir string) bool {
		trimmed := strings.TrimSpace(rawDir)
		if trimmed == "" {
			return false
		}
		if sameFileOrPath(trimmed, installDir) || pathWithinDirSameFS(installDir, trimmed) {
			return true
		}
		canonDir, cerr := canonical(trimmed)
		return cerr == nil && (sameFileOrPath(canonDir, installDir) || pathWithinDirSameFS(installDir, canonDir))
	}
	postWorkDir := declWorkDir
	if pointsInsideInstall(postWorkDir) {
		postWorkDir = ""
	}
	postDataDir := declDataDir
	if pointsInsideInstall(postDataDir) {
		postDataDir = ""
	}
	rawPostBases := polystrataConfigBases(cfg.Cwd, postWorkDir, cfgDir, postDataDir)
	postBases := make([]string, 0, len(rawPostBases))
	for _, b := range rawPostBases {
		if !pointsInsideInstall(b) {
			postBases = append(postBases, b)
		}
	}

	origValidFiles, origValidDirs, origOK := validatePolystrataAssets(refs, cfgDir, origBases)
	if origOK {
		postFiles, postDirs, postOK := validatePolystrataAssets(refs, cfgDir, postBases)
		if !postOK || !sameAssetFileSets(origValidFiles, postFiles) || !samePathSets(origValidDirs, postDirs) {
			return true
		}
	}
	postLenFiles, postLenDirs := resolvePolystrataConfigRefsWithBases(cfg.Args, rawMap, cfgDir, postBases)
	return !sameAssetFileSets(files, postLenFiles) || !samePathSets(dirs, postLenDirs)
}
