package core

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const modelDiscoveryEntries = 4096
const modelDiscoveryDepth = 4

func scanDirectoryModels(root string) ([]registryEntry, *ApiError) {
	root, err := canonical(root)
	if err != nil || !filepath.IsAbs(root) {
		return nil, Err("model_discovery_unreadable")
	}
	hubRoots := hfHubCacheRoots()
	type directory struct {
		path  string
		depth int
	}
	pending := []directory{{root, 0}}
	remaining := modelDiscoveryEntries
	result := []registryEntry{}
	var failure *ApiError
	for len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		f, err := os.Open(next.path)
		if err != nil {
			if !os.IsNotExist(err) {
				failure = Err("model_discovery_unreadable")
			}
			continue
		}
		entries, err := f.ReadDir(remaining + 1)
		f.Close()
		if err != nil && err != io.EOF {
			failure = Err("model_discovery_unreadable")
			continue
		}
		if len(entries) > remaining {
			return result, Err("model_discovery_limit")
		}
		remaining -= len(entries)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		allowed := allowedModelAncestors(root, next.path, hubRoots)
		config, tokenizer, weights, safe := false, false, false, true
		for _, entry := range entries {
			path := filepath.Join(next.path, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				resolved, ok := resolveAllowedModelSymlink(path, allowed)
				if !ok {
					safe = false
					continue
				}
				_ = resolved
				config, tokenizer, weights = markModelEntry(entry.Name(), config, tokenizer, weights)
				continue
			}
			resolved, err := Resolve(root, path)
			if err != nil {
				safe = false
				continue
			}
			if entry.IsDir() {
				if next.depth < modelDiscoveryDepth && !skipModelDirectory(entry.Name()) {
					pending = append(pending, directory{resolved, next.depth + 1})
				}
				continue
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			config, tokenizer, weights = markModelEntry(entry.Name(), config, tokenizer, weights)
		}
		if config && tokenizer && weights && safe {
			result = append(result, registryEntry{path: next.path, source: "模型目录", model: ModelAsset{Format: "directory", Available: true}})
		}
	}
	return result, failure
}

func markModelEntry(entryName string, config, tokenizer, weights bool) (bool, bool, bool) {
	name := strings.ToLower(entryName)
	config = config || name == "config.json"
	tokenizer = tokenizer || name == "tokenizer.json" || name == "tokenizer.model" || name == "tokenizer_config.json"
	weights = weights || strings.HasSuffix(name, ".safetensors") || strings.HasPrefix(name, "pytorch_model") && strings.HasSuffix(name, ".bin")
	return config, tokenizer, weights
}

func resolveAllowedModelSymlink(path string, allowed []string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	for _, ancestor := range allowed {
		if withinCanonicalDir(ancestor, resolved) {
			return resolved, true
		}
	}
	return "", false
}

func withinCanonicalDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hfHubCacheRoots() []string {
	var candidates []string
	if hub := os.Getenv("HF_HUB_CACHE"); hub != "" {
		candidates = append(candidates, hub)
	}
	if hfHome := os.Getenv("HF_HOME"); hfHome != "" {
		candidates = append(candidates, filepath.Join(hfHome, "hub"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".cache", "huggingface", "hub"))
	}
	seen := map[string]bool{}
	roots := make([]string, 0, len(candidates))
	for _, c := range candidates {
		resolved, err := filepath.EvalSymlinks(c)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() || seen[resolved] {
			continue
		}
		seen[resolved] = true
		roots = append(roots, resolved)
	}
	return roots
}

func allowedModelAncestors(root, dir string, hubRoots []string) []string {
	allowed := []string{root, dir}
	allowed = append(allowed, hubRoots...)
	for cur := dir; ; {
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		base := filepath.Base(cur)
		if strings.HasPrefix(base, "models--") {
			snapshots := filepath.Join(cur, "snapshots")
			if withinCanonicalDir(snapshots, dir) {
				blobs := filepath.Join(cur, "blobs")
				if resolved, err := filepath.EvalSymlinks(blobs); err == nil {
					if info, err := os.Stat(resolved); err == nil && info.IsDir() {
						allowed = append(allowed, resolved)
					}
				}
				allowed = append(allowed, cur)
				break
			}
		}
		cur = parent
	}
	return allowed
}

func skipModelDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || name == "venv" || name == "env" || name == "node_modules" || name == "site-packages" || name == "__pycache__"
}
