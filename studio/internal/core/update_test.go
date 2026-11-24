package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newUpdateTestService(t *testing.T, opts UpdateOptions) *Service {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if opts.StagingRoot == "" {
		opts.StagingRoot = filepath.Join(t.TempDir(), "staging")
	}
	if opts.StartupDelay == 0 {
		opts.StartupDelay = 5 * time.Millisecond
	}
	if opts.CheckTimeout == 0 {
		opts.CheckTimeout = 5 * time.Second
	}
	opts.AllowInsecureHTTP = true
	s, err := New(ctx, Options{
		SettingsPath:  filepath.Join(t.TempDir(), "settings.json"),
		HostStatePath: filepath.Join(t.TempDir(), "host.json"),
		Update:        opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func buildTestTarGz(t *testing.T, files map[string][]byte, symlinks map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, "inferyard-studio") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range symlinks {
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeSymlink,
			Linkname: target,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUpdateDownloadVerifyChecksumRejectionCancelAndRetry(t *testing.T) {
	goodPayload := bytes.Repeat([]byte("inferyard-binary-payload-"), 4096)
	badPayload := append([]byte(nil), goodPayload...)
	badPayload[42] ^= 0xff

	var serveCorrupted atomic.Bool
	var slowDownload atomic.Bool
	serveCorrupted.Store(true)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(githubReleasePayload{
				TagName: "v0.10.0",
				HTMLURL: srv.URL + "/releases/v0.10.0",
				Assets: []githubReleaseAsset{
					{Name: "inferyard-studio-v0.10.0-linux-amd64.tar.gz", BrowserDownloadURL: srv.URL + "/asset", Size: int64(len(goodPayload))},
					{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/SHA256SUMS", Size: 120},
				},
			})
		case "/asset":
			payload := goodPayload
			if serveCorrupted.Load() {
				payload = badPayload
			}
			w.Header().Set("Content-Length", "102400")
			if slowDownload.Load() {
				_, _ = w.Write(payload[:1024])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				<-r.Context().Done()
				return
			}
			_, _ = w.Write(payload)
		case "/SHA256SUMS":
			_, _ = w.Write([]byte(sha256Hex(goodPayload) + "  inferyard-studio-v0.10.0-linux-amd64.tar.gz\n"))
		}
	}))
	defer srv.Close()

	stagingRoot := filepath.Join(t.TempDir(), "staging")
	s := newUpdateTestService(t, UpdateOptions{
		APIURL:              srv.URL + "/latest",
		CurrentVersion:      "0.9.9",
		GOOS:                "linux",
		GOARCH:              "amd64",
		StagingRoot:         stagingRoot,
		DisableAutoDownload: true,
		InspectTarget:       func() updateInstallTarget { return updateInstallTarget{CanSelfUpdate: true, ExePath: "/opt/inferyard/inferyard-studio"} },
	})

	if _, err := s.CheckForUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Corrupted payload must fail SHA-256 verification and delete all staging files.
	st, err := s.DownloadUpdate(context.Background())
	if err == nil || st.State != UpdateError || st.ErrorCode != "update_checksum_mismatch" || st.FallbackPath != "" {
		t.Fatalf("expected checksum mismatch with empty fallback, got state=%+v err=%v", st, err)
	}
	if entries, _ := os.ReadDir(stagingRoot); len(entries) != 0 {
		t.Fatalf("corrupted staging files were not deleted: %v", entries)
	}

	// Slow download cancellation cleans up staging and returns to available.
	serveCorrupted.Store(false)
	slowDownload.Store(true)
	dlDone := make(chan struct{})
	go func() {
		_, _ = s.DownloadUpdate(context.Background())
		close(dlDone)
	}()
	wait(t, func() bool { return s.UpdateStatus().State == UpdateDownloading })
	cancelled := s.CancelUpdate()
	<-dlDone
	if cancelled.State != UpdateAvailable {
		t.Fatalf("cancel expected available state, got %+v", cancelled)
	}
	if entries, _ := os.ReadDir(stagingRoot); len(entries) != 0 {
		t.Fatalf("cancelled staging files were not deleted: %v", entries)
	}

	// Retry succeeds and transitions to ready_to_apply.
	slowDownload.Store(false)
	st, err = s.DownloadUpdate(context.Background())
	if err != nil || st.State != UpdateReadyToApply || st.DownloadedBytes != int64(len(goodPayload)) {
		t.Fatalf("retry download failed: state=%+v err=%v", st, err)
	}
}
