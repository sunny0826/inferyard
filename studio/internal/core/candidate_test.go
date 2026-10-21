package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeCandidateToml(t *testing.T, dir, modelPath, enginePath string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	model, _ := json.Marshal(modelPath)
	engine, _ := json.Marshal(enginePath)
	content := fmt.Sprintf("[model]\nlocal_path = %s\n\n[engine]\nbinary_path = %s\nstartup_args = []\n", model, engine)
	if err := os.WriteFile(filepath.Join(dir, "candidate.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func candidateFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	s := registryService(t)
	s.exec = func(ctx context.Context, binary string, args ...string) (inferyard.Execution, error) {
		if len(args) == 4 && args[0] == "config" && args[1] == "describe" {
			raw, _ := os.ReadFile(args[3])
			if strings.Count(string(raw), "[engine]") != 1 {
				return inferyard.Execution{ExitCode: 2}, nil
			}
			model := ""
			for _, line := range strings.Split(string(raw), "\n") {
				if value, ok := strings.CutPrefix(line, "local_path = "); ok {
					model, _ = strconv.Unquote(value)
				}
			}
			out, _ := engineFixtureDescription(args)
			var response map[string]any
			_ = json.Unmarshal(out.Stdout, &response)
			response["details"].(map[string]any)["assets"].(map[string]any)["model"].(map[string]any)["path"] = model
			out.Stdout, _ = json.Marshal(response)
			return out, nil
		}
		out, _ := version(args)
		return out, nil
	}
	model := filepath.Join(*s.settings.ModelsRoot, "model.gguf")
	if err := os.WriteFile(model, []byte("gguf"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := []byte(fmt.Sprintf(`{"models":[{"path":%q,"status":"available"}]}`, model))
	s.device.Snapshot = snapshot
	root := s.registryRoots(*s.settings.WorkspaceRoot)[0]
	engine := writeEngineBinary(t, filepath.Join(root, "release"), 0755, time.Unix(100, 0))
	// Registry entries are canonical (symlink-resolved); compare against those.
	if resolved, err := canonical(model); err == nil {
		model = resolved
	}
	if resolved, err := canonical(engine); err == nil {
		engine = resolved
	}
	return s, model, engine
}

func TestCandidateAssetsParsing(t *testing.T) {
	s, model, engine := candidateFixture(t)
	w := *s.settings.WorkspaceRoot
	dir := writeCandidateToml(t, filepath.Join(w, "candidates", "zh-core-1"), model, engine)
	assets, e := s.CandidateAssets(dir)
	if e != nil {
		t.Fatal(e)
	}
	if assets.ModelPath != model || assets.EnginePath != engine {
		t.Fatalf("%+v", assets)
	}
	if _, e = s.CandidateAssets(filepath.Join(w, "candidates", "missing")); e == nil || e.Code != "not_found" {
		t.Fatal(e)
	}
	if _, e = s.CandidateAssets("/etc/passwd"); e == nil || e.Code != "invalid_request" {
		t.Fatal(e)
	}
	bad := filepath.Join(w, "candidates", "bad-1")
	writeCandidateToml(t, bad, model, engine)
	raw, _ := os.ReadFile(filepath.Join(bad, "candidate.toml"))
	raw = append(raw, []byte("\n[engine]\nbinary_path = \"duplicate\"\n")...)
	if err := os.WriteFile(filepath.Join(bad, "candidate.toml"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, e = s.CandidateAssets(bad); e == nil || e.Code != "cli_blocked" {
		t.Fatal(e)
	}
}

func TestCandidateMatchesCombination(t *testing.T) {
	s, model, engine := candidateFixture(t)
	w := *s.settings.WorkspaceRoot
	data := s.registry()
	var modelName, engineName string
	for _, entry := range data.models {
		modelName = entry.model.Name
	}
	for _, entry := range data.engines {
		engineName = entry.engine.Name
	}
	dir := writeCandidateToml(t, filepath.Join(w, "candidates", "zh-core-1"), model, engine)
	match, known := s.CandidateMatchesCombination(dir, modelName, engineName)
	if !known || !match {
		t.Fatal(match, known)
	}
	other := writeEngineBinary(t, filepath.Join(s.registryRoots(w)[0], "older"), 0755, time.Unix(50, 0))
	dir2 := writeCandidateToml(t, filepath.Join(w, "candidates", "zh-core-2"), model, other)
	match, known = s.CandidateMatchesCombination(dir2, modelName, engineName)
	if !known || match {
		t.Fatal("different engine must not match", match, known)
	}
	if _, known = s.CandidateMatchesCombination(filepath.Join(w, "candidates", "missing"), modelName, engineName); known {
		t.Fatal("unreadable candidate must be unknown")
	}
	if _, known = s.CandidateMatchesCombination(dir, "no-such-model", engineName); known {
		t.Fatal("unresolvable name must be unknown")
	}
}

func writePreflight(t *testing.T, dir string, raw string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "device-preflight.json")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreflightModelPath(t *testing.T) {
	s, model, _ := candidateFixture(t)
	w := *s.settings.WorkspaceRoot
	recommended := fmt.Sprintf(`{"kind":"device_preflight.v1","recommendation":{"status":"recommended","reason":"","selected":{"model_path":%q,"mode":"metal","threads":4,"context_size":4096,"min_available_memory_bytes":1,"min_disk_bytes":1}}}`, model)
	path := writePreflight(t, filepath.Join(w, "preflight", "device-1"), recommended)
	got, e := s.PreflightModelPath(path)
	if e != nil || got != model {
		t.Fatalf("%q %v", got, e)
	}
	// A CommandResult-wrapped report reads through its details.
	wrapped := fmt.Sprintf(`{"command":"device-check","details":%s}`, recommended)
	path = writePreflight(t, filepath.Join(w, "preflight", "device-2"), wrapped)
	got, e = s.PreflightModelPath(path)
	if e != nil || got != model {
		t.Fatalf("%q %v", got, e)
	}
	blocked := `{"kind":"device_preflight.v1","recommendation":{"status":"blocked","reason":"insufficient_host_memory","selected":null}}`
	path = writePreflight(t, filepath.Join(w, "preflight", "device-3"), blocked)
	_, e = s.PreflightModelPath(path)
	if e == nil || e.Code != "device_preflight_blocked" || len(e.Limitations) != 1 || e.Limitations[0] != "insufficient_host_memory" {
		t.Fatal(e)
	}
	if _, e = s.PreflightModelPath("/etc/hostname"); e == nil || e.Code != "invalid_request" {
		t.Fatal(e)
	}
}

func TestPreflightMatchesModel(t *testing.T) {
	s, model, _ := candidateFixture(t)
	w := *s.settings.WorkspaceRoot
	data := s.registry()
	modelName := data.models[0].model.Name
	recommended := fmt.Sprintf(`{"kind":"device_preflight.v1","recommendation":{"status":"recommended","selected":{"model_path":%q,"mode":"metal","threads":4,"context_size":4096,"min_available_memory_bytes":1,"min_disk_bytes":1}}}`, model)
	path := writePreflight(t, filepath.Join(w, "preflight", "device-1"), recommended)
	match, e := s.PreflightMatchesModel(path, modelName)
	if e != nil || !match {
		t.Fatal(match, e)
	}
	other := filepath.Join(*s.settings.ModelsRoot, "other.gguf")
	if err := os.WriteFile(other, []byte("gguf"), 0600); err != nil {
		t.Fatal(err)
	}
	s.device.Snapshot = []byte(fmt.Sprintf(`{"models":[{"path":%q,"status":"available"},{"path":%q,"status":"available"}]}`, model, other))
	data = s.registry()
	if len(data.models) != 2 {
		t.Fatal(data.models)
	}
	match, e = s.PreflightMatchesModel(path, data.models[1].model.Name)
	if e != nil || match {
		t.Fatal("different model must not match", match, e)
	}
}
