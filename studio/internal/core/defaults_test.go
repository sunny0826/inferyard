package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func engineTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if strings.HasSuffix(name, "/") {
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
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

func TestValidEngineSource(t *testing.T) {
	if !validEngineSource(DefaultEngineURL()) {
		t.Fatal("default engine URL must be valid")
	}
	for _, bad := range []string{"http://x/a.tgz", "https://x/a.tgz?q=1", "https://user@x/a.tgz", "https://x/../a.tgz", "ftp://x/a"} {
		if validEngineSource(bad) {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestFetchEngineRejectsTraversal(t *testing.T) {
	stubInstall(t)
	archive := engineTarGz(t, map[string]string{"../escape": "x"})
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	}).Do
	s := installService(t)
	_, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", filepath.Join(t.TempDir(), "engine-new"), func(string) {})
	if e == nil || e.Code != "engine_unpack_failed" {
		t.Fatalf("err = %+v", e)
	}
}

func TestFetchEngineRejectsBadInput(t *testing.T) {
	s := installService(t)
	if _, e := s.FetchEngine(context.Background(), "http://insecure/a.tgz", "/tmp/x", func(string) {}); e == nil || e.Code != "invalid_request" {
		t.Fatalf("source: %+v", e)
	}
	if _, e := s.FetchEngine(context.Background(), "https://x/a.tgz", "relative", func(string) {}); e == nil || e.Code != "invalid_request" {
		t.Fatalf("outDir: %+v", e)
	}
}

func engineTarGzWithLink(t *testing.T, link, target string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\n"
	for _, h := range []*tar.Header{
		{Name: "dist/" + engineBinaryName(), Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg},
		{Name: "dist/libimpl.0.dylib", Mode: 0644, Size: 3, Typeflag: tar.TypeReg},
		{Name: "dist/" + link, Mode: 0755, Typeflag: tar.TypeSymlink, Linkname: target},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			content := body
			if h.Name == "dist/libimpl.0.dylib" {
				content = "lib"
			}
			if _, err := tw.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
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

// TestFetchEngineStreamsAnArchiveLargerThanTheInMemoryLimit pins the reason
// the archive is written to disk: a pinned archive well past the old 64 MiB
// in-memory ceiling has to reach the disk and unpack.
func TestFetchEngineStreamsAnArchiveLargerThanTheInMemoryLimit(t *testing.T) {
	stubInstall(t)
	// Random bytes, so the compressed archive stays near the raw size: the
	// body really is larger than the old in-memory ceiling.
	big := make([]byte, maxFetchBytes+1)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	archive := engineTarGz(t, map[string]string{
		"llama-prism/" + engineBinaryName(): "#!/bin/sh\n",
		"llama-prism/libimpl.dylib":         string(big),
	})
	if int64(len(archive)) <= maxFetchBytes {
		t.Fatalf("the fixture is not larger than the in-memory limit: %d", len(archive))
	}
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	}).Do
	s := installService(t)
	out := filepath.Join(t.TempDir(), "engine-new")
	path, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", out, func(string) {})
	if e != nil {
		t.Fatalf("FetchEngine: %+v", e)
	}
	if filepath.Base(path) != engineBinaryName() {
		t.Fatalf("path = %q", path)
	}
	if _, err := os.Stat(filepath.Join(out, "llama-prism", "libimpl.dylib")); err != nil {
		t.Fatal("sibling library missing:", err)
	}
	// The staging directory must not be left behind next to the output.
	entries, err := os.ReadDir(filepath.Dir(out))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".engine-download-") {
			t.Fatal("the staging directory was left behind", entry.Name())
		}
	}
}

// TestFetchEngineFetchesTheEngineBinaryItself covers the plain-file shape:
// the engine binary alone is a valid engine source.
func TestFetchEngineFetchesTheEngineBinaryItself(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("#!/bin/sh\n"))}, nil
	}).Do
	s := installService(t)
	out := filepath.Join(t.TempDir(), "engine-new")
	path, e := s.FetchEngine(context.Background(), "https://example.test/"+engineBinaryName(), out, func(string) {})
	if e != nil {
		t.Fatalf("FetchEngine: %+v", e)
	}
	if path != filepath.Join(out, engineBinaryName()) {
		t.Fatalf("path = %q", path)
	}
}

// TestFetchEngineRejectsAPlainFileThatIsNotTheEngine keeps the engine
// channel from registering a wheel or a runtime archive as an engine.
func TestFetchEngineRejectsAPlainFileThatIsNotTheEngine(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("wheel"))}, nil
	}).Do
	s := installService(t)
	out := filepath.Join(t.TempDir(), "engine-new")
	_, e := s.FetchEngine(context.Background(), "https://example.test/mlx_lm-0.32.2-py3-none-any.whl", out, func(string) {})
	if e == nil || e.Code != "engine_binary_missing" {
		t.Fatalf("err = %+v", e)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("a rejected download left an output directory behind")
	}
}

func writeEngineBinary(t *testing.T, dir string, mode os.FileMode, modTime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "llama-server")
	if err := os.WriteFile(path, []byte("bin"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return path
}
