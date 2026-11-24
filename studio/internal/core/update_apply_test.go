package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUpdateActiveWorkloadGuardAndUnwritableFallback(t *testing.T) {
	payload := buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.10.0-linux-amd64/inferyard-studio": []byte("new-binary"),
	}, nil)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(githubReleasePayload{
				TagName: "v0.10.0",
				HTMLURL: srv.URL + "/releases/v0.10.0",
				Assets: []githubReleaseAsset{
					{Name: "inferyard-studio-v0.10.0-linux-amd64.tar.gz", BrowserDownloadURL: srv.URL + "/asset", Size: int64(len(payload))},
					{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS", Size: 120},
				},
			})
		case "/asset":
			_, _ = w.Write(payload)
		case "/SHA256SUMS":
			_, _ = w.Write([]byte(sha256Hex(payload) + "  inferyard-studio-v0.10.0-linux-amd64.tar.gz\n"))
		}
	}))
	defer srv.Close()

	var allowWrite atomic.Bool
	var applied atomic.Bool
	s := newUpdateTestService(t, UpdateOptions{
		APIURL:              srv.URL + "/latest",
		CurrentVersion:      "0.9.9",
		GOOS:                "linux",
		GOARCH:              "amd64",
		DisableAutoDownload: true,
		InspectTarget: func() updateInstallTarget {
			if !allowWrite.Load() {
				return updateInstallTarget{
					ExePath:       "/readonly/inferyard-studio",
					InstallDir:    "/readonly",
					CanSelfUpdate: false,
					BlockCode:     "update_path_unwritable",
					BlockMessage:  "安装目录不可写",
				}
			}
			return updateInstallTarget{ExePath: "/writable/inferyard-studio", InstallDir: "/writable", CanSelfUpdate: true}
		},
		ApplyFunc: func(_ context.Context, plan platformApplyPlan) error {
			applied.Store(true)
			return nil
		},
	})

	if _, err := s.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.runs = append(s.runs, &runEntry{Task: RunTask{TaskId: "r1", State: "running"}})
	s.downloads = append(s.downloads, &downloadEntry{Task: DownloadTask{TaskId: "d1", State: "downloading"}})
	s.mu.Unlock()

	st, err := s.ApplyUpdate(context.Background(), false)
	if !errors.Is(err, ErrUpdateWorkloadActive) || st.State != UpdateReadyToApply || !st.Workloads.Any() {
		t.Fatalf("expected active workload guard, got state=%+v err=%v", st, err)
	}
	if applied.Load() {
		t.Fatal("apply ran despite unconfirmed active workloads")
	}

	st, err = s.ApplyUpdate(context.Background(), true)
	if err == nil || st.State != UpdateError || st.ErrorCode != "update_path_unwritable" || st.FallbackPath == "" {
		t.Fatalf("expected unwritable fallback error, got state=%+v err=%v", st, err)
	}
	if _, statErr := os.Stat(st.FallbackPath); statErr != nil {
		t.Fatalf("fallback installer missing: %v", statErr)
	}
	if applied.Load() {
		t.Fatal("apply ran on unwritable path")
	}

	allowWrite.Store(true)
	if _, err := s.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, err = s.ApplyUpdate(context.Background(), true)
	if err != nil || st.State != UpdateApplying || !applied.Load() {
		t.Fatalf("confirmed writable apply failed: state=%+v err=%v applied=%v", st, err, applied.Load())
	}
}

func TestUpdatePlatformHelpersAndSecurityGuards(t *testing.T) {
	tmp := t.TempDir()
	appDir := filepath.Join(tmp, "InferYard Studio.app")
	exePath := filepath.Join(appDir, "Contents", "MacOS", "inferyard-studio")
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	gotApp, code, _ := resolveDarwinAppBundle(exePath)
	if code != "" || filepath.Base(gotApp) != "InferYard Studio.app" {
		t.Fatalf("resolveDarwinAppBundle failed: app=%q code=%q", gotApp, code)
	}
	if _, code, _ := resolveDarwinAppBundle("/private/var/folders/x/AppTranslocation/ABC/d/InferYard Studio.app/Contents/MacOS/inferyard-studio"); code != "update_app_translocated" {
		t.Fatalf("expected AppTranslocation block, got %q", code)
	}
	if _, code, _ := resolveDarwinAppBundle(filepath.Join(tmp, "plain-binary")); code != "update_not_app_bundle" {
		t.Fatalf("expected non-bundle block, got %q", code)
	}

	goodArchive := filepath.Join(tmp, "good.tar.gz")
	if err := os.WriteFile(goodArchive, buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.10.0-linux-amd64/inferyard-studio": []byte("v2-binary"),
		"inferyard-studio-v0.10.0-linux-amd64/LICENSE":          []byte("MIT"),
	}, nil), 0o600); err != nil {
		t.Fatal(err)
	}
	extracted, err := extractUpdateTarGz(goodArchive, filepath.Join(tmp, "extract"))
	if err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(tmp, "linux-install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linuxExe := filepath.Join(installDir, "inferyard-studio")
	if err := os.WriteFile(linuxExe, []byte("v1-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyLinuxExtracted(extracted, linuxExe); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(linuxExe)
	if string(raw) != "v2-binary" {
		t.Fatalf("linux binary not replaced: %q", string(raw))
	}

	traversalArchive := filepath.Join(tmp, "evil.tar.gz")
	_ = os.WriteFile(traversalArchive, buildTestTarGz(t, map[string][]byte{"../escape": []byte("bad")}, nil), 0o600)
	if _, err := extractUpdateTarGz(traversalArchive, filepath.Join(tmp, "extract-evil")); err == nil {
		t.Fatal("expected traversal tar.gz to fail")
	}
	symlinkArchive := filepath.Join(tmp, "symlink.tar.gz")
	_ = os.WriteFile(symlinkArchive, buildTestTarGz(t, nil, map[string]string{"link": "/etc/passwd"}), 0o600)
	if _, err := extractUpdateTarGz(symlinkArchive, filepath.Join(tmp, "extract-sym")); err == nil {
		t.Fatal("expected symlink tar.gz to fail")
	}

	winArgs := windowsInstallerArgs(`C:\Program Files\InferYard Studio`)
	if strings.Join(winArgs, " ") != `/SP- /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR=C:\Program Files\InferYard Studio` {
		t.Fatalf("unexpected Windows installer args: %v", winArgs)
	}
	if script := windowsUpdateScriptContent(); !strings.Contains(script, "[string]$InstallDir") || !strings.Contains(script, "/VERYSILENT") {
		t.Fatalf("unexpected Windows helper script: %s", script)
	}
}
