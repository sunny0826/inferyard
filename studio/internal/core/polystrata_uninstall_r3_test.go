package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func writeDirModelFixture(t *testing.T, dir string, shardName string, size int) {
	t.Helper()
	mustWriteFile(t, filepath.Join(dir, "config.json"), []byte(`{"architectures":["Qwen2ForCausalLM"]}`), 0o600)
	mustWriteFile(t, filepath.Join(dir, "tokenizer_config.json"), []byte(`{"chat_template":"ok"}`), 0o600)
	mustWriteFile(t, filepath.Join(dir, shardName), bytes.Repeat([]byte("s"), size), 0o600)
}

func TestPolystrataUninstallProtectsDataDirAndAncestorsAsDirectoryModels(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	extraRoot := filepath.Join(base, "extra-root")
	bundleDir := filepath.Join(extraRoot, "bundle")
	nestedDD := filepath.Join(bundleDir, "nested-ps-data")
	mustMkdir(t, ws, dataDir, nestedDD)

	// Case 1: polystrata-data itself resembles a standard HF directory model alongside a strata_pack.
	writeDirModelFixture(t, dataDir, "model.safetensors", 512)
	packFile := filepath.Join(dataDir, "packs", "p1", "weights.bin")
	mustWriteFile(t, packFile, bytes.Repeat([]byte("w"), 512), 0o600)
	cfgP1 := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "p1", "strata-p1.json"), "Pack One", []string{"--pack", filepath.Dir(packFile)})

	// Case 2: extraRoot/bundle is scanned as a directory model while enclosing nested-ps-data.
	writeDirModelFixture(t, bundleDir, "bundle.safetensors", 256)
	psDir := filepath.Join(ws, "engines", "ps-nested")
	psInst := setupPolystrataInstallFixture(t, psDir, nestedDD, "v0.1.45-nest", true)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.ExtraModelsRoots = []string{extraRoot}
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	reg := s.Registry()
	for _, wantProtected := range []string{dataDir, bundleDir} {
		canonWant, err := canonical(wantProtected)
		if err != nil {
			canonWant = wantProtected
		}
		var found *ModelAsset
		for i := range reg.Models {
			m := &reg.Models[i]
			if m.Format == "directory" && sameFileOrPath(m.Path, canonWant) {
				found = m
				break
			}
		}
		if found == nil {
			t.Fatalf("expected %s to appear as scanned directory model in Registry: %+v", wantProtected, reg.Models)
		}
		if found.Deletable {
			t.Fatalf("protected PolyStrata data root / ancestor %s must have Deletable=false: %+v", wantProtected, *found)
		}
		if _, err := s.InspectDeleteAsset("model", found.Name); err == nil || err.Code != "asset_not_deletable" {
			t.Fatalf("expected InspectDeleteAsset(%s) asset_not_deletable, got %+v", found.Name, err)
		}
		if err := s.DeleteManagedAsset("model", found.Name); err == nil || err.Code != "asset_not_deletable" {
			t.Fatalf("expected DeleteManagedAsset(%s) asset_not_deletable, got %+v", found.Name, err)
		}
	}
	for _, survive := range []string{packFile, cfgP1, nestedDD} {
		if _, err := os.Stat(survive); err != nil {
			t.Fatalf("protected file/dir %s must remain intact: %v", survive, err)
		}
	}
}

func TestPolystrataUninstallAtomicStageRollbackOnReadOnlyConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0555 permission enforcement requires non-root POSIX environment")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	cfgDir := filepath.Join(dataDir, "models", "m-ro")
	packDir := filepath.Join(dataDir, "packs", "p-rw")
	mustMkdir(t, ws, cfgDir, packDir)

	weightFile := filepath.Join(packDir, "weights.bin")
	mustWriteFile(t, weightFile, bytes.Repeat([]byte("r"), 1024), 0o600)
	cfgPath := writeStrataConfigFixture(t, filepath.Join(cfgDir, "strata-ro.json"), "RO Config", []string{"--pack", packDir})

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	if err := os.Chmod(cfgDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o755) })

	if err := s.DeleteManagedAsset("model", "RO Config"); err == nil || err.Code != "internal_error" {
		t.Fatalf("expected internal_error deleting model with read-only config dir, got %+v", err)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config must remain intact on rollback: %v", err)
	}
	if _, err := os.Stat(weightFile); err != nil {
		t.Fatalf("pack weights must NOT be lost when config dir is read-only: %v", err)
	}

	_ = os.Chmod(cfgDir, 0o755)
	if err := s.DeleteManagedAsset("model", "RO Config"); err != nil {
		t.Fatalf("retry deletion after restoring permissions failed: %+v", err)
	}
	if _, err := os.Stat(weightFile); !os.IsNotExist(err) {
		t.Fatalf("expected weightFile removed on retry, stat err=%v", err)
	}
}

func TestPolystrataUninstallPostCommitCleanupFailureDoesNotReregisterBrokenEngine(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-locked")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-locked", true)

	origRemove := removeStagedPolystrataDir
	t.Cleanup(func() { removeStagedPolystrataDir = origRemove })
	stagedObserved := ""
	removeStagedPolystrataDir = func(dir string) error {
		stagedObserved = dir
		_ = os.RemoveAll(filepath.Join(dir, "serve"))
		return os.ErrPermission
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if err := s.DeleteManagedAsset("engine", engName); err == nil || err.Code != "internal_error" {
		t.Fatalf("expected internal_error on post-commit cleanup failure, got %+v", err)
	}
	if len(s.Settings().Settings.EngineInstallations) != 0 {
		t.Fatalf("must NOT re-register partially removed engine: %+v", s.Settings().Settings.EngineInstallations)
	}
	if _, err := os.Stat(psDir); !os.IsNotExist(err) {
		t.Fatalf("broken installDir must remain unregistered in staging dir, stat err=%v", err)
	}
	if stagedObserved == "" || !strings.HasPrefix(filepath.Base(stagedObserved), ".inferyard-uninstall-") {
		t.Fatalf("expected staged directory path observed, got %q", stagedObserved)
	}
	if _, err := os.Stat(stagedObserved); err != nil {
		t.Fatalf("leftover staging dir must remain for manual cleanup: %v", err)
	}
	if len(s.Registry().Engines) != 0 {
		t.Fatalf("broken partial engine must not appear in Registry().Engines: %+v", s.Registry().Engines)
	}
}

func TestPolystrataUninstallMigrationReadOnlyDataDirAndRelativePack(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0555 permission enforcement requires non-root POSIX environment")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-mig")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-mig", true)
	relPack := filepath.Join(dataDir, "packs", "rel-pack")
	mustWriteFile(t, filepath.Join(relPack, "weights.bin"), bytes.Repeat([]byte("m"), 600), 0o600)
	rootCfgPath := filepath.Join(psDir, "strata-rel.json")
	cfgBytes, _ := json.Marshal(map[string]any{
		"exe":        filepath.Join(psDir, ".venv", "bin", "python"),
		"cwd":        psDir,
		"args":       []string{"--pack", "../../models/polystrata-data/packs/rel-pack"},
		"model_name": "Rel Migrated Pack",
	})
	mustWriteFile(t, rootCfgPath, cfgBytes, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst}
	s.mu.Unlock()

	engName := s.Registry().Engines[0].Name
	if err := os.Chmod(dataDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o755) })

	if err := s.DeleteManagedAsset("engine", engName); err == nil || err.Code != "internal_error" {
		t.Fatalf("expected internal_error when root config migration cannot write to read-only dataDir, got %+v", err)
	}
	if !slices.Contains(s.Settings().Settings.EngineInstallations, psInst) {
		t.Fatalf("failed migration must keep engine registered: %+v", s.Settings().Settings.EngineInstallations)
	}
	if _, err := os.Stat(rootCfgPath); err != nil {
		t.Fatalf("failed migration must preserve root strata config: %v", err)
	}

	_ = os.Chmod(dataDir, 0o755)
	if err := s.DeleteManagedAsset("engine", engName); err != nil {
		t.Fatalf("uninstall with writable dataDir failed: %+v", err)
	}
	var migrated *ModelAsset
	for i := range s.Registry().Models {
		m := &s.Registry().Models[i]
		if m.Name == "Rel Migrated Pack" {
			migrated = m
			break
		}
	}
	if migrated == nil || !migrated.Available || !migrated.Deletable || migrated.SizeBytes == nil || *migrated.SizeBytes != 600 {
		t.Fatalf("expected migrated relative pack model to stay Available+Deletable with 600B: %+v", migrated)
	}
	if err := s.DeleteManagedAsset("model", migrated.Name); err != nil {
		t.Fatalf("delete migrated model failed: %+v", err)
	}
	if _, err := os.Stat(relPack); !os.IsNotExist(err) {
		t.Fatalf("expected migrated relative pack weights deleted cleanly, stat err=%v", err)
	}
}

func TestPolystrataUninstallBlockedByExternalLMStudioModelsRoot(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	psDir := filepath.Join(ws, "engines", "ps-lms")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-lms", true)
	lmsModels := filepath.Join(psDir, "shared-lms-models")
	mustWriteFile(t, filepath.Join(lmsModels, "keep.gguf"), []byte("k"), 0o600)
	fakeLmsExe := filepath.Join(base, "lms", "lms")
	mustWriteFile(t, fakeLmsExe, []byte("#!/bin/sh\n"), 0o755)
	lmsInst := filepath.Join(base, "lms", "installation.json")
	lmsPayload, _ := json.Marshal(map[string]any{
		"schema_version": 3, "definition": "local-engine-installation.v1",
		"family": "lmstudio", "executable": fakeLmsExe, "models_root": lmsModels,
	})
	mustWriteFile(t, lmsInst, lmsPayload, 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst, lmsInst}
	s.mu.Unlock()

	var psEng EngineAsset
	for _, e := range s.Registry().Engines {
		if e.Path == psInst {
			psEng = e
			break
		}
	}
	if psEng.Name == "" || psEng.Deletable {
		t.Fatalf("PolyStrata engine owning external LM Studio models_root must not be Deletable: %+v", psEng)
	}
	if _, err := s.InspectDeleteAsset("engine", psEng.Name); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable from InspectDeleteAsset, got %+v", err)
	}
	if err := s.DeleteManagedAsset("engine", psEng.Name); err == nil || err.Code != "asset_not_deletable" {
		t.Fatalf("expected asset_not_deletable from DeleteManagedAsset, got %+v", err)
	}
	if _, err := os.Stat(filepath.Join(lmsModels, "keep.gguf")); err != nil {
		t.Fatalf("external LM Studio models_root must remain intact: %v", err)
	}
}

func TestPolystrataUninstallFingerprintsDetectMemberSwapAndExcludeExternalHardlinks(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	mustMkdir(t, ws, dataDir)

	swapPack := filepath.Join(dataDir, "packs", "swap-pack")
	mustWriteFile(t, filepath.Join(swapPack, "weights-a.bin"), bytes.Repeat([]byte("a"), 512), 0o600)
	_ = writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "swap", "strata-swap.json"), "Swap Pack", []string{"--pack", swapPack})

	stdDir := filepath.Join(modelsRoot, "std-swap")
	writeDirModelFixture(t, stdDir, "shard-a.safetensors", 512)

	hlPack := filepath.Join(dataDir, "packs", "hl-reclaim")
	hlInner := filepath.Join(hlPack, "weights.bin")
	hlOuter := filepath.Join(base, "external-keep.bin")
	oneMiB := bytes.Repeat([]byte("z"), 1<<20)
	mustWriteFile(t, hlInner, oneMiB, 0o600)
	cfgHL := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "hlr", "strata-hl-reclaim.json"), "HL Reclaim", []string{"--pack", hlPack})

	if runtime.GOOS != "windows" {
		if err := os.Link(hlInner, hlOuter); err != nil {
			t.Fatal(err)
		}
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.mu.Unlock()

	// 1. Swapping a pack member file for one of identical byte size invalidates DeleteManagedAssetVerified.
	swapInfo, err := s.InspectDeleteAsset("model", "Swap Pack")
	if err != nil {
		t.Fatal(err)
	}
	dirStatBefore, _ := os.Lstat(swapPack)
	if rerr := os.Rename(filepath.Join(swapPack, "weights-a.bin"), filepath.Join(swapPack, "weights-b.bin")); rerr != nil {
		t.Fatal(rerr)
	}
	_ = os.Chtimes(swapPack, dirStatBefore.ModTime(), dirStatBefore.ModTime())
	if verr := s.DeleteManagedAssetVerified("model", "Swap Pack", swapInfo.TargetPath, swapInfo.Fingerprint); verr == nil || verr.Code != "conflict" {
		t.Fatalf("expected delete_target_changed after pack member filename swap, got %+v", verr)
	}
	if _, statErr := os.Stat(filepath.Join(swapPack, "weights-b.bin")); statErr != nil {
		t.Fatalf("renamed pack member must survive rejected delete: %v", statErr)
	}

	// Same check on standard directory model.
	stdInfo, err := s.InspectDeleteAsset("model", "std-swap")
	if err != nil {
		t.Fatal(err)
	}
	stdStatBefore, _ := os.Lstat(stdDir)
	if rerr := os.Rename(filepath.Join(stdDir, "shard-a.safetensors"), filepath.Join(stdDir, "shard-b.safetensors")); rerr != nil {
		t.Fatal(rerr)
	}
	_ = os.Chtimes(stdDir, stdStatBefore.ModTime(), stdStatBefore.ModTime())
	if verr := s.DeleteManagedAssetVerified("model", "std-swap", stdInfo.TargetPath, stdInfo.Fingerprint); verr == nil || verr.Code != "conflict" {
		t.Fatalf("expected delete_target_changed after directory member filename swap, got %+v", verr)
	}
	if _, statErr := os.Stat(filepath.Join(stdDir, "shard-b.safetensors")); statErr != nil {
		t.Fatalf("renamed directory member must survive rejected delete: %v", statErr)
	}

	// 2. External hardlink excludes shared file bytes from ReclaimableBytes.
	if runtime.GOOS != "windows" {
		cfgInfo, statErr := os.Lstat(cfgHL)
		if statErr != nil {
			t.Fatal(statErr)
		}
		hlInfo, err := s.InspectDeleteAsset("model", "HL Reclaim")
		if err != nil {
			t.Fatal(err)
		}
		if hlInfo.ReclaimableBytes == nil || *hlInfo.ReclaimableBytes != cfgInfo.Size() {
			t.Fatalf("expected ReclaimableBytes=%d (excluding 1 MiB external hardlink), got %+v", cfgInfo.Size(), hlInfo)
		}
		if err := s.DeleteManagedAssetVerified("model", "HL Reclaim", hlInfo.TargetPath, hlInfo.Fingerprint); err != nil {
			t.Fatalf("delete HL Reclaim failed: %+v", err)
		}
		if got, readErr := os.ReadFile(hlOuter); readErr != nil || len(got) != len(oneMiB) {
			t.Fatalf("external hardlink must remain intact after pack deletion: len=%d err=%v", len(got), readErr)
		}
	}
}
