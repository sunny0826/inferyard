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
	"time"

	"inferyard/studio/internal/inferyard"
)

func TestUpdateProxyRoutingAndTimeoutDegradation(t *testing.T) {
	var proxiedHits atomic.Int32
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHits.Add(1)
		_ = json.NewEncoder(w).Encode(githubReleasePayload{TagName: "v0.9.9"})
	}))
	defer proxySrv.Close()

	s := newUpdateTestService(t, UpdateOptions{
		APIURL:         "http://example.invalid/releases/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "darwin",
		GOARCH:         "arm64",
	})
	rawProxy, _ := json.Marshal(proxySrv.URL)
	if _, apiErr := s.UpdateSettings(map[string]json.RawMessage{"download_proxy": rawProxy}); apiErr != nil {
		t.Fatal(apiErr)
	}
	st, err := s.CheckForUpdate(context.Background())
	if err != nil || st.State != UpdateUpToDate || proxiedHits.Load() != 1 {
		t.Fatalf("proxy routing failed: state=%+v err=%v hits=%d", st, err, proxiedHits.Load())
	}

	// Timeout on startup check degrades silently; manual check returns retryable timeout error.
	releaseBlock := make(chan struct{})
	defer close(releaseBlock)
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-releaseBlock:
		case <-r.Context().Done():
		}
	}))
	defer slowSrv.Close()

	timeoutSvc := newUpdateTestService(t, UpdateOptions{
		APIURL:         slowSrv.URL + "/latest",
		CurrentVersion: "0.9.9",
		StartupDelay:   5 * time.Millisecond,
		CheckTimeout:   40 * time.Millisecond,
	})
	_, off, cursor := timeoutSvc.Bus.Subscribe()
	defer off()
	sawChecking := false
	timeoutSvc.StartBackgroundUpdateCheck()
	wait(t, func() bool {
		events, _, next := timeoutSvc.Bus.After(cursor)
		cursor = next
		for _, e := range events {
			if e.Type == "update.state" && json.Valid(e.Data) {
				var p struct {
					Update UpdateState `json:"update"`
				}
				if json.Unmarshal(e.Data, &p) == nil && p.Update.State == UpdateChecking {
					sawChecking = true
				}
			}
		}
		return sawChecking && timeoutSvc.UpdateStatus().State == UpdateIdle
	})
	if st := timeoutSvc.UpdateStatus(); st.ErrorCode != "" || st.ErrorMessage != "" {
		t.Fatalf("startup timeout leaked error: %+v", st)
	}

	st, err = timeoutSvc.CheckForUpdate(context.Background())
	if err == nil || st.State != UpdateError || st.ErrorCode != "update_timeout" {
		t.Fatalf("expected manual timeout error, got state=%+v err=%v", st, err)
	}
}

func TestUpdateContextSpeedGuardDevModeAndCorruptArchiveCleanup(t *testing.T) {
	payload := buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.10.0-linux-amd64/inferyard-studio": []byte("new-bin"),
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

	// Active context_speed run blocks unconfirmed ApplyUpdate, and corrupt apply cleans up staging.
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	s := newUpdateTestService(t, UpdateOptions{
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		StagingRoot:    stagingRoot,
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
		ApplyFunc: func(context.Context, platformApplyPlan) error {
			return &updateCheckError{code: "update_extract_failed", message: "bad archive"}
		},
	})
	if _, err := s.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.runs = append(s.runs, &runEntry{Task: RunTask{TaskId: "cs1", Kind: "context_speed", State: "running"}})
	s.mu.Unlock()

	st, err := s.ApplyUpdate(context.Background(), false)
	if !errors.Is(err, ErrUpdateWorkloadActive) || st.Workloads.ActiveRuns != 1 {
		t.Fatalf("expected active context_speed guard, got state=%+v err=%v", st, err)
	}

	st, err = s.ApplyUpdate(context.Background(), true)
	if err == nil || st.State != UpdateError || st.ErrorCode != "update_extract_failed" || st.FallbackPath != "" {
		t.Fatalf("corrupt archive did not clear fallback: state=%+v err=%v", st, err)
	}
	if entries, _ := os.ReadDir(stagingRoot); len(entries) != 0 {
		t.Fatalf("corrupt archive staging not cleaned: %v", entries)
	}

	// Dev-mode guard blocks self-update cleanly.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cliBin := filepath.Join(tmp, "inferyard")
	_ = os.WriteFile(cliBin, []byte("cli"), 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	devSvc, err := New(ctx, Options{
		Execute: func(context.Context, string, ...string) (inferyard.Execution, error) {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=dev\n")}, nil
		},
		Paths: &BoundPaths{
			CLI:       cliBin,
			Workspace: filepath.Join(tmp, "workspace"),
			Results:   filepath.Join(tmp, "results"),
			Models:    filepath.Join(tmp, "models"),
			Engines:   filepath.Join(tmp, "engines"),
			Settings:  filepath.Join(tmp, "settings.json"),
		},
		Update: UpdateOptions{
			APIURL:            srv.URL + "/latest",
			CurrentVersion:    "0.9.9",
			GOOS:              "linux",
			GOARCH:            "amd64",
			AllowInsecureHTTP: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err = devSvc.CheckForUpdate(context.Background())
	if err != nil || st.State != UpdateAvailable || st.CanSelfUpdate {
		t.Fatalf("dev-mode check should mark CanSelfUpdate=false: state=%+v err=%v", st, err)
	}
}

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
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
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
