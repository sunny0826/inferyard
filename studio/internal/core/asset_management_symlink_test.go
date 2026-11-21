package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestDeleteManagedAssetInternalAndEscapingSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlinks not used on Windows test fixture")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	outsideSecret := filepath.Join(base, "outside-secret.txt")
	mustMkdir(t, modelsRoot)
	mustWriteFile(t, outsideSecret, []byte("keep"), 0o644)

	safeEngineDir := filepath.Join(ws, "engines", "llama-safe")
	safeBin := filepath.Join(safeEngineDir, "bin", engineBinaryName())
	safeLibReal := filepath.Join(safeEngineDir, "bin", "libfoo.0.dylib")
	safeLibLink := filepath.Join(safeEngineDir, "bin", "libfoo.dylib")
	mustWriteFile(t, safeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	mustWriteFile(t, safeLibReal, []byte("lib"), 0o644)
	mustWriteFile(t, filepath.Join(safeEngineDir, "engine-prepare.json"), []byte(`{"out":"`+filepath.ToSlash(safeEngineDir)+`"}`), 0o644)
	if err := os.Symlink("libfoo.0.dylib", safeLibLink); err != nil {
		t.Fatal(err)
	}

	escapeEngineDir := filepath.Join(ws, "engines", "llama-escape")
	escapeBin := filepath.Join(escapeEngineDir, "bin", engineBinaryName())
	escapeLink := filepath.Join(escapeEngineDir, "bin", "escape.dylib")
	mustWriteFile(t, escapeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	mustWriteFile(t, filepath.Join(escapeEngineDir, "engine-prepare.json"), []byte(`{"out":"`+filepath.ToSlash(escapeEngineDir)+`"}`), 0o644)
	if err := os.Symlink(outsideSecret, escapeLink); err != nil {
		t.Fatal(err)
	}

	execWithEngines := func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{Stdout: []byte("inferyard 1.0 source=release\n")}, nil
		}
		if len(args) >= 2 && args[0] == "assets" && args[1] == "scan" {
			engines := []inferyard.ScannedEngine{}
			for _, bin := range []string{safeBin, escapeBin} {
				if info, err := os.Stat(bin); err == nil {
					engines = append(engines, inferyard.ScannedEngine{Path: bin, ModifiedNs: info.ModTime().UnixNano()})
				}
			}
			payload, _ := json.Marshal(map[string]any{
				"command": "assets scan", "status": "scanned", "completeness": "complete", "limitations": []string{},
				"details": inferyard.AssetScan{
					Definition: inferyard.AssetScanDefinition,
					Models:     []inferyard.ScannedModel{},
					Engines:    engines,
				},
			})
			return inferyard.Execution{Stdout: payload}, nil
		}
		return inferyard.Execution{}, nil
	}

	s := testService(t, execWithEngines)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	reg := s.Registry()
	var safeName, escapeName string
	for _, eng := range reg.Engines {
		switch eng.Path {
		case safeBin:
			safeName = eng.Name
		case escapeBin:
			escapeName = eng.Name
		}
	}
	if safeName == "" || escapeName == "" || safeName == escapeName {
		t.Fatalf("expected distinct scanned engines, got %+v", reg.Engines)
	}

	if err := s.DeleteManagedAsset("engine", safeName); err != nil {
		t.Fatalf("expected engine with safe internal symlink deleted, got %+v", err)
	}
	if _, err := os.Stat(safeEngineDir); !os.IsNotExist(err) {
		t.Fatalf("expected safeEngineDir removed, stat err=%v", err)
	}
	wait(t, func() bool { return !s.Registry().Refreshing })

	if err := s.DeleteManagedAsset("engine", escapeName); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable for escaping symlink, got %+v", err)
	}
	if _, err := os.Stat(escapeEngineDir); err != nil {
		t.Fatalf("escapeEngineDir must remain intact: %v", err)
	}
	if _, err := os.Stat(outsideSecret); err != nil {
		t.Fatalf("outsideSecret must remain intact: %v", err)
	}
}
