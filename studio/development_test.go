//go:build studio_dev && !release

package main

import (
	"os"
	"path/filepath"

	"strings"
	"testing"
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
