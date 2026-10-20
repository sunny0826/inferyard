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

// Discovery only offers directory candidates. CLI freezes and validates their
// complete assets when preparing a candidate; unknown measurements stay nil.
func scanDirectoryModels(root string) ([]registryEntry, *ApiError) {
	root, err := canonical(root)
	if err != nil || !filepath.IsAbs(root) {
		return nil, Err("model_discovery_unreadable")
	}
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
		config, tokenizer, weights, safe := false, false, false, true
		for _, entry := range entries {
			path := filepath.Join(next.path, entry.Name())
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
				if entry.Type()&os.ModeSymlink != 0 {
					safe = false
				}
				continue
			}
			name := strings.ToLower(entry.Name())
			config = config || name == "config.json"
			tokenizer = tokenizer || name == "tokenizer.json" || name == "tokenizer.model" || name == "tokenizer_config.json"
			weights = weights || strings.HasSuffix(name, ".safetensors") || strings.HasPrefix(name, "pytorch_model") && strings.HasSuffix(name, ".bin")
		}
		if config && tokenizer && weights && safe {
			result = append(result, registryEntry{path: next.path, source: "模型目录", model: ModelAsset{Format: "directory", Available: true}})
		}
	}
	return result, failure
}

func skipModelDirectory(name string) bool {
	return strings.HasPrefix(name, ".") || name == "venv" || name == "env" || name == "node_modules" || name == "site-packages" || name == "__pycache__"
}
