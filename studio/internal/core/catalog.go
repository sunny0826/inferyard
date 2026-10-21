package core

import (
	"runtime"
	"strings"
)

// CatalogKind separates the two asset families the built-in address list
// covers: a downloadable engine archive and a downloadable model file.
type CatalogKind string

const (
	CatalogEngine CatalogKind = "engine"
	CatalogModel  CatalogKind = "model"
)

// CatalogEntry is one built-in address. The list is the single source of
// truth for the prefilled download URLs, so a pinned asset is described once
// and its downloadability is derived from the validators the real download
// path uses rather than from a hand-maintained flag.
type CatalogEntry struct {
	// ID is stable within the catalog; it is never reused for another asset.
	ID string
	// Kind is engine or model.
	Kind CatalogKind
	// Title is the display name shown in the built-in address section.
	Title string
	// Family groups engines by runtime family (llama-cpp, mlx-lm, vllm,
	// sglang) and names the runtime a reference entry prepares.
	Family string
	// URL is the direct download address.
	URL string
	// Bytes is the measured size of the asset.
	Bytes int64
	// SHA256 is the measured digest of the asset.
	SHA256 string
	// Revision pins the upstream revision or commit the asset came from.
	Revision string
	// Platform names the platform the asset targets, empty when the asset
	// is platform independent.
	Platform string
	// Reason explains why the entry is reference-only, empty when the entry
	// is directly downloadable.
	Reason string
	// Doc points at the preparation document for reference-only entries.
	Doc string
}

// Downloadable reports whether the existing download path can fetch this
// entry. Engines must pass the engine source validator, stay inside the
// 64 MiB fetch limit and belong to the llama.cpp archive family; models must
// pass the model source validator. The answer is derived, so a new entry
// cannot claim to be downloadable without the transfer path accepting it.
func (e CatalogEntry) Downloadable() bool {
	if e.Reason != "" {
		return false
	}
	switch e.Kind {
	case CatalogEngine:
		return e.Bytes <= maxFetchBytes && isLlamaArchive(e.URL) && validEngineSource(e.URL)
	case CatalogModel:
		return validSource(e.URL)
	}
	return false
}

// isLlamaArchive reports whether the URL points at a llama.cpp release
// archive, the only engine shape the Studio unpack path understands.
func isLlamaArchive(source string) bool {
	lower := strings.ToLower(source)
	return strings.Contains(lower, "/llama.cpp/releases/") &&
		(strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".zip"))
}

// catalogEngineBase is the pinned Prism llama.cpp release the CLI contracts
// reference (see cli/docs/contracts/windows-prism-batch-contract.md).
const catalogEngineBase = "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/"

// Catalog returns the built-in address list.
func Catalog() []CatalogEntry {
	out := make([]CatalogEntry, len(catalogEntries))
	copy(out, catalogEntries)
	return out
}

// CatalogEntryByID returns the entry with the given id.
func CatalogEntryByID(id string) (CatalogEntry, bool) {
	for _, entry := range catalogEntries {
		if entry.ID == id {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

// CatalogEntryByURL returns the entry with the given URL.
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

// CatalogEngineForPlatform returns the downloadable engine archive for the
// running platform, or an empty id when the platform has no known build.
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

// CatalogEngineForCurrentPlatform returns the downloadable engine archive
// for the running platform, or an empty id when the platform has no known
// build.
func CatalogEngineForCurrentPlatform() string {
	return CatalogEngineForPlatform(runtime.GOOS, runtime.GOARCH)
}

// CurrentPlatform is the platform key of the running binary, in the same
// "os/arch" shape the catalog uses.
func CurrentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// PlatformLabel renders a platform key for display. The key is "os/arch", or
// "any" for assets that do not depend on the platform.
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

// DefaultModelURL returns the built-in tiny test model (SmolLM2-135M Q4_K_M,
// about 100 MiB) so first-run users can pull a working model without hunting
// for a URL. The revision is pinned; bump deliberately with releases.
func DefaultModelURL() string {
	for _, entry := range catalogEntries {
		if entry.ID == "model-smollm2-135m" {
			return entry.URL
		}
	}
	return ""
}

// DefaultEngineURL returns the pinned Prism engine archive for the current
// platform, or "" when no CPU build is known.
func DefaultEngineURL() string {
	id := CatalogEngineForCurrentPlatform()
	if id == "" {
		return ""
	}
	entry, _ := CatalogEntryByID(id)
	return entry.URL
}
