package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSGLangEachCandidateGetsNewMetricsWithoutChangingSource(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "installation.json")
	raw := []byte(`{"family":"sglang","working_directory":"/original","environment":{"SGLANG_USE_MLX":"1","SGLANG_RUN_ID":"original","PROMETHEUS_MULTIPROC_DIR":"/original/metrics"},"worker_environment":{"SGLANG_USE_MLX":"1","SGLANG_RUN_ID":"original","PROMETHEUS_MULTIPROC_DIR":"/original/metrics"}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	key := installationKey(path)
	previous := ""
	for range 2 {
		derived, failure := scopedSGLangInstallation(workspace, path, key)
		if failure != nil {
			t.Fatal(failure)
		}
		content, err := os.ReadFile(derived)
		if err != nil {
			t.Fatal(err)
		}
		var d struct {
			WorkingDirectory  string            `json:"working_directory"`
			Environment       map[string]string `json:"environment"`
			WorkerEnvironment map[string]string `json:"worker_environment"`
		}
		if json.Unmarshal(content, &d) != nil {
			t.Fatal("invalid derived declaration")
		}
		metrics := d.Environment["PROMETHEUS_MULTIPROC_DIR"]
		if metrics == previous || metrics != filepath.Join(d.WorkingDirectory, "metrics") || d.Environment["SGLANG_RUN_ID"] != d.WorkerEnvironment["SGLANG_RUN_ID"] || metrics != d.WorkerEnvironment["PROMETHEUS_MULTIPROC_DIR"] {
			t.Fatalf("%+v", d)
		}
		entries, err := os.ReadDir(metrics)
		if err != nil || len(entries) != 0 {
			t.Fatal(entries, err)
		}
		previous = metrics
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("source installation was changed")
	}
	if err := os.WriteFile(path, append(raw, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, failure := scopedSGLangInstallation(workspace, path, key); failure == nil || failure.Code != "local_engine_installation_changed" {
		t.Fatal(failure)
	}
}

func TestLocalContextSpeedStopsBeforePreparation(t *testing.T) {
	s := registryService(t)
	path := writeInstallation(t, t.TempDir(), "installation.json")
	s.settings.EngineInstallations = []string{path}
	name := s.Registry().Engines[0].Name
	if _, failure := s.StartContextSpeed(StartContextSpeedRequest{Model: "directory-model", Engine: name, Context: 65536}); failure == nil || failure.Code != "local_engine_context_speed_unsupported" {
		t.Fatal(failure)
	}
	if len(s.RunTasks()) != 0 {
		t.Fatal("unsupported specialist task was queued")
	}
	if _, err := os.Stat(filepath.Join(*s.settings.WorkspaceRoot, "context-speed-operations")); !os.IsNotExist(err) {
		t.Fatal("unsupported task wrote operations", err)
	}
}
