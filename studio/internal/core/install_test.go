package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func httpBody(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func stubInstall(t *testing.T) {
	t.Helper()
	look, stat, httpDo, cmdOut := installLookPath, installStat, installHTTPDo, installCommandOutput
	t.Cleanup(func() {
		installLookPath, installStat, installHTTPDo, installCommandOutput = look, stat, httpDo, cmdOut
	})
	// Isolate per-user fallback probing from the developer machine state.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func installService(t *testing.T) *Service {
	t.Helper()
	s, err := New(context.Background(), Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFindCLIInPath(t *testing.T) {
	stubInstall(t)
	installLookPath = func(string) (string, error) { return "/usr/local/bin/inferyard", nil }
	if got := FindCLI(); got != "/usr/local/bin/inferyard" {
		t.Fatalf("FindCLI = %q", got)
	}
}

func TestFindCLIFallsBackToLocalBin(t *testing.T) {
	stubInstall(t)
	installLookPath = func(string) (string, error) { return "", errors.New("not found") }
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	want := filepath.Join(home, ".local", "bin", cliBinaryName())
	installStat = func(p string) (os.FileInfo, error) {
		if p == want {
			return fakeInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	if got := FindCLI(); got != want {
		t.Fatalf("FindCLI = %q, want %q", got, want)
	}
}

type fakeInfo struct{}

func (fakeInfo) Name() string       { return "fake" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() os.FileMode  { return 0755 }
func (fakeInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() interface{}   { return nil }

func TestInstallCLIReusesExisting(t *testing.T) {
	stubInstall(t)
	installLookPath = func(string) (string, error) { return "/usr/local/bin/inferyard", nil }
	s := installService(t)
	statuses := []string{}
	path, e := s.InstallCLI(context.Background(), func(st string) { statuses = append(statuses, st) })
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if path != "/usr/local/bin/inferyard" {
		t.Fatalf("path = %q", path)
	}
	if got := textOf(s.Settings().Settings.InferyardPath); got != "/usr/local/bin/inferyard" {
		t.Fatalf("settings inferyard_path = %q", got)
	}
	if len(statuses) == 0 || !strings.Contains(statuses[len(statuses)-1], "已就绪") {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestInstallCLIKeepsConfiguredPath(t *testing.T) {
	stubInstall(t)
	installLookPath = func(string) (string, error) { return "/usr/local/bin/inferyard", nil }
	s := installService(t)
	configured := filepath.Join(t.TempDir(), "inferyard")
	if err := os.WriteFile(configured, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(configured)
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": raw}); e != nil {
		t.Fatal(e)
	}
	path, e := s.InstallCLI(context.Background(), func(string) {})
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if path != configured {
		t.Fatalf("path = %q, want configured %q", path, configured)
	}
}

func TestInstallCLIIgnoresMissingConfiguredPath(t *testing.T) {
	stubInstall(t)
	installLookPath = func(string) (string, error) { return "", errors.New("not found") }
	s := installService(t)
	raw, err := json.Marshal("/missing/inferyard")
	if err != nil {
		t.Fatal(err)
	}
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"inferyard_path": raw}); e != nil {
		t.Fatal(e)
	}
	installLookPath = func(string) (string, error) { return "/usr/local/bin/inferyard", nil }
	path, e := s.InstallCLI(context.Background(), func(string) {})
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if path != "/usr/local/bin/inferyard" {
		t.Fatalf("path = %q", path)
	}
}

func TestInstallCLIViaUV(t *testing.T) {
	stubInstall(t)
	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return "/opt/uv/uv", nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		if p == "/opt/uv/uv" {
			return fakeInfo{}, nil
		}
		return os.Stat(p)
	}
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network must not be used in this test")
	}).Do
	binDir := t.TempDir()
	installed := filepath.Join(binDir, cliBinaryName())
	calls := [][]string{}
	installCommandOutput = func(_ context.Context, path string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{path}, args...))
		if len(args) > 0 && args[0] == "tool" && args[1] == "install" {
			if err := os.WriteFile(installed, []byte("bin"), 0755); err != nil {
				t.Fatal(err)
			}
			return []byte("Installed 1 executable"), nil
		}
		if len(args) > 0 && args[0] == "tool" && args[1] == "dir" {
			return []byte(binDir + "\n"), nil
		}
		t.Fatalf("unexpected command: %v", args)
		return nil, nil
	}
	Version = "0.0.2"
	t.Cleanup(func() { Version = "dev" })
	s := installService(t)
	path, e := s.InstallCLI(context.Background(), func(string) {})
	if e != nil {
		t.Fatalf("InstallCLI: %+v", e)
	}
	if path != installed {
		t.Fatalf("path = %q, want %q", path, installed)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	joined := strings.Join(calls[0], " ")
	for _, want := range []string{"tool", "install", "--managed-python", "--python", installPython, "inferyard==0.0.2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("install call %q missing %q", joined, want)
		}
	}
	if got := textOf(s.Settings().Settings.InferyardPath); got != installed {
		t.Fatalf("settings inferyard_path = %q", got)
	}
}

func TestInstallCLIFailureTail(t *testing.T) {
	stubInstall(t)
	installLookPath = func(name string) (string, error) {
		if name == uvBinaryName() {
			return "/opt/uv/uv", nil
		}
		return "", errors.New("not found")
	}
	installStat = func(p string) (os.FileInfo, error) {
		if p == "/opt/uv/uv" {
			return fakeInfo{}, nil
		}
		return os.Stat(p)
	}
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network must not be used in this test")
	}).Do
	installCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("error: network unreachable"), errors.New("exit 1")
	}
	Version = "0.0.2"
	t.Cleanup(func() { Version = "dev" })
	s := installService(t)
	_, e := s.InstallCLI(context.Background(), func(string) {})
	if e == nil || e.Code != "install_failed" {
		t.Fatalf("err = %+v", e)
	}
	if !strings.Contains(e.Details["output_tail"].(string), "network unreachable") {
		t.Fatalf("details = %v", e.Details)
	}
}

func TestDownloadUVChecksumMismatch(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".sha256") {
			return httpBody(200, strings.Repeat("0", 64)+"  uv.tar.gz"), nil
		}
		return httpBody(200, "payload"), nil
	}).Do
	_, e := downloadUV(context.Background(), t.TempDir())
	if e == nil || e.Code != "install_checksum_failed" {
		t.Fatalf("err = %+v", e)
	}
}

func TestDownloadUVUnpacksVerifiedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tar.gz path covered on unix")
	}
	stubInstall(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	payload := []byte("#!/bin/sh\necho uv\n")
	name := "uv-aarch64-apple-darwin/" + uvBinaryName()
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		name = "uv-x86_64-apple-darwin/" + uvBinaryName()
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
		name = "uv-x86_64-unknown-linux-gnu/" + uvBinaryName()
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		name = "uv-aarch64-unknown-linux-gnu/" + uvBinaryName()
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
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
	sum := sha256.Sum256(buf.Bytes())
	installHTTPDo = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".sha256") {
			return httpBody(200, hex.EncodeToString(sum[:])+"  asset"), nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(buf.Bytes()))}, nil
	}).Do
	dir := t.TempDir()
	path, e := downloadUV(context.Background(), dir)
	if e != nil {
		t.Fatalf("downloadUV: %+v", e)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0755 {
		t.Fatalf("mode = %v", st.Mode())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("payload mismatch: %q", body)
	}
}

func textOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
