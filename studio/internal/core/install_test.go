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

	"inferyard/studio/internal/inferyard"
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

func installService(t *testing.T, exec Executor) *Service {
	t.Helper()
	s, err := New(context.Background(), Options{
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Execute:      exec,
	})
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

func TestInstallCLIReusesCompatibleAndUpgradesOutdated(t *testing.T) {
	t.Run("reuse compatible without uv", func(t *testing.T) {
		stubInstall(t)
		compatible := fakeCLIFile(t)
		installLookPath = func(string) (string, error) { return "", errors.New("not found") }
		installStat = os.Stat
		installCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("uv must not run when compatible CLI is present")
			return nil, nil
		}
		Version = "0.0.9"
		t.Cleanup(func() { Version = "dev" })
		s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
			if p == compatible && len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
			}
			return inferyard.Execution{}, errors.New("unexpected")
		})
		s.settings.InferyardPath = ptr(compatible)
		got, e := s.InstallCLI(context.Background(), nil)
		if e != nil {
			t.Fatalf("InstallCLI: %+v", e)
		}
		if got != compatible {
			t.Fatalf("got %q, want %q", got, compatible)
		}
	})

	t.Run("upgrade outdated existing CLI", func(t *testing.T) {
		stubInstall(t)
		oldCLI := fakeCLIFile(t)
		binDir := t.TempDir()
		installed := filepath.Join(binDir, cliBinaryName())
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
		var calls [][]string
		installCommandOutput = func(_ context.Context, path string, args ...string) ([]byte, error) {
			if len(args) == 1 && args[0] == "--version" {
				return []byte("uv " + uvRelease + "\n"), nil
			}
			calls = append(calls, append([]string{path}, args...))
			if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
				if err := os.WriteFile(installed, []byte("new-bin"), 0755); err != nil {
					t.Fatal(err)
				}
				return []byte("Upgraded"), nil
			}
			if len(args) >= 2 && args[0] == "tool" && args[1] == "dir" {
				return []byte(binDir + "\n"), nil
			}
			return nil, errors.New("unexpected command")
		}
		Version = "0.0.9"
		t.Cleanup(func() { Version = "dev" })
		s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
			if len(args) > 0 && args[0] == "--version" {
				if p == oldCLI {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
				}
				if p == installed {
					return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
				}
			}
			return inferyard.Execution{}, errors.New("unexpected")
		})
		s.settings.InferyardPath = ptr(oldCLI)

		var statuses []string
		got, e := s.InstallCLI(context.Background(), func(msg string) { statuses = append(statuses, msg) })
		if e != nil {
			t.Fatalf("InstallCLI upgrade: %+v", e)
		}
		if got != installed {
			t.Fatalf("got %q, want %q", got, installed)
		}
		if len(calls) < 2 || !strings.Contains(strings.Join(calls[0], " "), "--upgrade --force") || !strings.Contains(strings.Join(calls[0], " "), "inferyard==0.0.9") {
			t.Fatalf("expected uv tool install --upgrade --force inferyard==0.0.9, got %+v", calls)
		}
		if len(statuses) == 0 || !strings.Contains(statuses[0], "0.0.9") {
			t.Fatalf("expected upgrade status message mentioning target version, got %+v", statuses)
		}
		if textOf(s.Settings().Settings.InferyardPath) != installed {
			t.Fatalf("settings inferyard_path = %q, want %q", textOf(s.Settings().Settings.InferyardPath), installed)
		}
		r := s.Readiness()
		if r.CLI.Status != "ok" || str(r.CLI.Version) != "0.0.9" {
			t.Fatalf("Readiness.CLI = %+v, want ok 0.0.9", r.CLI)
		}
	})

	t.Run("do not reuse broken existing CLI", func(t *testing.T) {
		stubInstall(t)
		broken := filepath.Join(t.TempDir(), cliBinaryName())
		if err := os.WriteFile(broken, []byte("broken"), 0644); err != nil {
			t.Fatal(err)
		}
		binDir := t.TempDir()
		installed := filepath.Join(binDir, cliBinaryName())
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
		installCommandOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) == 1 && args[0] == "--version" {
				return []byte("uv " + uvRelease + "\n"), nil
			}
			if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
				if err := os.WriteFile(installed, []byte("good"), 0755); err != nil {
					t.Fatal(err)
				}
				return []byte("Installed"), nil
			}
			return []byte(binDir + "\n"), nil
		}
		Version = "0.0.9"
		t.Cleanup(func() { Version = "dev" })
		s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
			if p == installed && len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.9 source=release\n")}, nil
			}
			return inferyard.Execution{ExitCode: 1}, nil
		})
		s.settings.InferyardPath = ptr(broken)
		got, e := s.InstallCLI(context.Background(), nil)
		if e != nil {
			t.Fatalf("InstallCLI: %+v", e)
		}
		if got != installed {
			t.Fatalf("expected broken CLI not to be reused; got %q want %q", got, installed)
		}
	})

	t.Run("reject post-install still outdated CLI", func(t *testing.T) {
		stubInstall(t)
		binDir := t.TempDir()
		installed := filepath.Join(binDir, cliBinaryName())
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
		installCommandOutput = func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) == 1 && args[0] == "--version" {
				return []byte("uv " + uvRelease + "\n"), nil
			}
			if len(args) >= 2 && args[0] == "tool" && args[1] == "install" {
				if err := os.WriteFile(installed, []byte("stale"), 0755); err != nil {
					t.Fatal(err)
				}
				return []byte("Installed"), nil
			}
			return []byte(binDir + "\n"), nil
		}
		Version = "0.0.9"
		t.Cleanup(func() { Version = "dev" })
		s := installService(t, func(_ context.Context, p string, args ...string) (inferyard.Execution, error) {
			if p == installed && len(args) > 0 && args[0] == "--version" {
				return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 0.0.3 source=release\n")}, nil
			}
			return inferyard.Execution{}, errors.New("not found")
		})
		if _, e := s.InstallCLI(context.Background(), nil); e == nil || e.Code != "cli_outdated" {
			t.Fatalf("expected cli_outdated when post-install CLI is still 0.0.3, got %+v", e)
		}
	})
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
