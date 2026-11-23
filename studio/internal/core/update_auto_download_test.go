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
)

func TestUpdateBackgroundAutoDownloadAndSilentFailureDegradation(t *testing.T) {
	payload := buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.10.0-linux-amd64/inferyard-studio": []byte("new-bin"),
	}, nil)
	var failAsset atomic.Bool
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
			if failAsset.Load() {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write(payload)
		case "/SHA256SUMS":
			_, _ = w.Write([]byte(sha256Hex(payload) + "  inferyard-studio-v0.10.0-linux-amd64.tar.gz\n"))
		}
	}))
	defer srv.Close()

	// 1. Background startup check automatically downloads & verifies to ready_to_apply.
	autoSvc := newUpdateTestService(t, UpdateOptions{
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
	})
	autoSvc.StartBackgroundUpdateCheck()
	wait(t, func() bool { return autoSvc.UpdateStatus().State == UpdateReadyToApply })
	if st := autoSvc.UpdateStatus(); st.Release == nil || st.Release.Version != "0.10.0" {
		t.Fatalf("unexpected ready state after background auto-download: %+v", st)
	}

	// 2. Failing background auto-download cleans staging and degrades silently to available (no error state).
	failAsset.Store(true)
	failStaging := filepath.Join(t.TempDir(), "fail-staging")
	failSvc := newUpdateTestService(t, UpdateOptions{
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		StagingRoot:    failStaging,
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
	})
	_, off, cursor := failSvc.Bus.Subscribe()
	defer off()
	sawDownload := false
	failSvc.StartBackgroundUpdateCheck()
	wait(t, func() bool {
		events, _, next := failSvc.Bus.After(cursor)
		cursor = next
		for _, e := range events {
			if e.Type == "update.state" {
				var p struct {
					Update UpdateState `json:"update"`
				}
				if json.Unmarshal(e.Data, &p) == nil && p.Update.State == UpdateDownloading {
					sawDownload = true
				}
			}
		}
		return sawDownload && failSvc.UpdateStatus().State == UpdateAvailable
	})
	if st := failSvc.UpdateStatus(); st.ErrorCode != "" || st.ErrorMessage != "" {
		t.Fatalf("background auto-download failure leaked error: %+v", st)
	}
	if entries, _ := os.ReadDir(failStaging); len(entries) != 0 {
		t.Fatalf("failed auto-download staging not cleaned: %v", entries)
	}
}

func TestUpdateManualCheckAutoDownloadsAndPreservesReadyArtifact(t *testing.T) {
	payload10 := buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.10.0-linux-amd64/inferyard-studio": []byte("bin-0.10.0"),
	}, nil)
	payload11 := buildTestTarGz(t, map[string][]byte{
		"inferyard-studio-v0.11.0-linux-amd64/inferyard-studio": []byte("bin-0.11.0"),
	}, nil)
	var mode atomic.Int32 // 0: 503, 1: v0.10.0, 2: v0.11.0
	var assetHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			switch mode.Load() {
			case 0:
				w.WriteHeader(http.StatusServiceUnavailable)
			case 1:
				_ = json.NewEncoder(w).Encode(githubReleasePayload{
					TagName: "v0.10.0",
					HTMLURL: srv.URL + "/releases/v0.10.0",
					Assets: []githubReleaseAsset{
						{Name: "inferyard-studio-v0.10.0-linux-amd64.tar.gz", BrowserDownloadURL: srv.URL + "/asset-10", Size: int64(len(payload10))},
						{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS", Size: 120},
					},
				})
			case 2:
				_ = json.NewEncoder(w).Encode(githubReleasePayload{
					TagName: "v0.11.0",
					HTMLURL: srv.URL + "/releases/v0.11.0",
					Assets: []githubReleaseAsset{
						{Name: "inferyard-studio-v0.11.0-linux-amd64.tar.gz", BrowserDownloadURL: srv.URL + "/asset-11", Size: int64(len(payload11))},
						{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS", Size: 120},
					},
				})
			}
		case "/asset-10":
			assetHits.Add(1)
			_, _ = w.Write(payload10)
		case "/asset-11":
			assetHits.Add(1)
			_, _ = w.Write(payload11)
		case "/SHA256SUMS":
			_, _ = w.Write([]byte(sha256Hex(payload10) + "  inferyard-studio-v0.10.0-linux-amd64.tar.gz\n" + sha256Hex(payload11) + "  inferyard-studio-v0.11.0-linux-amd64.tar.gz\n"))
		}
	}))
	defer srv.Close()

	svc := newUpdateTestService(t, UpdateOptions{
		APIURL:         srv.URL + "/latest",
		CurrentVersion: "0.9.9",
		GOOS:           "linux",
		GOARCH:         "amd64",
		InspectTarget:  func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard-studio"} },
	})

	// 1. Startup 503 stays idle without hitting /asset.
	_, off, cursor := svc.Bus.Subscribe()
	defer off()
	sawChecking := false
	svc.StartBackgroundUpdateCheck()
	wait(t, func() bool {
		events, _, next := svc.Bus.After(cursor)
		cursor = next
		for _, e := range events {
			if e.Type == "update.state" && json.Valid(e.Data) {
				var p struct{ Update UpdateState `json:"update"` }
				if json.Unmarshal(e.Data, &p) == nil && p.Update.State == UpdateChecking {
					sawChecking = true
				}
			}
		}
		return sawChecking && svc.UpdateStatus().State == UpdateIdle
	})
	if assetHits.Load() != 0 {
		t.Fatalf("expected 0 asset hits after 503 startup check, got %d", assetHits.Load())
	}

	// 2. Manual check discovers v0.10.0 and automatically downloads into ready_to_apply.
	mode.Store(1)
	if _, err := svc.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return svc.UpdateStatus().State == UpdateReadyToApply })
	if assetHits.Load() != 1 {
		t.Fatalf("expected 1 asset hit after manual check auto-download, got %d", assetHits.Load())
	}

	// 3. Re-checking the same v0.10.0 preserves ready_to_apply without re-downloading.
	recheckSt, err := svc.CheckForUpdate(context.Background())
	if err != nil || recheckSt.State != UpdateReadyToApply || recheckSt.Release == nil || recheckSt.Release.Version != "0.10.0" {
		t.Fatalf("same-version re-check regressed ready state: state=%+v err=%v", recheckSt, err)
	}
	if assetHits.Load() != 1 {
		t.Fatalf("same-version re-check triggered unexpected re-download: hits=%d", assetHits.Load())
	}

	// 4. Newer release v0.11.0 supersedes v0.10.0 and auto-downloads to ready_to_apply.
	mode.Store(2)
	if _, err := svc.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool {
		st := svc.UpdateStatus()
		return st.State == UpdateReadyToApply && st.Release != nil && st.Release.Version == "0.11.0"
	})
	if assetHits.Load() != 2 {
		t.Fatalf("expected 2 total asset hits for v0.11.0 auto-download, got %d", assetHits.Load())
	}
}
