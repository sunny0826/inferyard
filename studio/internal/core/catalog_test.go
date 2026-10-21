package core

import (
	"strings"
	"testing"
)

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
		if entry.SHA256 != "" && len(entry.SHA256) != 64 {
			t.Fatal("entry with malformed digest", entry.ID)
		}
	}
}

func TestCatalogDownloadableIsDerivedFromTheRealValidators(t *testing.T) {
	for _, entry := range Catalog() {
		downloadable := entry.Downloadable()
		switch entry.Kind {
		case CatalogEngine:
			want := entry.Reason == "" && entry.Bytes <= maxFetchBytes && isLlamaArchive(entry.URL) && validEngineSource(entry.URL)
			if downloadable != want {
				t.Fatal("engine downloadability drifted from the transfer path", entry.ID)
			}
			if downloadable && !validEngineSource(entry.URL) {
				t.Fatal("downloadable engine rejected by the engine validator", entry.ID)
			}
		case CatalogModel:
			want := entry.Reason == "" && validSource(entry.URL)
			if downloadable != want {
				t.Fatal("model downloadability drifted from the transfer path", entry.ID)
			}
			if downloadable && !validSource(entry.URL) {
				t.Fatal("downloadable model rejected by the model validator", entry.ID)
			}
		default:
			t.Fatal("unknown kind", entry.ID)
		}
	}
}

func TestCatalogReferenceEntriesExplainWhyTheyAreNotDownloadable(t *testing.T) {
	reference := 0
	for _, entry := range Catalog() {
		if entry.Downloadable() {
			continue
		}
		reference++
		if entry.Reason == "" {
			t.Fatal("reference entry without a reason", entry.ID)
		}
		if entry.Doc == "" {
			t.Fatal("reference entry without a preparation document", entry.ID)
		}
	}
	if reference == 0 {
		t.Fatal("catalog lost its reference layer")
	}
}

func TestCatalogEngineForPlatform(t *testing.T) {
	cases := map[string]string{
		"darwin/arm64":  "engine-macos-arm64",
		"darwin/amd64":  "engine-macos-x64",
		"linux/amd64":   "engine-linux-x64",
		"linux/arm64":   "engine-linux-arm64",
		"windows/amd64": "engine-windows-x64",
	}
	for platform, want := range cases {
		id := CatalogEngineForPlatform(platform[:strings.Index(platform, "/")], platform[strings.Index(platform, "/")+1:])
		if id != want {
			t.Fatalf("%s = %q, want %q", platform, id, want)
		}
		entry, ok := CatalogEntryByID(id)
		if !ok {
			t.Fatal("unknown id", id)
		}
		if !entry.Downloadable() {
			t.Fatal("platform engine is not downloadable", id)
		}
	}
	if id := CatalogEngineForPlatform("plan9", "arm64"); id != "" {
		t.Fatal("unknown platform must not resolve", id)
	}
}

func TestCatalogDisplayNameIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range Catalog() {
		name := entry.DisplayName()
		if seen[name] {
			t.Fatal("two entries share a display name", entry.ID, name)
		}
		seen[name] = true
	}
}

func TestDefaultURLsComeFromTheCatalog(t *testing.T) {
	model, ok := CatalogEntryByID("model-smollm2-135m")
	if !ok {
		t.Fatal("built-in model missing from the catalog")
	}
	if DefaultModelURL() != model.URL {
		t.Fatal("default model url is not the catalog entry", DefaultModelURL())
	}
	if !model.Downloadable() {
		t.Fatal("built-in model is not downloadable")
	}
	engine, ok := CatalogEntryByID(CatalogEngineForCurrentPlatform())
	if !ok {
		t.Skip("no engine build for this platform")
	}
	if DefaultEngineURL() != engine.URL {
		t.Fatal("default engine url is not the catalog entry", DefaultEngineURL())
	}
}
