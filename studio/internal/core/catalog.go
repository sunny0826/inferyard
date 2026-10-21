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
	// Note says what still has to happen after a downloadable entry is
	// fetched, empty when fetching it is enough.
	Note string
}

// Downloadable reports whether the existing download path can fetch this
// entry. The engine channel streams the archive to disk and unpacks a
// llama.cpp archive from it, so a large pinned archive is downloadable while
// an archive of another shape is not, because unpacking it would not yield an
// engine. The model channel accepts a single GGUF at a fixed revision. The
// answer is derived, so a new entry cannot claim to be downloadable without
// the transfer path accepting it.
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

// isLlamaArchive reports whether the URL points at a llama.cpp release
// archive, the only engine shape the Studio unpack path understands.
func isLlamaArchive(source string) bool {
	lower := strings.ToLower(source)
	return strings.Contains(lower, "/llama.cpp/releases/") && isEngineArchive(lower)
}

// catalogEngineBase is the pinned Prism llama.cpp release the CLI contracts
// reference (see cli/docs/contracts/windows-prism-batch-contract.md).
const catalogEngineBase = "https://github.com/PrismML-Eng/llama.cpp/releases/download/prism-b10743-adfffbe/"

// RunsOnCurrentPlatform reports whether the asset can run on the machine
// Studio is running on. An asset for another platform can still be fetched —
// the operator may be preparing it for another machine — but it cannot be
// started here.
func (e CatalogEntry) RunsOnCurrentPlatform() bool {
	return e.Platform == "any" || e.Platform == CurrentPlatform()
}

// CatalogForPlatform excludes assets built for another OS or architecture.
// Matching the platform does not establish hardware or runtime eligibility.
func CatalogForPlatform(platform string) []CatalogEntry {
	var entries []CatalogEntry
	for _, entry := range catalogEntries {
		if entry.Platform == "any" || entry.Platform == platform {
			entries = append(entries, entry)
		}
	}
	return entries
}

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

// DisplayName names an entry for the user. Entries that share a title — the
// pinned llama-server archives differ only by platform — carry the platform
// so they stay distinguishable.
func (e CatalogEntry) DisplayName() string {
	if e.SharesTitle() {
		return e.Title + " · " + PlatformLabel(e.Platform)
	}
	return e.Title
}

// SharesTitle reports whether another entry of the same kind carries the
// same title, which makes the title alone ambiguous.
func (e CatalogEntry) SharesTitle() bool {
	count := 0
	for _, other := range catalogEntries {
		if other.Kind == e.Kind && other.Title == e.Title {
			count++
		}
	}
	return count > 1
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
