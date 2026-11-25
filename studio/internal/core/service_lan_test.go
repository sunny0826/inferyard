package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLANAccessDefaultLoopbackAndExplicitWildcard(t *testing.T) {
	s, workspace, engine := setupMultiServiceTestEnv(t, map[string]int64{
		"model-local.gguf": 4,
		"model-lan.gguf":   4,
	}, nil)

	// ExtraArgs trying to bind 0.0.0.0 while LANAccess=false must be rejected.
	if _, err := s.StartStandaloneEngine(StandaloneStartRequest{
		Model:       "model-local.gguf",
		Engine:      engine,
		ContextSize: 4096,
		LANAccess:   false,
		ExtraArgs:   []string{"--host", "0.0.0.0"},
	}); err == nil || err.Code != "engine_host_not_loopback" {
		t.Fatalf("expected engine_host_not_loopback when LANAccess=false, got %+v", err)
	}

	// Default LANAccess=false binds 127.0.0.1.
	stLocal, err := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-local.gguf", Engine: engine, ContextSize: 4096,
	})
	if err != nil {
		t.Fatalf("start local service failed: %+v", err)
	}
	stLocal = waitManagedServiceState(t, s, stLocal.ServiceID, "running")
	if stLocal.LANAccess || !strings.HasPrefix(stLocal.Endpoint, "http://127.0.0.1:") {
		t.Fatalf("expected loopback endpoint for LANAccess=false, got %+v", stLocal)
	}

	// Explicit LANAccess=true binds 0.0.0.0, persists to preset and owner, and passes local health check.
	stLAN, err := s.StartStandaloneEngine(StandaloneStartRequest{
		Model: "model-lan.gguf", Engine: engine, ContextSize: 4096, LANAccess: true,
	})
	if err != nil {
		t.Fatalf("start LAN service failed: %+v", err)
	}
	stLAN = waitManagedServiceState(t, s, stLAN.ServiceID, "running")
	if !stLAN.LANAccess || !strings.HasPrefix(stLAN.Endpoint, "http://0.0.0.0:") {
		t.Fatalf("expected 0.0.0.0 endpoint for LANAccess=true, got %+v", stLAN)
	}
	preset, ok := s.ServicePresetFor("model-lan.gguf", engine)
	if !ok || !preset.LANAccess {
		t.Fatalf("expected LANAccess=true persisted in ServicePreset, got %+v ok=%v", preset, ok)
	}
	rawOwner, readErr := os.ReadFile(serviceOwnerPath(workspace, stLAN.ServiceID))
	if readErr != nil {
		t.Fatalf("read LAN owner file: %v", readErr)
	}
	var owner engineOwner
	if unmarshalErr := json.Unmarshal(rawOwner, &owner); unmarshalErr != nil || !owner.LANAccess {
		t.Fatalf("expected LANAccess=true in owner file, got %+v err=%v", owner, unmarshalErr)
	}

	// PolyStrata config copy does not mutate original template.
	tmpDir := t.TempDir()
	origCfg := filepath.Join(tmpDir, "template.json")
	if writeErr := os.WriteFile(origCfg, []byte(`{"host":"127.0.0.1","port":8080,"model_name":"demo"}`), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	updated, lanErr := applyLANAccess(Startup{
		Family:   "polystrata",
		Endpoint: "http://127.0.0.1:8080",
		Args:     []string{"--config", origCfg},
	}, tmpDir, true)
	if lanErr != nil {
		t.Fatalf("applyLANAccess polystrata failed: %+v", lanErr)
	}
	origBytes, _ := os.ReadFile(origCfg)
	if !strings.Contains(string(origBytes), `"127.0.0.1"`) {
		t.Fatalf("original polystrata config was mutated: %s", string(origBytes))
	}
	lanCfgPath, _, _ := findConfigArg(updated.Args)
	if lanCfgPath == origCfg || lanCfgPath == "" {
		t.Fatalf("expected copied LAN config path, got %q", lanCfgPath)
	}
	lanBytes, _ := os.ReadFile(lanCfgPath)
	if !strings.Contains(string(lanBytes), `"0.0.0.0"`) {
		t.Fatalf("expected copied polystrata config host 0.0.0.0, got %s", string(lanBytes))
	}
}
