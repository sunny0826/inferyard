package core

import (
	"context"

	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

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

type fakeInfo struct{}

func (fakeInfo) Name() string       { return "fake" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() os.FileMode  { return 0755 }
func (fakeInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (fakeInfo) IsDir() bool        { return false }
func (fakeInfo) Sys() interface{}   { return nil }

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
	var joined string
	for _, call := range calls {
		if len(call) >= 3 && call[1] == "tool" && call[2] == "install" {
			joined = strings.Join(call, " ")
			break
		}
	}
	for _, want := range []string{"tool", "install", "--managed-python", "--python", installPython, "inferyard==0.0.2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("install call %q missing %q", joined, want)
		}
	}
	if got := textOf(s.Settings().Settings.InferyardPath); got != installed {
		t.Fatalf("settings inferyard_path = %q", got)
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

func textOf(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
