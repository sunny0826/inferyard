package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Registry exposes display names only. Paths stay in registryEntry and are
// resolved again at the command boundary.
type Registry struct {
	ModelError    *ApiError
	Refreshing    bool
	Models        []ModelAsset
	Engines       []EngineAsset
	DefaultEngine string
}
type ModelAsset struct {
	Name          string
	Format        string
	SizeBytes     *int64
	Architecture  string
	ContextLength *int64
	Available     bool
	Reason        string
}
type EngineAsset struct {
	Name           string
	Family         string
	Kind           string
	Source         string
	LatestDownload bool
}
type registryEntry struct {
	path, source         string
	installation, family string
	receipt              string
	model                ModelAsset
	engine               EngineAsset
	modified, downloaded time.Time
}
type registryData struct {
	modelError      *ApiError
	refreshing      bool
	models, engines []registryEntry
}

func scanEngineBinaries(roots ...string) []registryEntry {
	entries := []registryEntry{}
	seen := map[string]bool{}
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			if strings.Count(rel, string(filepath.Separator)) > 4 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			name := strings.ToLower(d.Name())
			if d.IsDir() || (name != "llama-server" && name != "llama-server.exe") {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0111 == 0) {
				return nil
			}
			identity, err := canonical(path)
			if err != nil || seen[identity] {
				return nil
			}
			seen[identity] = true
			entries = append(entries, registryEntry{path: path, source: root, receipt: engineReceipt(path), modified: info.ModTime()})
			return nil
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].modified.After(entries[j].modified) })
	return entries
}

// engineReceipt supports both Studio sibling and CLI ROOT/engine layouts.
func engineReceipt(enginePath string) string {
	dir := filepath.Dir(enginePath)
	for _, root := range []string{dir, filepath.Dir(dir)} {
		receipt := filepath.Join(root, "runtime-receipt.json")
		info, err := os.Stat(receipt)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if resolved, err := canonical(receipt); err == nil {
			return resolved
		}
	}
	return ""
}

func (s *Service) registry() registryData {
	s.mu.Lock()
	u, device := clone(s.settings), s.device
	device.Snapshot = append(json.RawMessage(nil), device.Snapshot...)
	device.LastError = clone(device.LastError)
	tasks := make([]DownloadTask, len(s.downloads))
	for i, d := range s.downloads {
		tasks[i] = clone(d.Task)
	}
	s.mu.Unlock()
	data := registryData{modelError: device.LastError, refreshing: device.Refreshing}
	models := map[string]int{}
	var snapshot struct {
		Models []struct {
			Path           string
			SizeBytes      *int64 `json:"size_bytes"`
			Architecture   string
			ContextLength  *int64 `json:"context_length"`
			Status, Reason string
		}
	}
	parseErr := json.Unmarshal(device.Snapshot, &snapshot)
	if len(device.Snapshot) > 0 && parseErr != nil {
		data.modelError = &ApiError{Code: "cli_error", Message: "设备快照无法读取，请到设备页刷新。"}
	}
	if parseErr == nil {
		for _, model := range snapshot.Models {
			path, err := canonical(model.Path)
			if err != nil || !filepath.IsAbs(path) {
				continue
			}
			if _, found := models[path]; found {
				continue
			}
			models[path] = len(data.models)
			data.models = append(data.models, registryEntry{path: path, source: "设备快照", model: ModelAsset{
				Name: filepath.Base(path), Format: "gguf", SizeBytes: model.SizeBytes, Architecture: model.Architecture,
				ContextLength: model.ContextLength, Available: model.Status == "available", Reason: model.Reason,
			}})
		}
	}
	if u.ModelsRoot != nil {
		local, failure := scanDirectoryModels(*u.ModelsRoot)
		if data.modelError == nil {
			data.modelError = failure
		}
		for _, entry := range local {
			if _, found := models[entry.path]; !found {
				models[entry.path] = len(data.models)
				data.models = append(data.models, entry)
			}
		}
	}
	rootsFor := EngineRoots
	if s.registryRoots != nil {
		rootsFor = s.registryRoots
	}
	roots := rootsFor(str(workspaceRoot(u)))
	data.engines = scanEngineBinaries(roots...)
	engines := map[string]int{}
	for i, e := range data.engines {
		identity, err := canonical(e.path)
		if err != nil {
			continue
		}
		data.engines[i].path = identity
		engines[identity] = i
		source := "用户引擎目录"
		if len(roots) > 0 && e.source == roots[0] && str(workspaceRoot(u)) != "" {
			source = "工作区引擎目录"
		}
		data.engines[i].source = source
	}
	for _, task := range tasks {
		if task.State != "done" {
			continue
		}
		if task.Kind == "engine" {
			path, err := canonical(str(task.EnginePath))
			if err != nil || !filepath.IsAbs(path) || str(task.EnginePath) == "" {
				continue
			}
			root := filepath.Join(str(workspaceRoot(u)), "engines")
			if s.paths != nil {
				root = s.paths.Engines
			}
			if _, err := Resolve(root, task.OutDir); err != nil {
				continue
			}
			if _, err := Resolve(task.OutDir, path); err != nil {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode()&0111 == 0) {
				continue
			}
			completed, _ := time.Parse(time.RFC3339Nano, str(task.FinishedAt))
			entry := registryEntry{path: path, source: "引擎下载 · " + filepath.Base(task.OutDir), receipt: engineReceipt(path), modified: info.ModTime(), downloaded: completed}
			if i, found := engines[path]; found {
				data.engines[i] = entry
			} else {
				engines[path] = len(data.engines)
				data.engines = append(data.engines, entry)
			}
		} else {
			if u.ModelsRoot == nil {
				continue
			}
			if _, err := Resolve(*u.ModelsRoot, task.OutDir); err != nil {
				continue
			}
			entries, err := os.ReadDir(task.OutDir)
			if err != nil {
				continue
			}
			for _, file := range entries {
				if !strings.HasSuffix(strings.ToLower(file.Name()), ".gguf") {
					continue
				}
				path, err := Resolve(task.OutDir, filepath.Join(task.OutDir, file.Name()))
				if err != nil {
					continue
				}
				if _, found := models[path]; found {
					continue
				}
				info, err := os.Stat(path)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				models[path] = len(data.models)
				data.models = append(data.models, registryEntry{path: path, source: "模型下载", model: ModelAsset{Name: filepath.Base(path), Format: "gguf", SizeBytes: ptr(info.Size()), Available: true}})
			}
		}
	}
	data.engines = append(data.engines, installationEntries(u.EngineInstallations)...)
	sort.SliceStable(data.engines, func(i, j int) bool {
		a, b := data.engines[i], data.engines[j]
		if !a.downloaded.Equal(b.downloaded) {
			return a.downloaded.After(b.downloaded)
		}
		return a.modified.After(b.modified)
	})
	nameEntries(data.models, false)
	nameEntries(data.engines, true)
	if len(data.engines) > 0 && !data.engines[0].downloaded.IsZero() {
		data.engines[0].engine.LatestDownload = true
	}
	return data
}

func nameEntries(entries []registryEntry, engine bool) {
	counts := map[string]int{}
	for i := range entries {
		e := &entries[i]
		name := filepath.Base(e.path)
		if engine {
			name = filepath.Base(filepath.Dir(e.path)) + " · " + name
			if e.installation != "" {
				name = e.family + " · " + filepath.Base(e.installation)
			}
		}
		counts[name]++
		if engine {
			kind, family := "legacy_binary", "llama-cpp"
			if e.installation != "" {
				kind, family = "local_installation", e.family
			}
			e.engine = EngineAsset{Name: name, Source: e.source, Kind: kind, Family: family}
		} else {
			e.model.Name = name
		}
	}
	labels := make([]string, len(entries))
	duplicates := map[string]int{}
	for i, e := range entries {
		name := e.model.Name
		if engine {
			name = e.engine.Name
		}
		if counts[name] > 1 {
			name += " · " + e.source
		}
		labels[i] = name
		duplicates[name]++
	}
	for i := range entries {
		name := labels[i]
		if duplicates[name] > 1 {
			identity := sha256.Sum256([]byte(entries[i].path))
			name += fmt.Sprintf(" · %x", identity[:6])
		}
		if engine {
			entries[i].engine.Name = name
		} else {
			entries[i].model.Name = name
		}
	}
}

func (s *Service) Registry() Registry {
	data := s.registry()
	result := Registry{Models: []ModelAsset{}, Engines: []EngineAsset{}, ModelError: data.modelError, Refreshing: data.refreshing}
	for _, e := range data.models {
		result.Models = append(result.Models, e.model)
	}
	for _, e := range data.engines {
		result.Engines = append(result.Engines, e.engine)
	}
	if len(result.Engines) > 0 {
		result.DefaultEngine = result.Engines[0].Name
	}
	return result
}

func (s *Service) resourcePath(name string, engine bool) (string, *ApiError) {
	data := s.registry()
	entries := data.models
	if engine {
		entries = data.engines
	}
	for _, entry := range entries {
		if engine && entry.engine.Name == name || !engine && entry.model.Name == name && entry.model.Available {
			return entry.path, nil
		}
	}
	return "", Err("not_found")
}
