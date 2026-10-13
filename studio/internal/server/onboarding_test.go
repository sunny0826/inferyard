package server

import (
	"context"
	"encoding/json"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"testing"
)

func TestReadinessProvisionAndSoftGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s, ts := setup(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		return inferyard.Execution{Stdout: []byte("inferyard fixture"), ExitCode: 0}, nil
	})
	code, raw := request(t, s, ts, "GET", "/api/readiness", "")
	var ready core.Readiness
	if code != 200 || json.Unmarshal(raw, &ready) != nil || !ready.FirstRun || ready.Workspace.Initialized || *ready.Workspace.Path != filepath.Join(home, ".inferyard") {
		t.Fatalf("%d %s", code, raw)
	}
	root := filepath.Join(home, "chosen")
	body, _ := json.Marshal(map[string]string{"workspace_root": root, "inferyard_path": filepath.Join(home, "missing-cli")})
	code, raw = request(t, s, ts, "PUT", "/api/settings", string(body))
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("PUT created directory")
	}
	for _, route := range []string{"/api/device", "/api/settings", "/api/readiness", "/"} {
		code, _ := request(t, s, ts, "GET", route, "")
		if code != 200 {
			t.Fatalf("soft gate blocks %s", route)
		}
	}
	_, unsubscribe, last := s.Core.Bus.Subscribe()
	defer unsubscribe()
	for i := 0; i < 2; i++ {
		code, raw = request(t, s, ts, "POST", "/api/settings/provision", "")
		if code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
		for _, dir := range []string{root, filepath.Join(root, "results"), filepath.Join(root, "models")} {
			st, err := os.Stat(dir)
			if err != nil || !st.IsDir() {
				t.Fatal(dir, err)
			}
		}
		if i == 0 {
			if err := os.WriteFile(filepath.Join(root, "keep"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(root, "keep")); err != nil || string(raw) != "original" {
		t.Fatal("provision overwrote file")
	}
	events, _, _ := s.Core.Bus.After(last)
	found := false
	for _, event := range events {
		if event.Type == "sync.reset" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing reset")
	}
	code, raw = request(t, s, ts, "GET", "/api/readiness", "")
	json.Unmarshal(raw, &ready)
	if code != 200 || ready.Workspace.Initialized || !ready.Workspace.Configured || ready.CLI.Status != "not_found" {
		t.Fatalf("%s", raw)
	}
	if err := os.WriteFile(filepath.Join(root, "preparation.json"), []byte(`{"kind":"community_init.v1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, raw = request(t, s, ts, "GET", "/api/readiness", "")
	json.Unmarshal(raw, &ready)
	if !ready.Workspace.Initialized {
		t.Fatalf("%s", raw)
	}
}
func TestWizardRejectsUnsafeCommands(t *testing.T) {
	calls := 0
	s, ts := setup(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		calls++
		return inferyard.Execution{}, nil
	})
	for _, command := range []string{"sh -c true", "inferyard service start", "inferyard model acquire --source https://user:secret@example.org/model --out /tmp/a", "inferyard model acquire --source https://example.org/m?token=secret --out /tmp/a", "inferyard model acquire --token secret", "inferyard model acquire --token-env 'NOT AN ENV'", "inferyard run --config /tmp/a; touch /tmp/b", "inferyard config bind --pid 2 --pid 3", "inferyard engine acquire --token-env TOKEN"} {
		body, _ := json.Marshal(core.WizardRequest{Command: command})
		code, raw := request(t, s, ts, "POST", "/api/onboarding/execute", string(body))
		if code != 400 {
			t.Fatalf("%s: %d %s", command, code, raw)
		}
	}
	if calls != 0 {
		t.Fatal("unsafe command reached CLI")
	}
}
