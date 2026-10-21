package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDirectoryModel(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "tokenizer.json", "model.safetensors"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRegistryDirectoryModelDiscoveryIsBoundedAndMergesGGUF(t *testing.T) {
	s := registryService(t)
	root := *s.settings.ModelsRoot
	model := filepath.Join(root, "organization", "model")
	writeDirectoryModel(t, model)
	writeDirectoryModel(t, filepath.Join(root, ".venv", "hidden"))
	writeDirectoryModel(t, filepath.Join(root, "venv", "hidden"))
	writeDirectoryModel(t, filepath.Join(root, "a", "b", "c", "d", "too-deep"))
	outside := filepath.Join(t.TempDir(), "outside")
	writeDirectoryModel(t, outside)
	if err := os.Symlink(outside, filepath.Join(root, "external-link")); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(root, "partial")
	if err := os.MkdirAll(partial, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	gguf := filepath.Join(root, "existing.gguf")
	s.device.Snapshot, _ = json.Marshal(map[string]any{"models": []any{map[string]any{"path": gguf, "status": "available", "size_bytes": 100}}})
	registry := s.Registry()
	if len(registry.Models) != 2 || registry.ModelError != nil {
		t.Fatal(registry)
	}
	formats := map[string]bool{}
	for _, asset := range registry.Models {
		formats[asset.Format] = true
		if asset.Format == "directory" {
			if !asset.Available || asset.SizeBytes != nil || asset.ContextLength != nil || asset.Architecture != "" {
				t.Fatal(asset)
			}
			path, failure := s.resourcePath(asset.Name, false)
			want, _ := canonical(model)
			if failure != nil || path != want {
				t.Fatal(path, failure)
			}
		}
	}
	if !formats["gguf"] || !formats["directory"] {
		t.Fatal(formats)
	}
	raw, _ := json.Marshal(registry)
	if strings.Contains(string(raw), root) || strings.Contains(string(raw), outside) {
		t.Fatal(string(raw))
	}
}

func TestDirectoryModelRejectsEscapingAssetSymlink(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "model")
	writeDirectoryModel(t, model)
	external := filepath.Join(t.TempDir(), "weights.safetensors")
	if err := os.WriteFile(external, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(model, "model.safetensors")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(model, "model.safetensors")); err != nil {
		t.Fatal(err)
	}
	assets, failure := scanDirectoryModels(root)
	if len(assets) != 0 || failure != nil {
		t.Fatal(assets, failure)
	}
}

func TestDirectoryDiscoveryLimitHasFixedReason(t *testing.T) {
	root := t.TempDir()
	for i := 0; i <= modelDiscoveryEntries; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("asset-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	assets, failure := scanDirectoryModels(root)
	if len(assets) != 0 || failure == nil || failure.Code != "model_discovery_limit" {
		t.Fatal(assets, failure)
	}
}
