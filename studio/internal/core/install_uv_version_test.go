package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestParseUVVersion(t *testing.T) {
	valid := []struct {
		raw  string
		want semver
	}{
		{"uv 0.13.6 (329bf9448 2026-10-26 aarch64-apple-darwin)\n", semver{0, 13, 6}},
		{"uv 0.12.1 (Homebrew 2026-07-31 aarch64-apple-darwin)", semver{0, 12, 1}},
		{"uv.exe 0.14.0", semver{0, 14, 0}},
		{"uv 0.13.6+local", semver{0, 13, 6}},
	}
	for _, tc := range valid {
		got, ok := parseUVVersion(tc.raw)
		if !ok || got != tc.want {
			t.Fatalf("parseUVVersion(%q) = (%+v, %v), want (%+v, true)", tc.raw, got, ok, tc.want)
		}
	}

	invalid := []string{
		"",
		"uv",
		"python 3.15.2",
		"uv 0.13.6-rc.1",
		"uv dev",
	}
	for _, raw := range invalid {
		if got, ok := parseUVVersion(raw); ok {
			t.Fatalf("parseUVVersion(%q) unexpectedly succeeded: %+v", raw, got)
		}
	}
}

func makeFakeUVArchive(t *testing.T, asset string, payload []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(asset, ".zip") {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create("uv-dir/" + uvBinaryName())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Name:     "uv-dir/" + uvBinaryName(),
			Mode:     0755,
			Size:     int64(len(payload)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	return archive, hex.EncodeToString(sum[:]) + "  " + asset + "\n"
}

func TestInstallCLISkipsOutdatedUVAndDownloadsManaged(t *testing.T) {
	stubInstall(t)
	asset, apiErr := uvAsset()
	if apiErr != nil {
		t.Skipf("unsupported test platform: %+v", apiErr)
	}
	archive, checksum := makeFakeUVArchive(t, asset, []byte("managed-uv-binary"))

	oldUV := filepath.Join(t.TempDir(), uvBinaryName())
	if err := os.WriteFile(oldUV, []byte("old-uv"), 0755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	installed := filepath.Join(binDir, cliBinaryName())

	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return oldUV, nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		for _, dir := range extraBinDirs() {
			if p == filepath.Join(dir, uvBinaryName()) {
				return nil, os.ErrNotExist
			}
		}
		return os.Stat(p)
	}

	var downloads []string
	installHTTPDo = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		downloads = append(downloads, req.URL.String())
		if strings.HasSuffix(req.URL.Path, ".sha256") {
			return httpBody(200, checksum), nil
		}
		return &http.Response{
			StatusCode:    200,
			ContentLength: int64(len(archive)),
			Body:          io.NopCloser(bytes.NewReader(archive)),
		}, nil
	}).Do

	var installUVPath string
	installCommandOutput = func(_ context.Context, path string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			if path == oldUV {
				return []byte("uv 0.11.7 (9d177269e 2026-04-15 aarch64-apple-darwin)\n"), nil
			}
			return []byte("uv " + uvRelease + " (329bf9448 2026-10-26 aarch64-apple-darwin)\n"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			installUVPath = path
			if err := os.WriteFile(installed, []byte("new-cli"), 0755); err != nil {
				t.Fatal(err)
			}
			return []byte("Installed"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	Version = "0.0.10"
	t.Cleanup(func() { Version = "dev" })
	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if p == installed && len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.10 source=release\n")}, nil
		}
		return inferyard.Execution{}, errors.New("not found")
	})

	if locs := s.PreparationLocations(); locs.UV != nil {
		t.Fatalf("PreparationLocations().UV = %q when only outdated uv exists, want nil", *locs.UV)
	}

	got, e := s.InstallCLI(context.Background(), nil)
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if got != installed {
		t.Fatalf("InstallCLI path = %q, want %q", got, installed)
	}
	managedUV := filepath.Join(filepath.Dir(s.settingsPath), "tools", uvBinaryName())
	if installUVPath != managedUV {
		t.Fatalf("uv tool install ran with %q, want managed %q", installUVPath, managedUV)
	}
	if len(downloads) != 2 {
		t.Fatalf("expected archive + sha256 download, got %v", downloads)
	}
	if locs := s.PreparationLocations(); locs.UV == nil || *locs.UV != managedUV {
		t.Fatalf("PreparationLocations().UV after download = %v, want %q", locs.UV, managedUV)
	}
}

func TestInstallCLISkipsOutdatedPathUVAndReusesCompatibleToolsUV(t *testing.T) {
	stubInstall(t)
	oldUV := filepath.Join(t.TempDir(), uvBinaryName())
	if err := os.WriteFile(oldUV, []byte("old-uv"), 0755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	installed := filepath.Join(binDir, cliBinaryName())

	Version = "0.0.10"
	t.Cleanup(func() { Version = "dev" })
	s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
		if p == installed && len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.10 source=release\n")}, nil
		}
		return inferyard.Execution{}, errors.New("not found")
	})

	toolsDir := filepath.Join(filepath.Dir(s.settingsPath), "tools")
	if err := os.MkdirAll(toolsDir, 0700); err != nil {
		t.Fatal(err)
	}
	managedUV := filepath.Join(toolsDir, uvBinaryName())
	if err := os.WriteFile(managedUV, []byte("managed-uv"), 0755); err != nil {
		t.Fatal(err)
	}

	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return oldUV, nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		for _, dir := range extraBinDirs() {
			if p == filepath.Join(dir, uvBinaryName()) {
				return nil, os.ErrNotExist
			}
		}
		return os.Stat(p)
	}
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network must not be used when toolsDir uv is compatible")
	}).Do

	var installUVPath string
	installCommandOutput = func(_ context.Context, path string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "--version" {
			if path == oldUV {
				return []byte("uv 0.11.7\n"), nil
			}
			if path == managedUV {
				return []byte("uv " + uvRelease + "\n"), nil
			}
			return nil, errors.New("unexpected uv path")
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
			installUVPath = path
			if err := os.WriteFile(installed, []byte("new-cli"), 0755); err != nil {
				t.Fatal(err)
			}
			return []byte("Installed"), nil
		}
		if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		return nil, errors.New("unexpected command")
	}

	if locs := s.PreparationLocations(); locs.UV == nil || *locs.UV != managedUV {
		t.Fatalf("PreparationLocations().UV = %v, want %q", locs.UV, managedUV)
	}

	got, e := s.InstallCLI(context.Background(), nil)
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if got != installed {
		t.Fatalf("InstallCLI path = %q, want %q", got, installed)
	}
	if installUVPath != managedUV {
		t.Fatalf("uv tool install ran with %q, want managed %q", installUVPath, managedUV)
	}
}
