package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"inferyard/studio/internal/inferyard"
)

func TestUpdateAdmissionFreezeCancelGuardPreApplyVerifyAndWindowsRecovery(t *testing.T) {
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

	// 1. Tampered staged artifact on disk is caught by pre-apply SHA-256 verification and cleaned up.
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	tamperSvc := newUpdateTestService(t, UpdateOptions{
		APIURL:              srv.URL + "/latest",
		CurrentVersion:      "0.9.9",
		GOOS:                "linux",
		GOARCH:              "amd64",
		StagingRoot:         stagingRoot,
		DisableAutoDownload: true,
		InspectTarget:       func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
	})
	if _, err := tamperSvc.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := tamperSvc.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	tamperSvc.updater.mu.Lock()
	artifactPath := tamperSvc.updater.stagedArtifact
	tamperSvc.updater.mu.Unlock()
	if err := os.WriteFile(artifactPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := tamperSvc.ApplyUpdate(context.Background(), false)
	if err == nil || st.State != UpdateError || st.ErrorCode != "update_checksum_mismatch" || st.FallbackPath != "" {
		t.Fatalf("tampered pre-apply file was not rejected: state=%+v err=%v", st, err)
	}
	if entries, _ := os.ReadDir(stagingRoot); len(entries) != 0 {
		t.Fatalf("tampered staging dir not deleted: %v", entries)
	}

	// 2. During UpdateApplying: new downloads/runs are blocked, late CancelUpdate is ignored, and admission restores on error.
	enteredApply := make(chan struct{})
	releaseApply := make(chan struct{})
	applySvc := newUpdateTestService(t, UpdateOptions{
		APIURL:              srv.URL + "/latest",
		CurrentVersion:      "0.9.9",
		GOOS:                "linux",
		GOARCH:              "amd64",
		StagingRoot:         filepath.Join(t.TempDir(), "staging2"),
		DisableAutoDownload: true,
		InspectTarget:       func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
		ApplyFunc: func(_ context.Context, plan platformApplyPlan) error {
			close(enteredApply)
			<-releaseApply
			if _, statErr := os.Stat(plan.ArtifactPath); statErr != nil {
				return statErr
			}
			return &updateCheckError{code: "update_apply_failed", message: "simulated fail"}
		},
	})
	applySvc.settings.ModelsRoot = ptr(t.TempDir())
	applySvc.settings.ResultsRoot = ptr(t.TempDir())
	applySvc.settings.InferyardPath = ptr(os.Args[0])
	applySvc.exec = func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
		}
		return inferyard.Execution{ExitCode: 0}, nil
	}
	if _, err := applySvc.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := applySvc.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	applyDone := make(chan error, 1)
	go func() {
		_, applyErr := applySvc.ApplyUpdate(context.Background(), false)
		applyDone <- applyErr
	}()
	<-enteredApply

	if cancelled := applySvc.CancelUpdate(); cancelled.State != UpdateApplying {
		t.Fatalf("late CancelUpdate altered applying state: %+v", cancelled)
	}
	if _, apiErr := applySvc.CreateModelDownload(CreateDownloadRequest{SourceUrl: DefaultModelURL()}, ""); apiErr == nil || apiErr.Code != "conflict" || apiErr.Details["reason"] != "update_applying" {
		t.Fatalf("expected download blocked with update_applying, got %+v", apiErr)
	}
	applySvc.mu.Lock()
	runErr := applySvc.admitRunLocked()
	applySvc.mu.Unlock()
	if runErr == nil || runErr.Details["reason"] != "update_applying" {
		t.Fatalf("expected run admission blocked with update_applying, got %+v", runErr)
	}

	close(releaseApply)
	if err := <-applyDone; err == nil {
		t.Fatal("expected simulated apply failure")
	}
	applySvc.mu.Lock()
	restoredErr := applySvc.admitRunLocked()
	applySvc.mu.Unlock()
	if restoredErr != nil {
		t.Fatalf("admission not restored after failed apply: %+v", restoredErr)
	}

	// 3. Windows failed-apply error record (with UTF-8 BOM) is consumed and surfaced on next startup.
	winStaging := filepath.Join(t.TempDir(), "win-staging")
	if err := os.MkdirAll(winStaging, 0o700); err != nil {
		t.Fatal(err)
	}
	errPayload := append([]byte("\xEF\xBB\xBF"), []byte(`{"version":"0.10.0","release_url":"`+srv.URL+`/releases/v0.10.0","exit_code":1}`)...)
	errFile := filepath.Join(winStaging, updateErrorRecordFile)
	if err := os.WriteFile(errFile, errPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	winSvc := newUpdateTestService(t, UpdateOptions{
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		StagingRoot:    winStaging,
	})
	winSvc.StartBackgroundUpdateCheck()
	got := winSvc.UpdateStatus()
	if got.State != UpdateError || got.ErrorCode != "update_windows_install_failed" || got.Release == nil || got.Release.ReleaseURL == "" {
		t.Fatalf("expected pending Windows apply error surfaced, got %+v", got)
	}
	if _, statErr := os.Stat(errFile); !os.IsNotExist(statErr) {
		t.Fatalf("error record file was not deleted after consumption: %v", statErr)
	}
}

func TestUpdateOverlappingApplyKeepsAdmissionFrozen(t *testing.T) {
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

	enteredApply := make(chan struct{})
	releaseApply := make(chan struct{})
	var applyCalls atomic.Int32
	svc := newUpdateTestService(t, UpdateOptions{
		APIURL:              srv.URL + "/latest",
		CurrentVersion:      "0.9.9",
		GOOS:                "linux",
		GOARCH:              "amd64",
		DisableAutoDownload: true,
		InspectTarget:       func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
		ApplyFunc: func(_ context.Context, _ platformApplyPlan) error {
			if applyCalls.Add(1) == 1 {
				close(enteredApply)
				<-releaseApply
			}
			return &updateCheckError{code: "update_apply_failed", message: "simulated fail"}
		},
	})
	svc.settings.ModelsRoot = ptr(t.TempDir())
	svc.settings.ResultsRoot = ptr(t.TempDir())
	svc.settings.InferyardPath = ptr(os.Args[0])
	svc.exec = func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if len(args) > 0 && args[0] == "--version" {
			return inferyard.Execution{ExitCode: 0, Stdout: []byte("inferyard 1.0 source=release\n")}, nil
		}
		return inferyard.Execution{ExitCode: 0}, nil
	}

	// Part A: Service admission lock rejects a second overlapping beginUpdateApplyAdmission without releasing the first.
	if _, apiErr, admitted := svc.beginUpdateApplyAdmission(false); !admitted || apiErr != nil {
		t.Fatalf("first admission failed: admitted=%v err=%+v", admitted, apiErr)
	}
	if _, apiErr, admitted := svc.beginUpdateApplyAdmission(false); admitted || apiErr == nil || apiErr.Details["reason"] != "update_applying" {
		t.Fatalf("second overlapping admission should conflict cleanly, got admitted=%v err=%+v", admitted, apiErr)
	}
	if _, apiErr := svc.CreateModelDownload(CreateDownloadRequest{SourceUrl: DefaultModelURL()}, ""); apiErr == nil || apiErr.Details["reason"] != "update_applying" {
		t.Fatalf("download admission should stay frozen during first apply: %+v", apiErr)
	}
	svc.endUpdateApplyAdmission()

	// Part B: Two concurrent ApplyUpdate calls + a third overlapping call while ApplyFunc is running keep download admission blocked.
	if _, err := svc.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DownloadUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.ApplyUpdate(context.Background(), false)
			errCh <- err
		}()
	}
	close(start)
	<-enteredApply

	// The losing concurrent ApplyUpdate must fail without unfreezing admission.
	loserErr := <-errCh
	if loserErr == nil {
		t.Fatal("expected losing concurrent ApplyUpdate to fail with busy/conflict")
	}
	// Deterministic overlapping call while winner is still in ApplyFunc must also be rejected and preserve lock.
	if _, err := svc.ApplyUpdate(context.Background(), false); err == nil {
		t.Fatal("expected overlapping ApplyUpdate during active apply to fail")
	}
	if _, apiErr := svc.CreateModelDownload(CreateDownloadRequest{SourceUrl: DefaultModelURL()}, ""); apiErr == nil || apiErr.Details["reason"] != "update_applying" {
		t.Fatalf("download was unexpectedly admitted during active apply after overlapping ApplyUpdate: %+v", apiErr)
	}
	if w := svc.ActiveUpdateWorkloads(); w.ActiveDownloads != 0 {
		t.Fatalf("expected zero active downloads, got %+v", w)
	}

	close(releaseApply)
	if winnerErr := <-errCh; winnerErr == nil {
		t.Fatal("expected simulated apply error from winner")
	}
	if got := applyCalls.Load(); got != 1 {
		t.Fatalf("expected ApplyFunc called once, got %d", got)
	}
}
