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
