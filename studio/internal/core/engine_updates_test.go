package core

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompareEngineVersions(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		latest    string
		wantCmp   int
		wantOK    bool
	}{
		{"build_older", "b5142", "b5200", -1, true},
		{"build_prism_older", "prism-b10743-adfffbe", "b11146", -1, true},
		{"build_equal", "b5200", "b5200", 0, true},
		{"build_newer", "b12000", "b11146", 1, true},
		{"semver_older", "0.24.0", "0.24.1", -1, true},
		{"semver_v_prefix", "v0.24.0", "0.24.1", -1, true},
		{"semver_equal", "v0.31.0", "0.31.0", 0, true},
		{"semver_prerelease_older", "v0.1.45-poly.9", "v0.1.45-poly.10", -1, true},
		{"semver_prerelease_equal", "v0.1.45-poly.10", "v0.1.45-poly.10", 0, true},
		{"incompatible_schemes", "b5142", "v1.0.0", 0, false},
		{"git_hash_incomparable", "e262b44", "latest", 0, false},
		{"unknown_string", "custom-dev", "v0.31.0", 0, false},
		{"empty_installed", "", "v0.31.0", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmp, ok := compareEngineVersions(tc.installed, tc.latest)
			if ok != tc.wantOK || cmp != tc.wantCmp {
				t.Fatalf("compareEngineVersions(%q, %q) = (%d, %v), want (%d, %v)", tc.installed, tc.latest, cmp, ok, tc.wantCmp, tc.wantOK)
			}
		})
	}
}

func TestServiceEngineUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.checkMu.Lock()
	svc.checkSupported = true
	svc.checkNames = []engineCheckTarget{
		{name: "llama-cpp-old", path: "/fake/llama-old", key: "k1"},
		{name: "mlx-lm-current", path: "/fake/mlx", key: "k2"},
		{name: "vllm-old", path: "/fake/vllm", key: "k3"},
		{name: "vllm-git", path: "/fake/vllm-git", key: "k4"},
	}
	svc.checks = map[string]engineCheckEntry{
		"/fake/llama-old": {
			key:      "k1",
			verified: true,
			result:   EngineEligibility{State: EngineCheckEligible, Family: "llama-cpp", Version: "b5142"},
		},
		"/fake/mlx": {
			key:      "k2",
			verified: true,
			result:   EngineEligibility{State: EngineCheckEligible, Family: "mlx-lm", Version: "0.32.2"},
		},
		"/fake/vllm": {
			key:      "k3",
			verified: true,
			result:   EngineEligibility{State: EngineCheckEligible, Family: "vllm", Version: "v0.29.0"},
		},
		"/fake/vllm-git": {
			key:      "k4",
			verified: true,
			result:   EngineEligibility{State: EngineCheckEligible, Family: "vllm", Version: "e262b44"},
		},
	}
	svc.checkMu.Unlock()

	updates := svc.EngineUpdates()
	if info := updates["llama-cpp-old"]; !info.HasUpdate || info.LatestVersion != "b11146" {
		t.Fatalf("expected llama-cpp-old update to b11146, got %+v", info)
	}
	if info := updates["mlx-lm-current"]; info.HasUpdate {
		t.Fatalf("expected mlx-lm-current no update, got %+v", info)
	}
	if info := updates["vllm-old"]; !info.HasUpdate || info.LatestVersion != "v0.31.0" {
		t.Fatalf("expected vllm-old update to v0.31.0, got %+v", info)
	}
	if info := updates["vllm-git"]; info.HasUpdate {
		t.Fatalf("expected vllm-git no false-positive update, got %+v", info)
	}
	if info := updates["llama-cpp-old"]; info.ReleaseID == "" || info.ReleaseVersion != "b11146" {
		t.Fatalf("expected llama-cpp-old ReleaseID and ReleaseVersion populated, got %+v", info)
	}
}

func TestServiceEngineUpdatesPolystrataBuildJSONFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	workDir := filepath.Join(dir, "polystrata-work")
	instPath := setupPolystrataInstallFixture(t, workDir, filepath.Join(dir, "data"), "v0.1.40-poly.1", false)

	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())
	svc.mu.Lock()
	svc.settings.EngineInstallations = []string{instPath}
	svc.mu.Unlock()

	// Simulate EngineChecks returning empty Version for PolyStrata installation.
	reg := svc.Registry()
	if len(reg.Engines) == 0 {
		t.Fatalf("expected registered polystrata engine in registry")
	}
	engName := reg.Engines[0].Name
	svc.checkMu.Lock()
	svc.checkSupported = true
	svc.checkNames = []engineCheckTarget{{name: engName, path: instPath, key: "p1"}}
	svc.checks = map[string]engineCheckEntry{
		instPath: {
			key:      "p1",
			verified: true,
			result:   EngineEligibility{State: EngineCheckEligible, Family: "polystrata", Version: ""},
		},
	}
	svc.checkMu.Unlock()

	updates := svc.EngineUpdates()
	info, ok := updates[engName]
	if !ok {
		t.Fatalf("expected update info for %q, got %+v", engName, updates)
	}
	if info.InstalledVersion != "v0.1.40-poly.1" {
		t.Fatalf("expected InstalledVersion from BUILD.json, got %+v", info)
	}
	if !info.HasUpdate || info.ReleaseID == "" || info.ReleaseVersion == "" {
		t.Fatalf("expected PolyStrata update detected with ReleaseID/ReleaseVersion, got %+v", info)
	}
}

func TestServiceEngineUpdatesPrismExpectedReleaseFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc, err := New(ctx, Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Shutdown(context.Background())

	svc.checkMu.Lock()
	svc.checkSupported = true
	svc.checkNames = []engineCheckTarget{
		{name: "prism-llama", path: "/fake/prism-llama", key: "prism1"},
	}
	svc.checks = map[string]engineCheckEntry{
		"/fake/prism-llama": {
			key:      "prism1",
			verified: true,
			result: EngineEligibility{
				State:   EngineCheckEligible,
				Family:  "llama-cpp",
				Version: "10743 adfffbe41",
				Release: "prism-b10743-adfffbe",
			},
		},
	}
	svc.checkMu.Unlock()

	updates := svc.EngineUpdates()
	info, ok := updates["prism-llama"]
	if !ok {
		t.Fatalf("expected update info for prism-llama, got %+v", updates)
	}
	if !info.HasUpdate || info.LatestVersion != "b11146" {
		t.Fatalf("expected Prism ExpectedRelease fallback to detect update to b11146, got %+v", info)
	}
	if info.InstalledVersion != "prism-b10743-adfffbe" {
		t.Fatalf("expected InstalledVersion to fall back to prism-b10743-adfffbe, got %+v", info)
	}
}
