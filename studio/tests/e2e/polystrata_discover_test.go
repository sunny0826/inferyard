package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func hostStudioSettingsPath(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "InferYard Studio", "settings.json")
	case "windows":
		if appdata := strings.TrimSpace(os.Getenv("APPDATA")); appdata != "" {
			return filepath.Join(appdata, "InferYard Studio", "settings.json")
		}
		return filepath.Join(home, "AppData", "Roaming", "InferYard Studio", "settings.json")
	default:
		if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
			return filepath.Join(xdg, "InferYard Studio", "settings.json")
		}
		return filepath.Join(home, ".config", "InferYard Studio", "settings.json")
	}
}

func usablePolystrataInstallation(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var decl struct {
		Family           string `json:"family"`
		WorkingDirectory string `json:"working_directory"`
		DataDir          string `json:"data_dir"`
	}
	if json.Unmarshal(raw, &decl) != nil || decl.Family != "polystrata" {
		return false
	}
	workDir := strings.TrimSpace(decl.WorkingDirectory)
	if workDir == "" {
		workDir = filepath.Dir(path)
	}
	dataDir := strings.TrimSpace(decl.DataDir)
	if dataDir == "" {
		return false
	}
	wInfo, wErr := os.Stat(workDir)
	dInfo, dErr := os.Stat(dataDir)
	return wErr == nil && wInfo.IsDir() && dErr == nil && dInfo.IsDir()
}

func scanPolystrataEngineDirs(enginesRoot string) []string {
	entries, err := os.ReadDir(enginesRoot)
	if err != nil {
		return nil
	}
	type hit struct {
		modNano int64
		path    string
	}
	var hits []hit
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		lower := strings.ToLower(entry.Name())
		if lower != "polystrata" && !strings.HasPrefix(lower, "polystrata-") {
			continue
		}
		inst := filepath.Join(enginesRoot, entry.Name(), "installation.json")
		if info, sErr := os.Stat(inst); sErr == nil && info.Mode().IsRegular() {
			hits = append(hits, hit{modNano: info.ModTime().UnixNano(), path: inst})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].modNano != hits[j].modNano {
			return hits[i].modNano > hits[j].modNano
		}
		return hits[i].path > hits[j].path
	})
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.path)
	}
	return out
}

func resolveLocalPolystrataInstallation(t *testing.T) string {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv("INFERYARD_POLYSTRATA_INSTALLATION")); raw != "" {
		abs, err := filepath.Abs(raw)
		if err != nil {
			t.Fatalf("resolve INFERYARD_POLYSTRATA_INSTALLATION: %v", err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("INFERYARD_POLYSTRATA_INSTALLATION missing at %s: %v", abs, err)
		}
		return abs
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home dir: %v", err)
	}

	var workspaces []string
	var registered []string
	settingsPath := hostStudioSettingsPath(home)
	if rawSettings, rErr := os.ReadFile(settingsPath); rErr == nil {
		var settingsDoc struct {
			WorkspaceRoot       string   `json:"workspace_root"`
			EngineInstallations []string `json:"engine_installations"`
		}
		if json.Unmarshal(rawSettings, &settingsDoc) == nil {
			if ws := strings.TrimSpace(settingsDoc.WorkspaceRoot); ws != "" {
				workspaces = append(workspaces, ws)
			}
			for i := len(settingsDoc.EngineInstallations) - 1; i >= 0; i-- {
				if p := strings.TrimSpace(settingsDoc.EngineInstallations[i]); p != "" {
					registered = append(registered, p)
				}
			}
		}
	}
	workspaces = append(workspaces, filepath.Join(home, ".inferyard"))

	var candidates []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			candidates = append(candidates, p)
		}
	}
	for _, ws := range workspaces {
		for _, inst := range scanPolystrataEngineDirs(filepath.Join(ws, "engines")) {
			add(inst)
		}
		add(filepath.Join(ws, "engines", "polystrata", "installation.json"))
	}
	for _, inst := range registered {
		add(inst)
	}
	add(filepath.Join(home, "PolyStrata", "installation.json"))
	add(filepath.Join(home, "polystrata", "installation.json"))

	for _, c := range candidates {
		if usablePolystrataInstallation(c) {
			if abs, aErr := filepath.Abs(c); aErr == nil {
				return abs
			}
			return c
		}
	}
	t.Fatalf("no local PolyStrata installation.json found in [%s, %s, %s] (set INFERYARD_POLYSTRATA_INSTALLATION; downloading models is forbidden)",
		filepath.Join(home, ".inferyard", "engines", "polystrata*", "installation.json"),
		settingsPath,
		filepath.Join(home, "PolyStrata", "installation.json"),
	)
	return ""
}
