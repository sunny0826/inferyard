package core

import (
	"strings"
	"testing"
)

func TestDefaultEngineCatalogIntegrity(t *testing.T) {
	reg := DefaultEngineCatalog()
	families := reg.Families()
	wantFamilies := []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"}
	if len(families) != len(wantFamilies) {
		t.Fatalf("family count = %d, want %d", len(families), len(wantFamilies))
	}
	for _, id := range wantFamilies {
		fam, ok := reg.FamilyByID(id)
		if !ok {
			t.Fatalf("missing family %s", id)
		}
		if fam.Name == "" || fam.ModelFormat == "" || len(fam.Releases) == 0 {
			t.Fatalf("incomplete family %s: %+v", id, fam)
		}
		for _, rel := range fam.Releases {
			if rel.ID == "" || rel.Version == "" || len(rel.Builds) == 0 {
				t.Fatalf("incomplete release %s/%s: %+v", id, rel.ID, rel)
			}
			for _, build := range rel.Builds {
				if build.ID == "" || build.Platform == "" || len(build.Artifacts) == 0 {
					t.Fatalf("incomplete build %s/%s/%s: %+v", id, rel.ID, build.ID, build)
				}
				for _, art := range build.Artifacts {
					if !strings.HasPrefix(art.URL, "https://") {
						t.Fatalf("non-https artifact URL in %s/%s: %q", id, build.ID, art.URL)
					}
					if art.SHA256 != "" && !hexDigest.MatchString(art.SHA256) {
						t.Fatalf("invalid sha256 in %s/%s: %q", id, build.ID, art.SHA256)
					}
				}
			}
		}
	}

	llama, _ := reg.FamilyByID("llama-cpp")
	var hasPrism, hasUpstream, hasWinCUDA bool
	for _, rel := range llama.Releases {
		if strings.Contains(rel.ID, "prism") {
			hasPrism = true
		}
		if strings.Contains(rel.ID, "upstream") {
			hasUpstream = true
		}
		for _, b := range rel.Builds {
			if b.ID == "engine-windows-cuda-x64" {
				hasWinCUDA = len(b.Artifacts) == 2 && b.TotalBytes() > 0 && b.HasSHA256()
			}
		}
	}
	if !hasPrism || !hasUpstream {
		t.Fatalf("llama-cpp must include both PrismML and upstream releases: %+v", llama.Releases)
	}
	if !hasWinCUDA {
		t.Fatal("windows cuda build must bundle engine and runtime artifacts")
	}
}

func TestSortEngineReleasesStableFirst(t *testing.T) {
	input := []EngineRelease{
		{ID: "snap", Channel: ReleaseSnapshot, Recommended: true},
		{ID: "pre", Channel: ReleasePreRelease, Recommended: true},
		{ID: "latest", Channel: ReleaseLatest, Recommended: false},
		{ID: "stable-normal", Channel: ReleaseStable, Recommended: false},
		{ID: "stable-rec", Channel: ReleaseStable, Recommended: true},
	}
	got := SortEngineReleases(input)
	want := []string{"stable-rec", "stable-normal", "latest", "pre", "snap"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("sorted[%d] = %s, want %s (full=%+v)", i, got[i].ID, id, got)
		}
	}
}

func TestEngineCatalogProviderExtensionAndIsolation(t *testing.T) {
	base := DefaultEngineCatalog()
	beforeCount := len(base.Families())

	// Register a new release into an existing family and a brand-new family.
	base.Register(StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID: "llama-cpp",
			Releases: []EngineRelease{
				{
					ID:           "llama-cpp-custom-rc",
					Version:      "b12000-rc1",
					Distribution: "扩展构建",
					Channel:      ReleasePreRelease,
					Builds: []EnginePlatformBuild{{
						ID: "custom-rc-mac", Platform: "darwin/arm64", Variant: "Metal", AcquireMode: EngineAcquireExternalPage,
						Artifacts: []EngineArtifact{{Role: "release_page", URL: "https://example.com/llama/rc1"}},
					}},
				},
			},
		},
	})
	base.Register(StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "koboldcpp",
			Name:           "KoboldCpp",
			ModelFormat:    "gguf",
			SupportsWizard: false,
			Releases: []EngineRelease{
				{
					ID: "kobold-snap", Version: "nightly", Channel: ReleaseSnapshot,
					Builds: []EnginePlatformBuild{{
						ID: "k-snap", Platform: "any", AcquireMode: EngineAcquireExternalPage,
						Artifacts: []EngineArtifact{{Role: "release_page", URL: "https://example.com/kobold/nightly"}},
					}},
				},
				{
					ID: "kobold-1.91", Version: "v1.91", Channel: ReleaseStable, Recommended: true,
					Builds: []EnginePlatformBuild{{
						ID: "k-191", Platform: "any", AcquireMode: EngineAcquireExternalPage,
						Artifacts: []EngineArtifact{{Role: "release_page", URL: "https://example.com/kobold/v1.91"}},
					}},
				},
			},
		},
	})

	if len(base.Families()) != beforeCount+1 {
		t.Fatalf("expected %d families after custom registration, got %d", beforeCount+1, len(base.Families()))
	}
	kobold, ok := base.FamilyByID("koboldcpp")
	if !ok || kobold.Releases[0].ID != "kobold-1.91" {
		t.Fatalf("custom family not sorted stable-first: %+v", kobold)
	}
	if _, leaked := DefaultEngineCatalog().FamilyByID("koboldcpp"); leaked {
		t.Fatal("mutating registry leaked into DefaultEngineCatalog()")
	}
}

func TestReleasesForPlatformAndStableFilter(t *testing.T) {
	reg := DefaultEngineCatalog()
	sglang, _ := reg.FamilyByID("sglang")
	if all := sglang.ReleasesForPlatform("darwin/arm64", false); len(all) != 2 {
		t.Fatalf("expected 2 sglang releases on darwin/arm64, got %d", len(all))
	}
	if stable := sglang.ReleasesForPlatform("darwin/arm64", true); len(stable) != 1 || stable[0].ID != "sglang-releases" {
		t.Fatalf("expected only official releases when stableOnly=true, got %+v", stable)
	}

	mlx, _ := reg.FamilyByID("mlx-lm")
	if win := mlx.ReleasesForPlatform("windows/amd64", false); len(win) != 0 {
		t.Fatalf("mlx-lm must not match windows/amd64: %+v", win)
	}
	if mac := mlx.ReleasesForPlatform("darwin/arm64", false); len(mac) != 1 || len(mac[0].Builds) != 1 {
		t.Fatalf("unexpected mlx-lm darwin/arm64 builds: %+v", mac)
	}
}
