package core

import (
	"regexp"
	"strings"
	"testing"
)

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestDefaultEngineCatalogDescriptionsAndManagedVenvSpecs(t *testing.T) {
	registry := DefaultEngineCatalog()
	for _, family := range registry.Families() {
		if family.Description == "" {
			t.Fatalf("%s has no description", family.ID)
		}
	}
	family, ok := registry.FamilyByID("mlx-lm")
	if !ok {
		t.Fatal("mlx-lm missing")
	}
	build := family.Releases[0].Builds[0]
	if build.AcquireMode != EngineAcquireManagedVenv || build.ManagedSpec == nil || len(build.ManagedSpec.WheelURLs) != 2 {
		t.Fatalf("unexpected mlx-lm managed spec: %+v", build)
	}
	cloned, _ := registry.FamilyByID("mlx-lm")
	cloned.Releases[0].Builds[0].ManagedSpec.WheelURLs[0] = "changed"
	original, _ := registry.FamilyByID("mlx-lm")
	if original.Releases[0].Builds[0].ManagedSpec.WheelURLs[0] == "changed" {
		t.Fatal("managed spec was not deep-cloned")
	}
	vllm, ok := registry.FamilyByID("vllm")
	if !ok {
		t.Fatal("vllm missing")
	}
	for _, rel := range vllm.Releases {
		for _, b := range rel.Builds {
			if strings.HasPrefix(b.Platform, "linux/") && b.AcquireMode == EngineAcquireManagedVenv {
				t.Fatalf("linux vllm build must not advertise managed venv install: %+v", b)
			}
			if b.Platform == "darwin/arm64" && b.AcquireMode != EngineAcquireManagedVenv {
				t.Fatalf("darwin/arm64 vllm build should use managed venv install: %+v", b)
			}
		}
	}
}

func TestCatalogEntriesAreUniqueAndComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range Catalog() {
		if entry.ID == "" {
			t.Fatal("entry without id", entry.Title)
		}
		if seen[entry.ID] {
			t.Fatal("duplicate id", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Title == "" {
			t.Fatal("entry without title", entry.ID)
		}
		if entry.URL == "" || !strings.HasPrefix(entry.URL, "https://") {
			t.Fatal("entry without https url", entry.ID)
		}
		if entry.SHA256 != "" && !hexDigest.MatchString(entry.SHA256) {
			t.Fatal("entry with malformed digest", entry.ID)
		}
		if entry.RuntimeURL != "" || entry.RuntimeSHA256 != "" || entry.RuntimeBytes != 0 {
			if !strings.HasPrefix(entry.RuntimeURL, "https://") || !validEngineSource(entry.RuntimeURL) {
				t.Fatal("entry with invalid runtime url", entry.ID)
			}
			if entry.RuntimeBytes <= 0 || !hexDigest.MatchString(entry.RuntimeSHA256) {
				t.Fatal("entry with invalid runtime size or digest", entry.ID)
			}
			if entry.Profile == "" {
				t.Fatal("entry with runtime archive missing profile", entry.ID)
			}
		}
	}
	for _, id := range []string{"engine-linux-x64", "engine-linux-arm64"} {
		entry, ok := CatalogEntryByID(id)
		if !ok || entry.Downloadable() {
			t.Fatalf("%s must not be marked downloadable: %+v", id, entry)
		}
	}
	winCPU, ok := CatalogEntryByID("engine-windows-x64")
	if !ok || winCPU.Profile == "" || !winCPU.Downloadable() {
		t.Fatalf("unexpected windows cpu entry: %+v", winCPU)
	}
	winCUDA, ok := CatalogEntryByID("engine-windows-cuda-x64")
	if !ok || winCUDA.Profile == "" || winCUDA.RuntimeURL == "" || !winCUDA.Downloadable() {
		t.Fatalf("unexpected windows cuda entry: %+v", winCUDA)
	}
	mlxModel, ok := CatalogEntryByID("model-qwen3-0.6b-mlx")
	if !ok || mlxModel.Format != "directory" || !mlxModel.Downloadable() {
		t.Fatalf("unexpected mlx directory catalog entry: %+v", mlxModel)
	}
}
