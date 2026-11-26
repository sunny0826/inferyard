package e2e

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

type stagedPolystrata struct {
	InstPath   string
	WorkDir    string
	DataDir    string
	ChosenPath string
}

func snapshotDirState(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, _ := os.Readlink(path)
			snap[rel] = "sym:" + link
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".venv" {
				snap[rel] = "dir:.venv"
				return filepath.SkipDir
			}
			snap[rel] = "dir"
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		snap[rel] = fmt.Sprintf("file:%d:%d", info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snap
}

func assertDirStateUnchanged(t *testing.T, label, root string, want map[string]string) {
	t.Helper()
	got := snapshotDirState(t, root)
	if len(got) != len(want) {
		t.Fatalf("%s mutated: entry count=%d want=%d", label, len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s mutated at %s: got=%q want=%q", label, k, got[k], v)
		}
	}
}

func cloneTreeCoW(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("/bin/cp", "-cR", src, dst).CombinedOutput(); err == nil {
			return
		} else {
			t.Logf("APFS cp -cR fallback to cp -R (%v: %s)", err, strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("cp", "-R", src, dst).CombinedOutput(); err != nil {
		t.Fatalf("clone %s -> %s failed: %v\n%s", src, dst, err, string(out))
	}
}

func rewritePathPrefixesInFile(t *testing.T, path string, replacements [][2]string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	updated := string(raw)
	for _, pair := range replacements {
		if pair[0] != "" && pair[0] != pair[1] {
			updated = strings.ReplaceAll(updated, pair[0], pair[1])
		}
	}
	if updated != string(raw) {
		if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
			t.Fatalf("write rewritten %s: %v", path, err)
		}
	}
}

func strataConfigUsable(cfgPath, dstWorkDir string, diskBytes int64) bool {
	if diskBytes >= 1<<60 {
		return false
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return false
	}
	var doc struct {
		Exe string `json:"exe"`
		Cwd string `json:"cwd"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	exe := strings.TrimSpace(doc.Exe)
	if exe == "" {
		return false
	}
	if filepath.IsAbs(exe) {
		info, sErr := os.Stat(exe)
		return sErr == nil && info.Mode().IsRegular()
	}
	if strings.ContainsAny(exe, `/\`) {
		cwd := strings.TrimSpace(doc.Cwd)
		if cwd == "" {
			cwd = dstWorkDir
		}
		if info, sErr := os.Stat(filepath.Join(cwd, exe)); sErr == nil && info.Mode().IsRegular() {
			return true
		}
		info, sErr := os.Stat(filepath.Join(dstWorkDir, exe))
		return sErr == nil && info.Mode().IsRegular()
	}
	_, lErr := exec.LookPath(exe)
	return lErr == nil
}

func polystrataStageSearchDirs(dstWorkDir, dstDataDir string) []string {
	dirs := []string{
		dstWorkDir,
		filepath.Join(dstWorkDir, "terminal"),
		dstDataDir,
		filepath.Join(dstDataDir, "models"),
	}
	modelsDir := filepath.Join(dstDataDir, "models")
	if subEntries, err := os.ReadDir(modelsDir); err == nil {
		for _, sub := range subEntries {
			if sub.Type()&os.ModeSymlink == 0 && sub.IsDir() {
				dirs = append(dirs, filepath.Join(modelsDir, sub.Name()))
			}
		}
	}
	return dirs
}

func stageLocalPolystrata(t *testing.T, workspace string) stagedPolystrata {
	t.Helper()
	srcInstPath := resolveLocalPolystrataInstallation(t)
	rawInst, err := os.ReadFile(srcInstPath)
	if err != nil {
		t.Fatalf("read %s: %v", srcInstPath, err)
	}
	var decl struct {
		Family           string `json:"family"`
		WorkingDirectory string `json:"working_directory"`
		DataDir          string `json:"data_dir"`
	}
	if err := json.Unmarshal(rawInst, &decl); err != nil || decl.Family != "polystrata" {
		t.Fatalf("invalid polystrata installation at %s: %v", srcInstPath, err)
	}
	srcWorkDir := strings.TrimSpace(decl.WorkingDirectory)
	if srcWorkDir == "" {
		srcWorkDir = filepath.Dir(srcInstPath)
	}
	srcDataDir := strings.TrimSpace(decl.DataDir)
	if srcDataDir == "" {
		t.Fatalf("polystrata installation at %s has empty data_dir", srcInstPath)
	}

	workSnap := snapshotDirState(t, srcWorkDir)
	dataSnap := snapshotDirState(t, srcDataDir)
	t.Cleanup(func() {
		assertDirStateUnchanged(t, "host polystrata working_directory", srcWorkDir, workSnap)
		assertDirStateUnchanged(t, "host polystrata data_dir", srcDataDir, dataSnap)
	})

	dstWorkDir := filepath.Join(workspace, "engines", "polystrata")
	dstDataDir := filepath.Join(workspace, "models", "polystrata-data")
	cloneTreeCoW(t, srcWorkDir, dstWorkDir)
	cloneTreeCoW(t, srcDataDir, dstDataDir)

	srcWorkCanon, _ := filepath.EvalSymlinks(srcWorkDir)
	srcDataCanon, _ := filepath.EvalSymlinks(srcDataDir)
	replacements := [][2]string{
		{srcWorkDir, dstWorkDir},
		{srcWorkCanon, dstWorkDir},
		{srcDataDir, dstDataDir},
		{srcDataCanon, dstDataDir},
	}

	dstInstPath := filepath.Join(dstWorkDir, "installation.json")
	rewritePathPrefixesInFile(t, dstInstPath, replacements)
	for _, relCfg := range []string{
		filepath.Join(".inferyard-config", "polystrata", "settings.json"),
		filepath.Join(".inferyard-config", "PolyStrata", "settings.json"),
	} {
		cfgFile := filepath.Join(dstWorkDir, relCfg)
		if info, sErr := os.Lstat(cfgFile); sErr == nil && info.Mode().IsRegular() {
			rewritePathPrefixesInFile(t, cfgFile, replacements)
		}
	}

	type cfgCandidate struct {
		path       string
		usableRank int
		localRank  int
		bytes      int64
	}
	var configs []cfgCandidate
	seenFiles := map[string]bool{}
	for _, searchDir := range polystrataStageSearchDirs(dstWorkDir, dstDataDir) {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			full := filepath.Join(searchDir, name)
			if seenFiles[full] {
				continue
			}
			seenFiles[full] = true
			if strings.HasSuffix(name, ".log") {
				_ = os.Remove(full)
				continue
			}
			if !strings.HasPrefix(name, "strata-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			rewritePathPrefixesInFile(t, full, replacements)
			bytes := estimateStrataConfigDiskBytes(full)
			usableRank := 1
			if strataConfigUsable(full, dstWorkDir, bytes) {
				usableRank = 0
			}
			localRank := 1
			if rel, rErr := filepath.Rel(dstWorkDir, full); rErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				localRank = 0
			}
			configs = append(configs, cfgCandidate{
				path:       full,
				usableRank: usableRank,
				localRank:  localRank,
				bytes:      bytes,
			})
		}
	}
	if len(configs) == 0 {
		t.Fatalf("no strata-*.json model configs found in staged PolyStrata (%s, %s)", dstWorkDir, dstDataDir)
	}
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].usableRank != configs[j].usableRank {
			return configs[i].usableRank < configs[j].usableRank
		}
		if configs[i].localRank != configs[j].localRank {
			return configs[i].localRank < configs[j].localRank
		}
		if configs[i].bytes != configs[j].bytes {
			return configs[i].bytes < configs[j].bytes
		}
		return configs[i].path < configs[j].path
	})
	for _, extra := range configs[1:] {
		_ = os.Remove(extra.path)
	}
	t.Logf("staged smallest local PolyStrata model config: %s (referenced disk bytes=%d)", filepath.Base(configs[0].path), configs[0].bytes)
	return stagedPolystrata{
		InstPath:   dstInstPath,
		WorkDir:    dstWorkDir,
		DataDir:    dstDataDir,
		ChosenPath: configs[0].path,
	}
}

func estimateStrataConfigDiskBytes(cfgPath string) int64 {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return 1 << 60
	}
	var doc struct {
		Args []string `json:"args"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return 1 << 60
	}
	var total int64
	for i := 0; i+1 < len(doc.Args); i++ {
		switch doc.Args[i] {
		case "--pack", "--native", "--ple-gguf", "--model", "--mtp":
			p := doc.Args[i+1]
			if info, err := os.Stat(p); err == nil {
				if info.IsDir() {
					_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, wErr error) error {
						if wErr == nil && d.Type().IsRegular() {
							if fi, iErr := d.Info(); iErr == nil {
								total += fi.Size()
							}
						}
						return nil
					})
				} else {
					total += info.Size()
				}
			}
		}
	}
	if total <= 0 {
		return 1 << 60
	}
	return total
}

func freePortInt(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitManagedServiceStopped(t *testing.T, service *core.Service, serviceID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, ok := service.ManagedService(serviceID)
		if (!ok || st.State == "stopped") && len(service.ManagedServices()) == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	st, _ := service.ManagedService(serviceID)
	t.Fatalf("managed service %s did not stop in time: state=%+v active=%+v", serviceID, st, service.ManagedServices())
}
