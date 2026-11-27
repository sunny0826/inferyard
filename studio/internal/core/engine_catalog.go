package core

import (
	"inferyard/studio/internal/l10n"
	"slices"
	"sort"
	"strings"
)

type EngineAcquireMode string

const (
	EngineAcquireDirect        EngineAcquireMode = "direct_download"
	EngineAcquireManagedSetup  EngineAcquireMode = "managed_setup"
	EngineAcquireManagedVenv   EngineAcquireMode = "managed_venv"
	EngineAcquireManualArchive EngineAcquireMode = "manual_archive"
	EngineAcquireExternalPage  EngineAcquireMode = "external_page"
)

type ReleaseChannel string

const (
	ReleaseStable     ReleaseChannel = "stable"
	ReleaseLatest     ReleaseChannel = "latest"
	ReleasePreRelease ReleaseChannel = "prerelease"
	ReleaseSnapshot   ReleaseChannel = "snapshot"
)

type EngineArtifact struct {
	Role     string
	Label    string
	Version  string
	Filename string
	URL      string
	Bytes    int64
	SHA256   string
}

type EngineManagedVenvSpec struct {
	Python    string
	Packages  []string
	WheelURLs []string
}

type EnginePlatformBuild struct {
	ID             string
	Platform       string
	Variant        string
	AcquireMode    EngineAcquireMode
	Profile        string
	Artifacts      []EngineArtifact
	ManagedSpec    *EngineManagedVenvSpec
	InstallCommand string
	Note           string
}

type EngineRelease struct {
	ID           string
	Version      string
	Revision     string
	Distribution string
	Channel      ReleaseChannel
	Recommended  bool
	ReleaseURL   string
	Builds       []EnginePlatformBuild
}

type EngineFamilyCatalog struct {
	ID             string
	Name           string
	Description    string
	ModelFormat    string
	HomepageURL    string
	ReleasesURL    string
	SupportsWizard bool
	ExternalApp    bool
	Releases       []EngineRelease
}

type EngineCatalogProvider interface {
	FamilyID() string
	Catalog() EngineFamilyCatalog
}

type StaticEngineProvider struct {
	Family EngineFamilyCatalog
}

func (p StaticEngineProvider) FamilyID() string {
	return p.Family.ID
}

func (p StaticEngineProvider) Catalog() EngineFamilyCatalog {
	return cloneEngineFamilyCatalog(p.Family)
}

type EngineCatalogRegistry struct {
	order    []string
	families map[string]EngineFamilyCatalog
}

func NewEngineCatalogRegistry(providers ...EngineCatalogProvider) *EngineCatalogRegistry {
	reg := &EngineCatalogRegistry{
		families: map[string]EngineFamilyCatalog{},
	}
	for _, provider := range providers {
		reg.Register(provider)
	}
	return reg
}

func (r *EngineCatalogRegistry) Register(provider EngineCatalogProvider) {
	if r == nil || provider == nil {
		return
	}
	if r.families == nil {
		r.families = map[string]EngineFamilyCatalog{}
	}
	incoming := cloneEngineFamilyCatalog(provider.Catalog())
	if incoming.ID == "" {
		if id := provider.FamilyID(); id != "" {
			incoming.ID = id
		} else {
			return
		}
	}
	existing, ok := r.families[incoming.ID]
	if !ok {
		r.order = append(r.order, incoming.ID)
		r.families[incoming.ID] = incoming
		return
	}
	if existing.Name == "" {
		existing.Name = incoming.Name
	}
	if existing.Description == "" {
		existing.Description = incoming.Description
	}
	if existing.ModelFormat == "" {
		existing.ModelFormat = incoming.ModelFormat
	}
	if existing.HomepageURL == "" {
		existing.HomepageURL = incoming.HomepageURL
	}
	if existing.ReleasesURL == "" {
		existing.ReleasesURL = incoming.ReleasesURL
	}
	existing.SupportsWizard = existing.SupportsWizard || incoming.SupportsWizard
	existing.ExternalApp = existing.ExternalApp || incoming.ExternalApp
	for _, rel := range incoming.Releases {
		replaced := false
		for i := range existing.Releases {
			if existing.Releases[i].ID == rel.ID && rel.ID != "" {
				existing.Releases[i] = rel
				replaced = true
				break
			}
		}
		if !replaced {
			existing.Releases = append(existing.Releases, rel)
		}
	}
	r.families[incoming.ID] = existing
}

func (r *EngineCatalogRegistry) Families() []EngineFamilyCatalog {
	if r == nil {
		return nil
	}
	out := make([]EngineFamilyCatalog, 0, len(r.order))
	for _, id := range r.order {
		fam := cloneEngineFamilyCatalog(r.families[id])
		fam.Releases = SortEngineReleases(fam.Releases)
		out = append(out, fam)
	}
	return out
}

func (r *EngineCatalogRegistry) FamilyByID(id string) (EngineFamilyCatalog, bool) {
	if r == nil || r.families == nil {
		return EngineFamilyCatalog{}, false
	}
	fam, ok := r.families[id]
	if !ok {
		return EngineFamilyCatalog{}, false
	}
	out := cloneEngineFamilyCatalog(fam)
	out.Releases = SortEngineReleases(out.Releases)
	return out, true
}

func ReleaseChannelRank(ch ReleaseChannel) int {
	switch ch {
	case ReleaseStable:
		return 0
	case ReleaseLatest:
		return 1
	case ReleasePreRelease:
		return 2
	case ReleaseSnapshot:
		return 3
	default:
		return 4
	}
}

func SortEngineReleases(releases []EngineRelease) []EngineRelease {
	out := make([]EngineRelease, len(releases))
	for i, rel := range releases {
		out[i] = cloneEngineRelease(rel)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := ReleaseChannelRank(out[i].Channel), ReleaseChannelRank(out[j].Channel)
		if ri != rj {
			return ri < rj
		}
		if out[i].Recommended != out[j].Recommended {
			return out[i].Recommended
		}
		return false
	})
	return out
}

func (f EngineFamilyCatalog) ReleasesForPlatform(platform string, stableOnly bool) []EngineRelease {
	var filtered []EngineRelease
	for _, rel := range f.Releases {
		if stableOnly && rel.Channel != ReleaseStable && rel.Channel != ReleaseLatest {
			continue
		}
		var builds []EnginePlatformBuild
		for _, build := range rel.Builds {
			if platform == "" || platform == "all" || build.Platform == platform || build.Platform == "any" {
				builds = append(builds, cloneEnginePlatformBuild(build))
			}
		}
		if len(builds) == 0 {
			continue
		}
		copied := cloneEngineRelease(rel)
		copied.Builds = builds
		filtered = append(filtered, copied)
	}
	return SortEngineReleases(filtered)
}

func (r EngineRelease) Platforms() []string {
	var platforms []string
	seen := make(map[string]bool, len(r.Builds))
	for _, build := range r.Builds {
		if build.Platform == "" || seen[build.Platform] {
			continue
		}
		seen[build.Platform] = true
		platforms = append(platforms, build.Platform)
	}
	return platforms
}

func (r EngineRelease) BuildsForPlatform(platform string) []EnginePlatformBuild {
	var builds []EnginePlatformBuild
	for _, build := range r.Builds {
		if platform == "" || platform == "all" || build.Platform == platform || build.Platform == "any" {
			builds = append(builds, cloneEnginePlatformBuild(build))
		}
	}
	return builds
}

func (b EnginePlatformBuild) PrimaryURL() string {
	if len(b.Artifacts) == 0 {
		return ""
	}
	return b.Artifacts[0].URL
}

func (b EnginePlatformBuild) TotalBytes() int64 {
	var total int64
	for _, artifact := range b.Artifacts {
		if artifact.Bytes > 0 {
			total += artifact.Bytes
		}
	}
	return total
}

func (b EnginePlatformBuild) HasSHA256() bool {
	for _, artifact := range b.Artifacts {
		if strings.TrimSpace(artifact.SHA256) != "" {
			return true
		}
	}
	return false
}

func (b EnginePlatformBuild) CanDirectDownloadOn(currentPlatform string) bool {
	return b.AcquireMode == EngineAcquireDirect && (b.Platform == currentPlatform || b.Platform == "any")
}

func (b EnginePlatformBuild) CanManagedVenvInstallOn(currentPlatform string) bool {
	return b.AcquireMode == EngineAcquireManagedVenv && b.ManagedSpec != nil &&
		(b.Platform == currentPlatform || b.Platform == "any")
}

func ChannelLabel(ch ReleaseChannel) string {
	switch ch {
	case ReleaseStable:
		return l10n.TR("core.engine_catalog.channel.stable")
	case ReleaseLatest:
		return l10n.TR("core.engine_catalog.channel.latest")
	case ReleasePreRelease:
		return l10n.TR("core.engine_catalog.channel.prerelease")
	case ReleaseSnapshot:
		return l10n.TR("core.engine_catalog.channel.snapshot")
	default:
		return string(ch)
	}
}

func AcquireModeLabel(mode EngineAcquireMode) string {
	switch mode {
	case EngineAcquireDirect:
		return l10n.TR("core.engine_catalog.acquire.direct")
	case EngineAcquireManagedSetup:
		return l10n.TR("core.engine_catalog.acquire.managed_setup")
	case EngineAcquireManagedVenv:
		return l10n.TR("core.engine_catalog.acquire.managed_venv")
	case EngineAcquireManualArchive:
		return l10n.TR("core.engine_catalog.acquire.manual_archive")
	case EngineAcquireExternalPage:
		return l10n.TR("core.engine_catalog.acquire.external_page")
	default:
		return string(mode)
	}
}

func ModelFormatLabel(format string) string {
	switch format {
	case "gguf":
		return "GGUF"
	case "directory":
		return l10n.TR("core.engine_catalog.model_format.directory")
	default:
		return format
	}
}

func CatalogPlatforms() []string {
	return []string{"darwin/arm64", "darwin/amd64", "windows/amd64", "linux/amd64", "linux/arm64"}
}

func cloneEnginePlatformBuild(b EnginePlatformBuild) EnginePlatformBuild {
	b.Artifacts = slices.Clone(b.Artifacts)
	if b.ManagedSpec != nil {
		spec := *b.ManagedSpec
		spec.Packages = slices.Clone(spec.Packages)
		spec.WheelURLs = slices.Clone(spec.WheelURLs)
		b.ManagedSpec = &spec
	}
	return b
}

func cloneEngineRelease(r EngineRelease) EngineRelease {
	builds := make([]EnginePlatformBuild, len(r.Builds))
	for i, b := range r.Builds {
		builds[i] = cloneEnginePlatformBuild(b)
	}
	r.Builds = builds
	return r
}

func cloneEngineFamilyCatalog(f EngineFamilyCatalog) EngineFamilyCatalog {
	releases := make([]EngineRelease, len(f.Releases))
	for i, r := range f.Releases {
		releases[i] = cloneEngineRelease(r)
	}
	f.Releases = releases
	return f
}
