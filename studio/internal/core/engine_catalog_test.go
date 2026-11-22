package core

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
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
	var hasPrism, hasUpstreamV1, hasUpstreamSnap, hasWinCUDA bool
	var prismIdx, v1Idx, snapIdx = -1, -1, -1
	for i, rel := range llama.Releases {
		switch rel.ID {
		case "llama-cpp-prism-b10743":
			hasPrism = rel.Channel == ReleaseStable
			prismIdx = i
		case "llama-cpp-upstream-v1.0.0":
			hasUpstreamV1 = rel.Channel == ReleaseStable
			v1Idx = i
		case "llama-cpp-upstream-b11146":
			hasUpstreamSnap = rel.Channel == ReleaseSnapshot
			snapIdx = i
		}
		for _, b := range rel.Builds {
			if b.ID == "engine-windows-cuda-x64" {
				hasWinCUDA = len(b.Artifacts) == 2 && b.TotalBytes() > 0 && b.HasSHA256()
			}
		}
	}
	if !hasPrism || !hasUpstreamV1 || !hasUpstreamSnap {
		t.Fatalf("llama-cpp must include PrismML stable, upstream v1.0.0 stable, and b11146 snapshot: %+v", llama.Releases)
	}
	if !(v1Idx < prismIdx && prismIdx < snapIdx) {
		t.Fatalf("expected llama-cpp releases sorted v1.0.0 -> prism -> b11146, got indices %d, %d, %d", v1Idx, prismIdx, snapIdx)
	}
	if llama.Releases[0].ID != "llama-cpp-upstream-v1.0.0" || llama.Releases[0].Channel != ReleaseStable || !llama.Releases[0].Recommended {
		t.Fatalf("expected llama-cpp recommended stable release v1.0.0 first, got %+v", llama.Releases[0])
	}
	if llama.ReleasesURL != "https://github.com/ggml-org/llama.cpp/releases" {
		t.Fatalf("llama-cpp ReleasesURL = %q, want upstream releases URL", llama.ReleasesURL)
	}
	if !hasWinCUDA {
		t.Fatal("windows cuda build must bundle engine and runtime artifacts")
	}

	for _, fam := range families {
		wantExternal := fam.ID == "lmstudio"
		if fam.ExternalApp != wantExternal {
			t.Fatalf("family %s ExternalApp = %v, want %v", fam.ID, fam.ExternalApp, wantExternal)
		}
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

	llama, _ := reg.FamilyByID("llama-cpp")
	v1 := llama.Releases[0]
	prism := llama.Releases[1]
	if v1.ID != "llama-cpp-upstream-v1.0.0" || prism.ID != "llama-cpp-prism-b10743" {
		t.Fatalf("unexpected release order in llama-cpp: %+v", llama.Releases)
	}
	platforms := v1.Platforms()
	wantPlatforms := []string{"darwin/arm64", "darwin/amd64", "windows/amd64", "linux/amd64", "linux/arm64"}
	if len(platforms) != len(wantPlatforms) {
		t.Fatalf("Platforms() = %v, want %v", platforms, wantPlatforms)
	}
	for i, want := range wantPlatforms {
		if platforms[i] != want {
			t.Fatalf("Platforms()[%d] = %q, want %q", i, platforms[i], want)
		}
	}
	winBuilds := prism.BuildsForPlatform("windows/amd64")
	if len(winBuilds) != 2 || winBuilds[0].ID != "engine-windows-x64" || winBuilds[1].ID != "engine-windows-cuda-x64" {
		t.Fatalf("unexpected BuildsForPlatform(windows/amd64): %+v", winBuilds)
	}

	sglangLatest := sglang.Releases[0]
	if plats := sglangLatest.Platforms(); len(plats) != 1 || plats[0] != "any" {
		t.Fatalf("expected [any] platform for sglang latest, got %v", plats)
	}
	if anyBuilds := sglangLatest.BuildsForPlatform("darwin/arm64"); len(anyBuilds) != 1 {
		t.Fatalf("expected BuildsForPlatform to include any-platform build, got %+v", anyBuilds)
	}

	for _, rel := range llama.Releases {
		if rel.ID != "llama-cpp-upstream-b11146" {
			continue
		}
		for _, build := range rel.Builds {
			if (build.ID == "llama-upstream-b11146-macos-arm64" || build.ID == "llama-upstream-b11146-macos-x64") && build.InstallCommand != "" {
				t.Fatalf("snapshot %s must not claim brew install (which installs stable 1.0.0): %q", build.ID, build.InstallCommand)
			}
		}
	}

	var checkedSGLangSnap bool
	for _, rel := range sglang.Releases {
		if rel.ID != "sglang-e262b44" {
			continue
		}
		for _, build := range rel.Builds {
			if build.ID != "engine-sglang-snapshot-any" {
				continue
			}
			checkedSGLangSnap = true
			wantArchive := "sglang-e262b44eef1f2bf83e34345276e2bb91a086587e.tar.gz"
			if len(build.Artifacts) != 1 || build.Artifacts[0].Filename != wantArchive {
				t.Fatalf("sglang snapshot artifact Filename = %+v, want %q", build.Artifacts, wantArchive)
			}
			if !strings.HasPrefix(build.InstallCommand, "tar -xzf "+wantArchive+" && ") {
				t.Fatalf("sglang snapshot InstallCommand must extract %q: %q", wantArchive, build.InstallCommand)
			}
			if !strings.Contains(build.InstallCommand, "./sglang-e262b44eef1f2bf83e34345276e2bb91a086587e/python") {
				t.Fatalf("sglang snapshot InstallCommand must point to python/ subdir: %q", build.InstallCommand)
			}
			if _, afterPip, ok := strings.Cut(build.InstallCommand, "uv pip install"); !ok || strings.Contains(afterPip, ".tar.gz") {
				t.Fatalf("sglang snapshot uv pip install must not pass .tar.gz directly: %q", build.InstallCommand)
			}

			// Verify the first tar step extracts the actual GitHub archive filename into python/.
			firstStep, _, _ := strings.Cut(build.InstallCommand, "&&")
			fields := strings.Fields(firstStep)
			if len(fields) < 3 {
				t.Fatalf("unexpected tar step in InstallCommand: %q", firstStep)
			}
			if _, err := exec.LookPath(fields[0]); err == nil {
				tmpDir := t.TempDir()
				archivePath := filepath.Join(tmpDir, build.Artifacts[0].Filename)
				f, err := os.Create(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				gz := gzip.NewWriter(f)
				tw := tar.NewWriter(gz)
				content := []byte("[project]\nname = \"sglang\"\n")
				if err := tw.WriteHeader(&tar.Header{
					Name: "sglang-e262b44eef1f2bf83e34345276e2bb91a086587e/python/pyproject.toml",
					Mode: 0o644,
					Size: int64(len(content)),
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(content); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}

				cmd := exec.Command(fields[0], fields[1:]...)
				cmd.Dir = tmpDir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("tar extract step %q failed: %v\n%s", firstStep, err, out)
				}
				extracted := filepath.Join(tmpDir, "sglang-e262b44eef1f2bf83e34345276e2bb91a086587e", "python", "pyproject.toml")
				if _, err := os.Stat(extracted); err != nil {
					t.Fatalf("expected extracted file %q: %v", extracted, err)
				}
			}
		}
	}
	if !checkedSGLangSnap {
		t.Fatal("missing sglang-e262b44 snapshot build")
	}
}
