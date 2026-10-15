package core

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func registryService(t *testing.T) *Service {
	t.Helper()
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		v, _ := version(args)
		return v, nil
	})
	s.settings.WorkspaceRoot, s.settings.ModelsRoot = ptr(t.TempDir()), ptr(t.TempDir())
	userRoot := t.TempDir()
	s.registryRoots = func(workspace string) []string { return []string{filepath.Join(workspace, "engines"), userRoot} }
	return s
}

func TestRegistryEngineRootsDepthAndPlainFile(t *testing.T) {
	s := registryService(t)
	roots := s.registryRoots(*s.settings.WorkspaceRoot)
	old := writeEngineBinary(t, filepath.Join(roots[0], "release"), 0755, time.Unix(100, 0))
	user := writeEngineBinary(t, filepath.Join(roots[1], "release"), 0755, time.Unix(200, 0))
	depth4 := writeEngineBinary(t, filepath.Join(roots[0], "a", "b", "c", "d"), 0755, time.Unix(300, 0))
	writeEngineBinary(t, filepath.Join(roots[0], "a", "b", "c", "d", "e"), 0755, time.Unix(500, 0))
	writeEngineBinary(t, filepath.Join(roots[0], "no-exec"), 0600, time.Unix(600, 0))
	dir := filepath.Join(roots[0], "plain")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "inference-engine")
	if err := os.WriteFile(plain, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	s.downloads = []*downloadEntry{{Task: DownloadTask{Kind: "engine", State: "done", OutDir: dir, EnginePath: &plain, FinishedAt: ptr("2026-10-15T00:00:00Z")}}}
	registry := s.Registry()
	if len(registry.Engines) != 4 || !strings.Contains(registry.DefaultEngine, "inference-engine") || !registry.Engines[0].LatestDownload {
		t.Fatalf("%+v", registry)
	}
	seen := map[string]bool{}
	for _, e := range registry.Engines {
		if seen[e.Name] {
			t.Fatal("duplicate", e.Name)
		}
		seen[e.Name] = true
		path, err := s.resourcePath(e.Name, true)
		if err != nil {
			t.Fatal(err)
		}
		if path != mustResolve(t, roots[0], old) && path != mustResolve(t, roots[0], depth4) && path != mustResolve(t, roots[1], user) && path != mustResolve(t, roots[0], plain) {
			t.Fatal(path)
		}
	}
	raw, _ := json.Marshal(registry)
	if strings.Contains(string(raw), *s.settings.WorkspaceRoot) || strings.Contains(string(raw), roots[1]) || strings.Contains(string(raw), "EnginePath") {
		t.Fatal(string(raw))
	}
	// A newer task from another workspace must not become the current choice.
	foreign := filepath.Join(t.TempDir(), "engine")
	if err := os.WriteFile(foreign, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	s.downloads = append(s.downloads, &downloadEntry{Task: DownloadTask{Kind: "engine", State: "done", OutDir: filepath.Dir(foreign), EnginePath: &foreign, FinishedAt: ptr("2026-10-16T00:00:00Z")}})
	if s.Registry().DefaultEngine != registry.DefaultEngine {
		t.Fatal("foreign download adopted")
	}
}

func TestRegistryModelsCombineAndDeduplicate(t *testing.T) {
	s := registryService(t)
	root := *s.settings.ModelsRoot
	downloaded := filepath.Join(root, "model")
	if err := os.MkdirAll(downloaded, 0700); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(downloaded, "a.gguf"), filepath.Join(downloaded, "b.gguf")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := map[string]any{"models": []any{map[string]any{"path": a, "status": "available", "size_bytes": 7, "architecture": "llama", "context_length": 4096}, map[string]any{"path": filepath.Join(root, "broken.gguf"), "status": "unavailable", "reason": "invalid_or_unreadable_gguf"}}}
	s.device.Snapshot, _ = json.Marshal(snapshot)
	s.downloads = []*downloadEntry{{Task: DownloadTask{Kind: "model", State: "done", OutDir: downloaded}}}
	registry := s.Registry()
	if len(registry.Models) != 3 {
		t.Fatal(registry)
	}
	var known, missing bool
	for _, m := range registry.Models {
		if m.Name == "a.gguf" {
			known = m.Available && m.Architecture == "llama" && m.ContextLength != nil && *m.ContextLength == 4096
		}
		if m.Name == "b.gguf" {
			missing = m.Available && m.ContextLength == nil && m.Architecture == ""
		}
		if m.Name == "broken.gguf" {
			if _, e := s.resourcePath(m.Name, false); e == nil {
				t.Fatal("unavailable model selectable")
			}
		}
	}
	if !known || !missing {
		t.Fatal(registry)
	}
}

func TestRegistrySelectionReachesExecutor(t *testing.T) {
	s := registryService(t)
	roots := s.registryRoots(*s.settings.WorkspaceRoot)
	engine := writeEngineBinary(t, filepath.Join(roots[0], "release"), 0755, time.Now())
	model := filepath.Join(*s.settings.ModelsRoot, "model.gguf")
	if err := os.WriteFile(model, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	s.device.Snapshot, _ = json.Marshal(map[string]any{"models": []any{map[string]any{"path": model, "status": "available"}}})
	var calls [][]string
	s.exec = func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		calls = append(calls, args)
		return inferyard.Execution{ExitCode: 2, Stdout: []byte(`{"status":"blocked","limitations":[]}`)}, nil
	}
	registry := s.Registry()
	for _, tc := range []struct{ command, model, engine, flag, path string }{
		{"inferyard device-check --out " + filepath.Join(t.TempDir(), "preflight"), registry.Models[0].Name, "", "--model", model},
		{"inferyard config create --bundle zh-core --out " + filepath.Join(t.TempDir(), "candidate"), "", registry.DefaultEngine, "--engine", engine},
	} {
		_, _ = s.WizardExecute(context.Background(), WizardRequest{Command: tc.command, ModelName: tc.model, EngineName: tc.engine})
		if len(calls) == 0 {
			t.Fatal("executor not called")
		}
		args := calls[len(calls)-1]
		found := ""
		for i, a := range args {
			if a == tc.flag {
				found = args[i+1]
			}
		}
		want, _ := canonical(tc.path)
		if found != want {
			t.Fatal(args, want)
		}
	}
	before := len(calls)
	if _, e := s.WizardExecute(context.Background(), WizardRequest{Command: "inferyard config create --bundle zh-core --out " + filepath.Join(t.TempDir(), "new"), EngineName: "missing"}); e == nil {
		t.Fatal("unknown selection admitted")
	}
	if len(calls) != before {
		t.Fatal("unknown selection reached executor")
	}
}

func TestRegistrySameRootNamesKeepIdentityWhenMtimeChanges(t *testing.T) {
	s := registryService(t)
	root := s.registryRoots(*s.settings.WorkspaceRoot)[0]
	a := writeEngineBinary(t, filepath.Join(root, "a", "release"), 0755, time.Unix(100, 0))
	writeEngineBinary(t, filepath.Join(root, "b", "release"), 0755, time.Unix(200, 0))
	before := map[string]string{}
	for _, entry := range s.Registry().Engines {
		path, e := s.resourcePath(entry.Name, true)
		if e != nil {
			t.Fatal(e)
		}
		before[entry.Name] = path
	}
	if len(before) != 2 {
		t.Fatal(before)
	}
	if err := os.Chtimes(a, time.Unix(300, 0), time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	for name, path := range before {
		actual, e := s.resourcePath(name, true)
		if e != nil || actual != path {
			t.Fatal("selection changed identity", name, actual, path, e)
		}
	}
}

func TestRegistryInvalidSnapshotReportsFailure(t *testing.T) {
	s := registryService(t)
	s.device.Snapshot = json.RawMessage("{")
	got := s.Registry()
	if got.ModelError == nil || got.ModelError.Code != "cli_error" {
		t.Fatal(got)
	}
}

func TestScanEngineBinariesAcceptsExecutableNamesCaseInsensitively(t *testing.T) {
	for _, name := range []string{"llama-server", "llama-server.exe", "LLAMA-SERVER.EXE", "Llama-Server"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0755); err != nil {
				t.Fatal(err)
			}
			if got := scanEngineBinaries(root); len(got) != 1 {
				t.Fatalf("missing executable %s: %+v", name, got)
			}
		})
	}
}
