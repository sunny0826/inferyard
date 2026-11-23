package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		StagingRoot:    stagingRoot,
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
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
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		StagingRoot:    filepath.Join(t.TempDir(), "staging2"),
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
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
