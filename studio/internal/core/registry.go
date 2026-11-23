package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Registry struct {
	ModelError      *ApiError
	Refreshing      bool
	Models          []ModelAsset
	Engines         []EngineAsset
	DefaultEngine   string
	TotalModelBytes int64
	DiskFreeBytes   *int64
	ModelRoots      []string
}
type ModelAsset struct {
	Name          string
	Format        string
	SizeBytes     *int64
	Architecture  string
	ContextLength *int64
	Available     bool
	Reason        string
	Path          string
	Deletable     bool
}
type EngineAsset struct {
	Name           string
	Family         string
	Kind           string
	Source         string
	LatestDownload bool
	Path           string
	Deletable      bool
}
type registryEntry struct {
	path, rawPath, source string
	installation, family  string
	receipt, displayName  string
	assetFiles            []assetFileSize
	model                 ModelAsset
	engine                EngineAsset
	modified, downloaded  time.Time
}
type registryData struct {
	modelError      *ApiError
	refreshing      bool
	models, engines []registryEntry
	modelRoots      []string
	resultsRoot     *string
	wsEngines       string
	totalModelBytes int64
	diskFreeBytes   *int64
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
			entries = append(entries, registryEntry{path: path, source: root, receipt: engineReceipt(path), displayName: engineReadableName(path, "", ""), modified: info.ModTime()})
			return nil
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].modified.After(entries[j].modified) })
	return entries
}

func engineReadableName(enginePath, folder, sourceURL string) string {
	dir := filepath.Dir(enginePath)
	var mode, release, prepareOut, prepareURL string
	for _, root := range []string{dir, filepath.Dir(dir)} {
		if raw, err := os.ReadFile(filepath.Join(root, "runtime-receipt.json")); err == nil {
			var doc struct {
				Mode          string `json:"mode"`
				EngineRelease string `json:"engine_release"`
			}
			if json.Unmarshal(raw, &doc) == nil && (doc.Mode != "" || doc.EngineRelease != "") {
				mode, release = doc.Mode, doc.EngineRelease
				if folder == "" {
					folder = filepath.Base(root)
				}
			}
		}
		if raw, err := os.ReadFile(filepath.Join(root, "engine-prepare.json")); err == nil {
			var doc struct {
				SourceURL string `json:"source_url"`
				Out       string `json:"out"`
			}
			if json.Unmarshal(raw, &doc) == nil && (doc.SourceURL != "" || doc.Out != "") {
				prepareURL, prepareOut = doc.SourceURL, doc.Out
				if folder == "" {
					if prepareOut != "" {
						folder = filepath.Base(prepareOut)
					} else {
						folder = filepath.Base(root)
					}
				}
			}
		}
	}
	if sourceURL == "" {
		sourceURL = prepareURL
	}
	if folder == "" && sourceURL == "" {
		return ""
	}
	entry, inCatalog := CatalogEntryByURL(sourceURL)
	prefix := folder
	if prefix == "" || prefix == "engine" {
		switch {
		case release != "":
			prefix = release
		case inCatalog && entry.Revision != "":
			prefix = entry.Revision
		case sourceURL != "":
			prefix = SuggestDownloadSlug("engine", sourceURL)
		}
	}
	if prefix == "" {
		return ""
	}
	bin := filepath.Base(enginePath)
	if strings.EqualFold(mode, "cuda") || (inCatalog && entry.RuntimeURL != "") {
		return prefix + " · CUDA · " + bin
	}
	return prefix + " · " + bin
}

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
	u, device, assets := clone(s.settings), s.device, cloneAssetsState(s.assets)
	device.Snapshot = append(json.RawMessage(nil), device.Snapshot...)
	device.LastError = clone(device.LastError)
	tasks := make([]DownloadTask, len(s.downloads))
	for i, d := range s.downloads {
		tasks[i] = clone(d.Task)
	}
	s.mu.Unlock()
	data := registryData{
		refreshing:  assets.AssetsRefreshing || device.Refreshing,
		modelRoots:  activeModelRoots(u),
		resultsRoot: u.ResultsRoot,
		wsEngines:   workspaceEnginesRoot(u, s.paths),
	}
	models := map[string]int{}
	var scanned *inferyard.AssetScan
	if assets.Parsed != nil {
		scanned = assets.Parsed
	} else if len(assets.Snapshot) > 0 {
		if parsed, err := inferyard.ParseAssetScan(assets.Snapshot); err == nil {
			scanned = &parsed
		} else {
			data.modelError = &ApiError{Code: "cli_error", Message: "模型资产快照无法读取，请点击刷新重试。"}
		}
	}
	if scanned != nil {
		data.modelError = assets.AssetsError
		for _, model := range scanned.Models {
			path, err := canonical(model.Path)
			if err != nil || !filepath.IsAbs(path) {
				continue
			}
			if _, found := models[path]; found {
				continue
			}
			source := "本地模型"
			if model.Format == "directory" {
				source = "模型目录"
			}
			models[path] = len(data.models)
			data.models = append(data.models, registryEntry{
				path:    path,
				rawPath: model.Path,
				source:  source,
				model: ModelAsset{
					Name:          filepath.Base(path),
					Format:        model.Format,
					SizeBytes:     model.SizeBytes,
					Architecture:  str(model.Architecture),
					ContextLength: model.ContextLength,
					Available:     model.Available,
					Reason:        str(model.Reason),
				},
			})
		}
	} else {
		data.modelError = device.LastError
		if data.modelError == nil && assets.AssetsError != nil && assets.AssetsError.Code != "cli_outdated" {
			data.modelError = assets.AssetsError
		}
		var snapshot struct {
			Models []struct {
				Path                  string
				SizeBytes             *int64 `json:"size_bytes"`
				Architecture          string
				ContextLength         *int64 `json:"context_length"`
				Status, Reason        string
				ChatTemplateAvailable *bool `json:"chat_template_available"`
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
				available := model.Status == "available"
				reason := model.Reason
				if available && model.ChatTemplateAvailable != nil && !*model.ChatTemplateAvailable {
					available = false
					reason = "chat_template_unavailable"
				}
				models[path] = len(data.models)
				data.models = append(data.models, registryEntry{path: path, rawPath: model.Path, source: "设备快照", model: ModelAsset{
					Name: filepath.Base(path), Format: "gguf", SizeBytes: model.SizeBytes, Architecture: model.Architecture,
					ContextLength: model.ContextLength, Available: available, Reason: reason,
				}})
			}
		}
		for i, root := range data.modelRoots {
			local, failure := scanDirectoryModels(root)
			if i == 0 && data.modelError == nil {
				data.modelError = failure
			}
			for _, entry := range local {
				if _, found := models[entry.path]; !found {
					models[entry.path] = len(data.models)
					data.models = append(data.models, entry)
				}
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
			entry := registryEntry{path: path, rawPath: str(task.EnginePath), source: "引擎下载 · " + filepath.Base(task.OutDir), receipt: engineReceipt(path), displayName: engineReadableName(path, filepath.Base(task.OutDir), task.SourceUrl), modified: info.ModTime(), downloaded: completed}
			if i, found := engines[path]; found {
				data.engines[i] = entry
			} else {
				engines[path] = len(data.engines)
				data.engines = append(data.engines, entry)
			}
		} else if task.Kind == "model" {
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
	psModels, psScope := scanPolystrataModelConfigs(u.EngineInstallations)
	for _, entry := range psModels {
		if _, found := models[entry.path]; !found {
			models[entry.path] = len(data.models)
			data.models = append(data.models, entry)
		}
	}
	data.models = deduplicatePolystrataModels(data.models, psScope)
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
	data.totalModelBytes, data.diskFreeBytes = registryStorageMetrics(u, data.modelRoots, data.models, scanned)
	return data
}

func nameEntries(entries []registryEntry, engine bool) {
	counts := map[string]int{}
	for i := range entries {
		e := &entries[i]
		name := filepath.Base(e.path)
		if e.displayName != "" {
			name = e.displayName
		}
		if engine {
			name = filepath.Base(filepath.Dir(e.path)) + " · " + filepath.Base(e.path)
			if e.installation != "" {
				name = e.family + " · " + filepath.Base(e.installation)
			}
			if e.displayName != "" {
				name = e.displayName
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
	result := Registry{
		Models:          []ModelAsset{},
		Engines:         []EngineAsset{},
		ModelError:      data.modelError,
		Refreshing:      data.refreshing,
		TotalModelBytes: data.totalModelBytes,
		DiskFreeBytes:   data.diskFreeBytes,
		ModelRoots:      data.modelRoots,
	}
	for _, e := range data.models {
		m := e.model
		m.Path = e.path
		m.Deletable = m.Format != "strata_pack" && isModelDeletable(e.rawPath, e.path, data.modelRoots, data.resultsRoot)
		result.Models = append(result.Models, m)
	}
	for _, e := range data.engines {
		eng := e.engine
		eng.Path = e.path
		eng.Deletable = isEngineDeletable(e.rawPath, e.path, eng.Kind, data.wsEngines, data.resultsRoot)
		result.Engines = append(result.Engines, eng)
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
