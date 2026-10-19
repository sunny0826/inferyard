//go:build studio_dev && !release

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"inferyard/studio/internal/core"
)

func devFixture(t *testing.T) *devManifest {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, ".inferyard-dev")
	m := &devManifest{Kind: "studio_dev.v1", Checkout: root, BuildRoot: filepath.Join(base, "builds", "build-1"), ProfileRoot: filepath.Join(base, "profiles", "branch", "generation"), AssetsRoot: filepath.Join(base, "assets"), Branch: "feat/test", Commit: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64)}
	m.path = filepath.Join(base, "launches", "session-1", "manifest.json")
	m.PackageRoot = filepath.Join(m.BuildRoot, "cli-env", "lib", "python3.15", "site-packages", "inferyard")
	for _, path := range []string{m.PackageRoot, m.ProfileRoot, m.AssetsRoot, filepath.Dir(m.path), filepath.Dir(m.cliPath()), filepath.Dir(m.studioPath())} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeDevFile(t, m.studioPath(), "studio")
	writeDevFile(t, m.cliPath(), "cli")
	writeDevFile(t, filepath.Join(m.PackageRoot, "__init__.py"), "# fixture\n")
	m.StudioSHA256, _ = fileDigest(m.studioPath())
	m.CLISHA256, _ = fileDigest(m.cliPath())
	m.PackageSHA256, _ = packageDigest(m.PackageRoot)
	if err := atomicDevJSON(m.path, m); err != nil {
		t.Fatal(err)
	}
	return m
}
func writeDevFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentOptions(t *testing.T) {
	for _, name := range []string{"--dev-manifest", "--dev-check", "--dev-stop"} {
		if _, err := parseOptions([]string{name, "/tmp/m.json"}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if _, err := parseOptions([]string{name, ""}, &bytes.Buffer{}); err == nil {
			t.Fatal("empty path accepted")
		}
	}
	if _, err := parseOptions([]string{"--dev-check", "/tmp/a", "--dev-stop", "/tmp/a"}, &bytes.Buffer{}); err == nil {
		t.Fatal("mixed modes accepted")
	}
	if _, err := parseOptions([]string{"--dev-manifest", "/tmp/a", "--debug-ui"}, &bytes.Buffer{}); err == nil {
		t.Fatal("preview and development accepted together")
	}
}

func TestDevelopmentBuildRequiresLaunchContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if handled, err := runDevelopment(ctx, cancel, startupOptions{}); !handled || err == nil {
		t.Fatal("a developer executable can fall back to normal user settings", handled, err)
	}
}

func TestDevelopmentManifest(t *testing.T) {
	m := devFixture(t)
	if _, err := loadDevManifest(m.path); err != nil {
		t.Fatal(err)
	}
	if err := m.checkSelf(); err == nil {
		t.Fatal("wrong running executable accepted")
	}
	for _, change := range []func(*devManifest){
		func(m *devManifest) { m.BuildRoot = m.Checkout },
		func(m *devManifest) { m.ProfileRoot = m.AssetsRoot },
		func(m *devManifest) { m.PackageRoot = filepath.Join(m.Checkout, "inferyard") },
		func(m *devManifest) { m.Commit = "short" },
		func(m *devManifest) { m.Kind = "wrong" },
	} {
		bad := *m
		change(&bad)
		if err := atomicDevJSON(m.path, &bad); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDevManifest(m.path); err == nil {
			t.Fatal("invalid manifest accepted", bad)
		}
	}
	for _, body := range []string{`{"kind":"studio_dev.v1","kind":"studio_dev.v1"}`, `{"kind":"studio_dev.v1"}`, `{"dirty":null}`} {
		writeDevFile(t, m.path, body)
		if _, err := loadDevManifest(m.path); err == nil {
			t.Fatal("invalid JSON accepted", body)
		}
	}
}

func TestDevelopmentPackageDigest(t *testing.T) {
	m := devFixture(t)
	expected := sha256.Sum256([]byte("__init__.py\x00# fixture\n\x00"))
	if m.PackageSHA256 != hex.EncodeToString(expected[:]) {
		t.Fatal(m.PackageSHA256)
	}
	cache := filepath.Join(m.PackageRoot, "__pycache__")
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	writeDevFile(t, filepath.Join(cache, "junk.pyc"), "ignored")
	writeDevFile(t, filepath.Join(m.PackageRoot, "junk.pyo"), "ignored")
	if got, err := packageDigest(m.PackageRoot); err != nil || got != m.PackageSHA256 {
		t.Fatal(got, err)
	}
	writeDevFile(t, filepath.Join(m.PackageRoot, "z.py"), "changed")
	if err := m.checkCLIArtifacts(); err == nil {
		t.Fatal("modified installed package accepted")
	}
	if got, err := packageDigest(m.PackageRoot); err != nil || got == m.PackageSHA256 {
		t.Fatal(got, err)
	}
	if err := os.Symlink(m.cliPath(), filepath.Join(m.PackageRoot, "link.py")); err != nil {
		t.Skip(err)
	}
	if _, err := packageDigest(m.PackageRoot); err == nil {
		t.Fatal("package symlink accepted")
	}
}

func TestDevelopmentCanonicalPaths(t *testing.T) {
	m := devFixture(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.AssetsRoot, "models")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if err := canonicalDevPath(link); err == nil {
		t.Fatal("symlink escape accepted")
	}
	m.ProfileRoot = filepath.Join(m.AssetsRoot, "models", "profile")
	if err := atomicDevJSON(m.path, m); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDevManifest(m.path); err == nil {
		t.Fatal("symlinked profile accepted")
	}
}

func TestDevelopmentImportAndArtifactChecks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX interpreter fixture; native Windows integration is separate")
	}
	m := devFixture(t)
	// The fixture interpreter records argv and simulates only the isolated import response.
	writeDevFile(t, m.pythonPath(), "#!/bin/sh\n[ \"$1\" = '-I' ] || exit 4\n[ \"$2\" = '-X' ] || exit 5\n[ \"$3\" = 'utf8' ] || exit 6\n[ \"$4\" = '-c' ] || exit 7\nprintf '%s\\n' '"+m.PackageRoot+"'\n")
	if err := m.checkCLI(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeDevFile(t, m.pythonPath(), "#!/bin/sh\nprintf '/other/inferyard\\n'\n")
	if err := m.checkCLI(context.Background()); err == nil {
		t.Fatal("wrong imported package accepted")
	}
	writeDevFile(t, m.cliPath(), "modified")
	if err := m.checkCLI(context.Background()); err == nil {
		t.Fatal("modified CLI accepted")
	}
}

func TestDevelopmentNativeProcessIdentity(t *testing.T) {
	pid := int64(os.Getpid())
	if ticks, err := core.DevelopmentProcessStartTicks(pid); err != nil || ticks <= 0 {
		t.Fatal(ticks, err)
	}
	got, err := core.DevelopmentProcessExecutable(pid)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, err = filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	want, err = filepath.EvalSymlinks(want)
	if err != nil || got != want {
		t.Fatal(got, want, err)
	}
}

func TestDevelopmentWindowStore(t *testing.T) {
	m := devFixture(t)
	store := devWindowStates{filepath.Join(m.ProfileRoot, "window-state.json")}
	states, err := store.LoadWindowStates()
	if err != nil || len(states) != 0 {
		t.Fatal(states, err)
	}
	if err := store.SaveWindowStates(states); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.path)
	if err != nil || !json.Valid(raw) {
		t.Fatal(err, string(raw))
	}
}
