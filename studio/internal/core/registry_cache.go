package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type cachedEngineScan struct {
	signature string
	entries   []registryEntry
}

func cloneRegistryEntries(in []registryEntry) []registryEntry {
	if len(in) == 0 {
		return []registryEntry{}
	}
	out := make([]registryEntry, len(in))
	copy(out, in)
	return out
}

func engineScanSignature(roots ...string) (string, bool) {
	h := sha256.New()
	walkOK := true
	for _, root := range roots {
		if root == "" {
			continue
		}
		appendFileSig(h, "root:"+root, root)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				walkOK = false
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			if strings.Count(rel, string(filepath.Separator)) > 4 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if info, infoErr := d.Info(); infoErr == nil {
					_, _ = fmt.Fprintf(h, "d|%s|%d\n", path, info.ModTime().UnixNano())
				}
				return nil
			}
			lower := strings.ToLower(d.Name())
			if lower == "llama-server" || lower == "llama-server.exe" || lower == "runtime-receipt.json" || lower == "engine-prepare.json" {
				if info, infoErr := d.Info(); infoErr == nil {
					_, _ = fmt.Fprintf(h, "f|%s|%d|%d|%d\n", path, info.Mode(), info.Size(), info.ModTime().UnixNano())
				}
			}
			return nil
		})
		if err != nil {
			walkOK = false
		}
	}
	if !walkOK {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}

func (s *Service) scanEngineBinariesCached(roots ...string) []registryEntry {
	sig, ok := engineScanSignature(roots...)
	if ok {
		s.caches.mu.Lock()
		if s.caches.engineScan != nil && s.caches.engineScan.signature == sig {
			copied := cloneRegistryEntries(s.caches.engineScan.entries)
			s.caches.mu.Unlock()
			return copied
		}
		s.caches.mu.Unlock()
	}
	entries := scanEngineBinaries(roots...)
	if ok {
		s.caches.mu.Lock()
		s.caches.engineScan = &cachedEngineScan{
			signature: sig,
			entries:   cloneRegistryEntries(entries),
		}
		s.caches.mu.Unlock()
	}
	return entries
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
			entries = append(entries, registryEntry{
				path:        identity,
				source:      root,
				receipt:     engineReceipt(identity),
				displayName: engineReadableName(identity, "", ""),
				modified:    info.ModTime(),
			})
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

// RegistryAndEvidence computes both Registry and ModelBenchmarkEvidence from a
// single shared registryData scan, avoiding duplicate directory walks during refreshes.
func (s *Service) RegistryAndEvidence(summaries []ReportSummary, history RunHistoryResponse) (Registry, map[string]ModelEvidenceSummary) {
	data := s.registry()
	return s.buildRegistryFromData(data), s.modelBenchmarkEvidenceFromData(summaries, history, data)
}
