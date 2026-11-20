package core

import (
	"runtime"
	"strings"
)

type CatalogKind string

const (
	CatalogEngine CatalogKind = "engine"
	CatalogModel  CatalogKind = "model"
)

type CatalogEntry struct {
	ID       string
	Kind     CatalogKind
	Title    string
	Family   string
	URL      string
	Bytes    int64
	SHA256   string
	Revision string
	Platform string
	Reason   string
	Doc      string
	Note     string
}

func (e CatalogEntry) Downloadable() bool {
	if e.Reason != "" {
		return false
	}
	switch e.Kind {
	case CatalogEngine:
		return isLlamaArchive(e.URL) && validEngineSource(e.URL)
	case CatalogModel:
		return validSource(e.URL)
	}
	return false
}

func isLlamaArchive(source string) bool {
	lower := strings.ToLower(source)
	return strings.Contains(lower, "/llama.cpp/releases/") && isEngineArchive(lower)
}

const catalogEngineBase = "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/"

func (e CatalogEntry) RunsOnCurrentPlatform() bool {
	return e.Platform == "any" || e.Platform == CurrentPlatform()
}

func CatalogForPlatform(platform string) []CatalogEntry {
	var entries []CatalogEntry
	for _, entry := range catalogEntries {
		if entry.Platform == "any" || entry.Platform == platform {
			entries = append(entries, entry)
		}
	}
	return entries
}

func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalogEntries))
	copy(out, catalogEntries)
	return out
}

func CatalogEntryByID(id string) (CatalogEntry, bool) {
	for _, entry := range catalogEntries {
		if entry.ID == id {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func CatalogEntryByURL(url string) (CatalogEntry, bool) {
	if url == "" {
		return CatalogEntry{}, false
	}
	for _, entry := range catalogEntries {
		if entry.URL == url {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

func CatalogEngineForPlatform(goos, goarch string) string {
	wanted := goos + "/" + goarch
	for _, entry := range catalogEntries {
		if entry.Kind != CatalogEngine || !entry.Downloadable() {
			continue
		}
		if entry.Platform == wanted {
			return entry.ID
		}
	}
	return ""
}

func CatalogEngineForCurrentPlatform() string {
	return CatalogEngineForPlatform(runtime.GOOS, runtime.GOARCH)
}

func CurrentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func PlatformLabel(platform string) string {
	if platform == "any" {
		return "任意平台"
	}
	parts := strings.SplitN(platform, "/", 2)
	if len(parts) != 2 {
		return platform
	}
	labels := map[string]string{
		"darwin":  "macOS",
		"linux":   "Linux",
		"windows": "Windows",
	}
	name, ok := labels[parts[0]]
	if !ok {
		name = parts[0]
	}
	arch := parts[1]
	if arch == "amd64" {
		arch = "x64"
	}
	return name + " " + arch
}

func (e CatalogEntry) DisplayName() string {
	if e.SharesTitle() {
		return e.Title + " · " + PlatformLabel(e.Platform)
	}
	return e.Title
}

func (e CatalogEntry) SharesTitle() bool {
	count := 0
	for _, other := range catalogEntries {
		if other.Kind == e.Kind && other.Title == e.Title {
			count++
		}
	}
	return count > 1
}

func DefaultModelURL() string {
	for _, entry := range catalogEntries {
		if entry.ID == "model-smollm2-135m" {
			return entry.URL
		}
	}
	return ""
}

func DefaultEngineURL() string {
	id := CatalogEngineForCurrentPlatform()
	if id == "" {
		return ""
	}
	entry, _ := CatalogEntryByID(id)
	return entry.URL
}
