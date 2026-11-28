package e2e

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const minBonsaiPrismBuild = 12232

var (
	bonsaiPrismTagRe    = regexp.MustCompile(`(?i)\bprism-b(\d+)-[0-9a-f]{7,40}\b`)
	bonsaiPrismBannerRe = regexp.MustCompile(`(?i)\(\s*build\s+(\d+)\s*,\s*commit\s+[0-9a-f]{7,40}\s*\)`)
)

type bonsaiModelSource struct {
	Dir         string
	GGUFPath    string
	Filename    string
	SourceJSON  string
	Bytes       int64
	IsLooseFile bool
}

type stagedBonsai struct {
	EngineDir        string
	EngineBin        string
	ChosenModelPath  string
	ChosenModelName  string
	ChosenSourceJSON string
	AllModels        []string
}

func llamaServerBinaryName() string {
	if runtime.GOOS == "windows" {
		return "llama-server.exe"
	}
	return "llama-server"
}

func isBonsaiGGUFFilename(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".gguf") &&
		strings.Contains(lower, "bonsai") &&
		(strings.Contains(lower, "ptq1_0") || strings.Contains(lower, "pq2_0"))
}

func parsePrismBuildFromReceipts(roots ...string) (int, bool) {
	seen := map[string]bool{}
	for _, root := range roots {
		clean := filepath.Clean(root)
		if clean == "" || seen[clean] {
			continue
		}
		seen[clean] = true
		for _, name := range []string{".inferyard-managed-engine.json", "engine-prepare.json"} {
			raw, err := os.ReadFile(filepath.Join(clean, name))
			if err != nil {
				continue
			}
			var doc struct {
				Version   string `json:"version"`
				SourceURL string `json:"source_url"`
			}
			if json.Unmarshal(raw, &doc) != nil {
				continue
			}
			for _, field := range []string{doc.Version, doc.SourceURL} {
				if m := bonsaiPrismTagRe.FindStringSubmatch(field); len(m) == 2 {
					if n, err := strconv.Atoi(m[1]); err == nil {
						return n, true
					}
				}
			}
		}
	}
	return 0, false
}

func isPrismLlamaBuildAtLeast(binPath string, minBuild int) bool {
	dir := filepath.Dir(binPath)
	if n, ok := parsePrismBuildFromReceipts(dir, filepath.Dir(dir)); ok {
		return n >= minBuild
	}
	out, err := exec.Command(binPath, "--version").CombinedOutput()
	if err != nil {
		return false
	}
	text := string(out)
	if m := bonsaiPrismBannerRe.FindStringSubmatch(text); len(m) == 2 {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			return n >= minBuild
		}
	}
	if m := bonsaiPrismTagRe.FindStringSubmatch(text); len(m) == 2 {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			return n >= minBuild
		}
	}
	return false
}

func bonsaiEngineRootDir(binPath string) string {
	dir := filepath.Dir(binPath)
	parent := filepath.Dir(dir)
	for _, name := range []string{".inferyard-managed-engine.json", "engine-prepare.json"} {
		if info, err := os.Stat(filepath.Join(parent, name)); err == nil && info.Mode().IsRegular() {
			return parent
		}
	}
	return dir
}

func hostBonsaiSearchRoots(realHome string) (engineRoots, modelRoots []string) {
	seenEng, seenMod := map[string]bool{}, map[string]bool{}
	addEng := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seenEng[p] {
			seenEng[p] = true
			engineRoots = append(engineRoots, p)
		}
	}
	addMod := func(p string) {
		p = strings.TrimSpace(p)
		if p != "" && !seenMod[p] {
			seenMod[p] = true
			modelRoots = append(modelRoots, p)
		}
	}

	settingsPath := hostStudioSettingsPath(realHome)
	if raw, err := os.ReadFile(settingsPath); err == nil {
		var doc struct {
			WorkspaceRoot    string   `json:"workspace_root"`
			ModelsRoot       string   `json:"models_root"`
			ExtraModelsRoots []string `json:"extra_models_roots"`
		}
		if json.Unmarshal(raw, &doc) == nil {
			if doc.WorkspaceRoot != "" {
				addEng(filepath.Join(doc.WorkspaceRoot, "engines"))
				addMod(filepath.Join(doc.WorkspaceRoot, "models"))
			}
			addMod(doc.ModelsRoot)
			for _, extra := range doc.ExtraModelsRoots {
				addMod(extra)
			}
		}
	}
	addEng(filepath.Join(realHome, ".inferyard", "engines"))
	addMod(filepath.Join(realHome, ".inferyard", "models"))
	return engineRoots, modelRoots
}

func resolveLocalBonsaiEngine(t *testing.T, realHome string) (engineDir, engineBin string) {
	t.Helper()
	wantBin := llamaServerBinaryName()
	if raw := strings.TrimSpace(os.Getenv("INFERYARD_BONSAI_ENGINE")); raw != "" {
		abs, err := filepath.Abs(raw)
		if err != nil {
			t.Fatalf("resolve INFERYARD_BONSAI_ENGINE: %v", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("INFERYARD_BONSAI_ENGINE missing at %s: %v", abs, err)
		}
		if info.Mode().IsRegular() {
			if !isPrismLlamaBuildAtLeast(abs, minBonsaiPrismBuild) {
				t.Fatalf("INFERYARD_BONSAI_ENGINE %s is not PrismML llama.cpp >= b%d", abs, minBonsaiPrismBuild)
			}
			return bonsaiEngineRootDir(abs), abs
		}
		var found string
		_ = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || found != "" || d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if strings.EqualFold(d.Name(), wantBin) && isPrismLlamaBuildAtLeast(p, minBonsaiPrismBuild) {
				found = p
			}
			return nil
		})
		if found == "" {
			t.Fatalf("no PrismML llama.cpp >= b%d binary found under INFERYARD_BONSAI_ENGINE=%s", minBonsaiPrismBuild, abs)
		}
		return abs, found
	}

	engineRoots, _ := hostBonsaiSearchRoots(realHome)
	var hits []string
	for _, root := range engineRoots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr == nil && strings.Count(rel, string(filepath.Separator)) > 3 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(d.Name(), wantBin) {
				return nil
			}
			if isPrismLlamaBuildAtLeast(p, minBonsaiPrismBuild) {
				hits = append(hits, p)
			}
			return nil
		})
	}
	if len(hits) == 0 {
		t.Fatalf("no local PrismML llama.cpp >= b%d binary found in %v (set INFERYARD_BONSAI_ENGINE; downloading engines is forbidden)", minBonsaiPrismBuild, engineRoots)
	}
	sort.Strings(hits)
	chosen := hits[len(hits)-1]
	return bonsaiEngineRootDir(chosen), chosen
}

func resolveLocalBonsaiModels(t *testing.T, realHome string) []bonsaiModelSource {
	t.Helper()
	var scanRoots []string
	if raw := strings.TrimSpace(os.Getenv("INFERYARD_BONSAI_MODEL")); raw != "" {
		abs, err := filepath.Abs(raw)
		if err != nil {
			t.Fatalf("resolve INFERYARD_BONSAI_MODEL: %v", err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			t.Fatalf("INFERYARD_BONSAI_MODEL missing at %s: %v", abs, err)
		}
		if info.Mode().IsRegular() {
			src := filepath.Join(filepath.Dir(abs), "source.json")
			if sInfo, sErr := os.Stat(src); sErr != nil || !sInfo.Mode().IsRegular() {
				src = ""
			}
			return []bonsaiModelSource{{
				Dir:         filepath.Dir(abs),
				GGUFPath:    abs,
				Filename:    filepath.Base(abs),
				SourceJSON:  src,
				Bytes:       info.Size(),
				IsLooseFile: true,
			}}
		}
		scanRoots = []string{abs}
	} else {
		_, scanRoots = hostBonsaiSearchRoots(realHome)
	}

	seen := map[string]bool{}
	var found []bonsaiModelSource
	for _, root := range scanRoots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			rel, relErr := filepath.Rel(root, p)
			if relErr == nil && strings.Count(rel, string(filepath.Separator)) > 2 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !isBonsaiGGUFFilename(d.Name()) {
				return nil
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			canon := abs
			if c, cErr := filepath.EvalSymlinks(abs); cErr == nil {
				canon = c
			}
			if seen[canon] {
				return nil
			}
			seen[canon] = true
			info, iErr := d.Info()
			if iErr != nil || !info.Mode().IsRegular() {
				return nil
			}
			dir := filepath.Dir(abs)
			src := filepath.Join(dir, "source.json")
			if sInfo, sErr := os.Stat(src); sErr != nil || !sInfo.Mode().IsRegular() {
				src = ""
			}
			found = append(found, bonsaiModelSource{
				Dir:         dir,
				GGUFPath:    abs,
				Filename:    filepath.Base(abs),
				SourceJSON:  src,
				Bytes:       info.Size(),
				IsLooseFile: filepath.Clean(dir) == filepath.Clean(root),
			})
			return nil
		})
	}
	if len(found) == 0 {
		t.Fatalf("no local Bonsai 2.5 GGUF (PTQ1_0 / PQ2_0) found in %v (set INFERYARD_BONSAI_MODEL; downloading models is forbidden)", scanRoots)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Bytes != found[j].Bytes {
			return found[i].Bytes < found[j].Bytes
		}
		return found[i].GGUFPath < found[j].GGUFPath
	})
	return found
}

func stageLocalBonsai(t *testing.T, realHome, workspace string) stagedBonsai {
	t.Helper()
	srcEngineDir, srcEngineBin := resolveLocalBonsaiEngine(t, realHome)
	models := resolveLocalBonsaiModels(t, realHome)

	engSnap := snapshotDirState(t, srcEngineDir)
	t.Cleanup(func() {
		assertDirStateUnchanged(t, "host Bonsai engine directory", srcEngineDir, engSnap)
	})
	snappedModelDirs := map[string]bool{}
	for _, m := range models {
		if m.IsLooseFile {
			info, err := os.Stat(m.GGUFPath)
			if err != nil {
				t.Fatalf("stat %s: %v", m.GGUFPath, err)
			}
			wantSize, wantMod := info.Size(), info.ModTime().UnixNano()
			p := m.GGUFPath
			t.Cleanup(func() {
				after, err := os.Stat(p)
				if err != nil || after.Size() != wantSize || after.ModTime().UnixNano() != wantMod {
					t.Fatalf("host Bonsai model mutated at %s: err=%v", p, err)
				}
			})
			continue
		}
		if snappedModelDirs[m.Dir] {
			continue
		}
		snappedModelDirs[m.Dir] = true
		d, snap := m.Dir, snapshotDirState(t, m.Dir)
		t.Cleanup(func() {
			assertDirStateUnchanged(t, "host Bonsai model directory", d, snap)
		})
	}

	engName := filepath.Base(srcEngineDir)
	if !strings.HasPrefix(strings.ToLower(engName), "llama-cpp-prism-") {
		engName = "llama-cpp-prism-" + engName
	}
	dstEngineDir := filepath.Join(workspace, "engines", engName)
	cloneTreeCoW(t, srcEngineDir, dstEngineDir)
	relBin, err := filepath.Rel(srcEngineDir, srcEngineBin)
	if err != nil {
		t.Fatalf("relative binary path (%s -> %s): %v", srcEngineDir, srcEngineBin, err)
	}
	dstEngineBin := filepath.Join(dstEngineDir, relBin)

	clonedDirs := map[string]string{}
	var chosenPath, chosenName, chosenSourceJSON string
	var allModels []string
	for idx, m := range models {
		var dstModelDir, dstGGUF, dstSource string
		if m.IsLooseFile {
			dstModelDir = filepath.Join(workspace, "models", "bonsai-staged-"+strconv.Itoa(idx))
			dstGGUF = filepath.Join(dstModelDir, m.Filename)
			cloneTreeCoW(t, m.GGUFPath, dstGGUF)
			if m.SourceJSON != "" {
				dstSource = filepath.Join(dstModelDir, "source.json")
				cloneTreeCoW(t, m.SourceJSON, dstSource)
			}
		} else {
			if existing, ok := clonedDirs[m.Dir]; ok {
				dstModelDir = existing
			} else {
				dstModelDir = filepath.Join(workspace, "models", filepath.Base(m.Dir))
				cloneTreeCoW(t, m.Dir, dstModelDir)
				clonedDirs[m.Dir] = dstModelDir
			}
			dstGGUF = filepath.Join(dstModelDir, m.Filename)
			if m.SourceJSON != "" {
				dstSource = filepath.Join(dstModelDir, "source.json")
			}
		}
		if idx == 0 {
			chosenPath, chosenName, chosenSourceJSON = dstGGUF, m.Filename, dstSource
		}
		allModels = append(allModels, dstGGUF)
	}
	t.Logf("staged local Bonsai models (%d total, primary=%s) and engine=%s", len(allModels), chosenName, dstEngineBin)
	return stagedBonsai{
		EngineDir:        dstEngineDir,
		EngineBin:        dstEngineBin,
		ChosenModelPath:  chosenPath,
		ChosenModelName:  chosenName,
		ChosenSourceJSON: chosenSourceJSON,
		AllModels:        allModels,
	}
}
