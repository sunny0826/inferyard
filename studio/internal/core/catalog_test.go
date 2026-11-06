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

func TestCatalogReferenceEntriesExplainWhyTheyAreNotDownloadable(t *testing.T) {
	for _, entry := range Catalog() {
		if entry.Downloadable() {
			continue
		}
		if entry.Reason == "" {
			t.Fatal("reference entry without a reason", entry.ID)
		}
		if entry.Doc == "" {
			t.Fatal("reference entry without a preparation document", entry.ID)
		}
	}
}

func TestCatalogEngineForPlatform(t *testing.T) {
	cases := map[string]string{
		"darwin/arm64": "engine-macos-arm64", "darwin/amd64": "engine-macos-x64",
		"linux/amd64": "engine-linux-x64", "linux/arm64": "engine-linux-arm64",
		"windows/amd64": "engine-windows-x64",
	}
	for platform, want := range cases {
		id := CatalogEngineForPlatform(platform[:strings.Index(platform, "/")], platform[strings.Index(platform, "/")+1:])
		if id != want {
			t.Fatalf("%s = %q, want %q", platform, id, want)
		}
		entry, ok := CatalogEntryByID(id)
		if !ok || !entry.Downloadable() {
			t.Fatal("platform engine unavailable", id)
		}
	}
	if id := CatalogEngineForPlatform("plan9", "arm64"); id != "" {
		t.Fatal("unknown platform must not resolve", id)
	}
}
