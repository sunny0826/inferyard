package core

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestPolystrataUninstallCaseInsensitiveCustomDataOrphanAndPartialRetry(t *testing.T) {
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	customDir := filepath.Join(modelsRoot, "personal-collection")
	mustMkdir(t, ws, dataDir, customDir)

	psDir := filepath.Join(ws, "engines", "polystrata-edge")
	psInst := setupPolystrataInstallFixture(t, psDir, dataDir, "v0.1.45-edge", true)

	// 1. Custom data_dir under ModelsRoot is NOT treated as managed deletion scope.
	customGGUF := filepath.Join(customDir, "user-keep.gguf")
	mustWriteFile(t, customGGUF, bytes.Repeat([]byte("u"), 512), 0o600)
	customInst := setupPolystrataInstallFixture(t, filepath.Join(ws, "engines", "polystrata-custom"), customDir, "v0.1.45-cust", true)
	cfgCustomRef := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-custom-ref.json"), "Custom Data Ref", []string{"--model", customGGUF})
	cfgInCustom := writeStrataConfigFixture(t, filepath.Join(customDir, "strata-in-custom.json"), "Inside Custom", []string{"--model", customGGUF})

	// 2. Case-insensitive shared GGUF check (only test same-file aliasing if OS creates case-aliased files).
	upperGGUF := filepath.Join(dataDir, "models", "case", "Weights.gguf")
	lowerGGUF := filepath.Join(dataDir, "models", "case", "weights.gguf")
	mustWriteFile(t, upperGGUF, bytes.Repeat([]byte("c"), 1024), 0o600)
	cfgCaseA := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "ca", "strata-case-a.json"), "Case A", []string{"--model", upperGGUF})
	cfgCaseB := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "cb", "strata-case-b.json"), "Case B", []string{"--model", lowerGGUF})

	// 3. Broken split-GGUF where shard 1 is missing but orphan shard 2 remains on disk.
	orphanShard1 := filepath.Join(dataDir, "models", "broken-split", "m-00001-of-00002.gguf")
	orphanShard2 := filepath.Join(dataDir, "models", "broken-split", "m-00002-of-00002.gguf")
	mustWriteFile(t, orphanShard2, bytes.Repeat([]byte("2"), 400), 0o600)
	cfgBrokenSplit := writeStrataConfigFixture(t, filepath.Join(psDir, "strata-broken-split.json"), "Broken Split", []string{"--model", orphanShard1})

	// 4. Retained strata_pack inside default polystrata-data + standard GGUF folder inspect alignment.
	retPack := filepath.Join(dataDir, "packs", "retained")
	mustWriteFile(t, filepath.Join(retPack, "weights.bin"), bytes.Repeat([]byte("r"), 700), 0o600)
	cfgRetained := writeStrataConfigFixture(t, filepath.Join(dataDir, "models", "ret", "strata-retained.json"), "Retained Pack", []string{"--pack", retPack})

	stdDir := filepath.Join(modelsRoot, "std-single")
	stdGGUF := filepath.Join(stdDir, "single.gguf")
	mustWriteFile(t, stdGGUF, bytes.Repeat([]byte("g"), 256), 0o600)
	mustWriteFile(t, filepath.Join(stdDir, "source.json"), []byte("{}"), 0o600)

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{psInst, customInst}
	s.assets.Parsed = &inferyard.AssetScan{
		Definition: inferyard.AssetScanDefinition,
		Models:     []inferyard.ScannedModel{{Path: stdGGUF, Name: "single", Format: "gguf", SizeBytes: ptr(int64(256)), Available: true}},
		Engines:    []inferyard.ScannedEngine{},
	}
	s.mu.Unlock()

	stdInfo, err := s.InspectDeleteAsset("model", "single.gguf")
	if err != nil || stdInfo.TargetPath != stdDir || stdInfo.ReclaimableBytes == nil || *stdInfo.ReclaimableBytes <= 256 {
		t.Fatalf("expected standard single-GGUF inspect to target folder %q, got %+v err=%+v", stdDir, stdInfo, err)
	}

	findModel := func(cfgPath string) (ModelAsset, bool) {
		for _, m := range s.Registry().Models {
			if m.Path == cfgPath {
				return m, true
			}
		}
		return ModelAsset{}, false
	}

	if mCust, ok := findModel(cfgInCustom); ok && mCust.Deletable {
		t.Fatalf("config inside custom user data_dir must not be Deletable: %+v", mCust)
	}
	mRef, ok := findModel(cfgCustomRef)
	if !ok {
		t.Fatalf("missing cfgCustomRef")
	}
	refInfo, err := s.InspectDeleteAsset("model", mRef.Name)
	if err != nil || refInfo.Scope != "polystrata_model_config_only" || !refInfo.PreservedExternal {
		t.Fatalf("expected custom data_dir weights preserved as external, got %+v err=%+v", refInfo, err)
	}
	if err := s.DeleteManagedAsset("model", mRef.Name); err != nil {
		t.Fatalf("delete cfgCustomRef failed: %+v", err)
	}
	if _, statErr := os.Stat(customGGUF); statErr != nil {
		t.Fatalf("user file in custom data_dir must stay intact: %v", statErr)
	}

	// Case-insensitive shared weight check
	infoUpper, errU := os.Lstat(upperGGUF)
	infoLower, errL := os.Lstat(lowerGGUF)
	if errU == nil && errL == nil && os.SameFile(infoUpper, infoLower) {
		mA, ok := findModel(cfgCaseA)
		if !ok {
			t.Fatalf("missing cfgCaseA")
		}
		if err := s.DeleteManagedAsset("model", mA.Name); err != nil {
			t.Fatalf("delete cfgCaseA failed: %+v", err)
		}
		if _, statErr := os.Stat(lowerGGUF); statErr != nil {
			t.Fatalf("case-aliased shared GGUF must remain for Case B: %v", statErr)
		}
		mB, ok := findModel(cfgCaseB)
		if !ok || !mB.Available {
			t.Fatalf("Case B must remain available: ok=%v asset=%+v", ok, mB)
		}
	}

	// Broken split-GGUF removes orphan shard 2
	mBrokenSplit, ok := findModel(cfgBrokenSplit)
	if !ok {
		t.Fatalf("missing cfgBrokenSplit")
	}
	if err := s.DeleteManagedAsset("model", mBrokenSplit.Name); err != nil {
		t.Fatalf("delete cfgBrokenSplit failed: %+v", err)
	}
	if _, statErr := os.Stat(orphanShard2); !os.IsNotExist(statErr) {
		t.Fatalf("expected orphaned split shard2 removed, stat err=%v", statErr)
	}

	// Orphan service blocks deletion with workspace_busy
	s.mu.Lock()
	s.orphan = &orphanEngine{state: EngineState{State: "orphaned", Pid: 99999}}
	s.mu.Unlock()
	if err := s.DeleteManagedAsset("model", "Retained Pack"); err == nil || err.Details["reason"] != "workspace_busy" {
		t.Fatalf("expected workspace_busy when orphan service active, got %+v", err)
	}
	s.mu.Lock()
	s.orphan = nil
	s.mu.Unlock()

	// Partial uninstall failure due to read-only subdir preserves installation.json & retryability
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		roDir := filepath.Join(psDir, "locked-sub")
		roFile := filepath.Join(roDir, "locked.txt")
		mustWriteFile(t, roFile, []byte("x"), 0o600)
		if err := os.Chmod(roDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

		var psName string
		for _, e := range s.Registry().Engines {
			if e.Path == psInst {
				psName = e.Name
				break
			}
		}
		if err := s.DeleteManagedAsset("engine", psName); err == nil || err.Code != "internal_error" {
			t.Fatalf("expected internal_error on read-only subdir, got %+v", err)
		}
		if _, statErr := os.Stat(psInst); statErr != nil {
			t.Fatalf("failed uninstall must restore install dir and installation.json for retry: %v", statErr)
		}
		if !slices.Contains(s.Settings().Settings.EngineInstallations, psInst) {
			t.Fatalf("failed uninstall must keep EngineInstallations entry: %+v", s.Settings().Settings.EngineInstallations)
		}
		_ = os.Chmod(roDir, 0o755)
		var retryEngine EngineAsset
		for _, e := range s.Registry().Engines {
			if e.Path == psInst {
				retryEngine = e
				break
			}
		}
		if retryEngine.Name == "" || !retryEngine.Deletable {
			t.Fatalf("failed uninstall must stay registered and Deletable in Registry, got %+v", retryEngine)
		}
		if err := s.DeleteManagedAsset("engine", retryEngine.Name); err != nil {
			t.Fatalf("retry uninstall after fixing permissions failed: %+v", err)
		}
	}

	// Uninstall remaining engine so NO PolyStrata installations remain registered.
	for _, e := range s.Registry().Engines {
		if e.Family == "polystrata" && e.Deletable {
			if err := s.DeleteManagedAsset("engine", e.Name); err != nil {
				t.Fatalf("uninstall engine %s failed: %+v", e.Name, err)
			}
		}
	}
	wait(t, func() bool { return !s.Registry().Refreshing })
	if len(s.Settings().Settings.EngineInstallations) != 0 {
		t.Fatalf("expected all installations removed: %+v", s.Settings().Settings.EngineInstallations)
	}

	// Retained strata_pack inside polystrata-data MUST remain visible & Deletable even with 0 engines!
	retModel, ok := findModel(cfgRetained)
	if !ok || !retModel.Deletable {
		t.Fatalf("retained polystrata-data pack must stay visible and Deletable after uninstalling last engine: ok=%v m=%+v", ok, retModel)
	}
	if err := s.DeleteManagedAsset("model", retModel.Name); err != nil {
		t.Fatalf("delete retained pack after engine uninstall failed: %+v", err)
	}
	if _, statErr := os.Stat(retPack); !os.IsNotExist(statErr) {
		t.Fatalf("expected retained pack weights removed, stat err=%v", statErr)
	}
}

func TestPolystrataUninstallRejectsBadVenvSymlinkNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlinks not used on Windows test fixture")
	}
	_, base := isolatedHomeAndBase(t)
	ws := filepath.Join(base, "ws")
	modelsRoot := filepath.Join(base, "models")
	dataDir := filepath.Join(modelsRoot, "polystrata-data")
	outside := filepath.Join(base, "secret.txt")
	mustWriteFile(t, outside, []byte("keep"), 0o600)

	dirEvilName := filepath.Join(ws, "engines", "ps-evil")
	instEvil := setupPolystrataInstallFixture(t, dirEvilName, dataDir, "v0.1.45-e1", true)
	mustWriteFile(t, filepath.Join(dirEvilName, ".venv", "pyvenv.cfg"), []byte("home = /tmp\n"), 0o600)
	mustMkdir(t, filepath.Join(dirEvilName, ".venv", "bin"))
	if err := os.Symlink(outside, filepath.Join(dirEvilName, ".venv", "bin", "python-evil")); err != nil {
		t.Fatal(err)
	}

	dirMarkerLink := filepath.Join(ws, "engines", "ps-marker")
	instMarker := setupPolystrataInstallFixture(t, dirMarkerLink, dataDir, "v0.1.45-e2", true)
	if err := os.Symlink("setup.py", filepath.Join(dirMarkerLink, "manifest.json")); err != nil {
		t.Fatal(err)
	}

	s := testService(t, assetTestExec)
	s.mu.Lock()
	s.settings.WorkspaceRoot = &ws
	s.settings.ModelsRoot = &modelsRoot
	s.settings.EngineInstallations = []string{instEvil, instMarker}
	s.mu.Unlock()

	for _, e := range s.Registry().Engines {
		if err := s.DeleteManagedAsset("engine", e.Name); err == nil || err.Code != "asset_not_deletable" {
			t.Fatalf("expected asset_not_deletable for %s (%s), got %+v", e.Name, e.Path, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside secret must remain intact: %v", err)
	}
}
