package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func TestDefaultEngineURLPlatforms(t *testing.T) {
	u := DefaultEngineURL()
	if runtime.GOOS+"/"+runtime.GOARCH == "darwin/arm64" && !strings.HasSuffix(u, "macos-arm64.tar.gz") {
		t.Fatalf("DefaultEngineURL = %q", u)
	}
	if u != "" && !strings.HasPrefix(u, engineReleaseBase) {
		t.Fatalf("DefaultEngineURL outside pinned release: %q", u)
	}
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

func TestFetchEngineUnpacksArchive(t *testing.T) {
	stubInstall(t)
	archive := engineTarGz(t, map[string]string{
		"llama-prism/":                    "",
		"llama-prism/" + engineBinaryName(): "#!/bin/sh\n",
		"llama-prism/libimpl.dylib":        "lib",
	})
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	}).Do
	s := installService(t)
	out := filepath.Join(t.TempDir(), "engine-new")
	statuses := []string{}
	path, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", out, func(st string) { statuses = append(statuses, st) })
	if e != nil {
		t.Fatalf("FetchEngine: %+v", e)
	}
	if filepath.Base(path) != engineBinaryName() {
		t.Fatalf("path = %q", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0755 {
		t.Fatalf("mode = %v", st.Mode())
	}
	if _, err := os.Stat(filepath.Join(out, "llama-prism", "libimpl.dylib")); err != nil {
		t.Fatal("sibling library missing:", err)
	}
	if len(statuses) == 0 {
		t.Fatal("no status lines")
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

func TestFetchEngineMissingBinary(t *testing.T) {
	stubInstall(t)
	archive := engineTarGz(t, map[string]string{"README.txt": "no engine here"})
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive))}, nil
	}).Do
	s := installService(t)
	_, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", filepath.Join(t.TempDir(), "engine-new"), func(string) {})
	if e == nil || e.Code != "engine_binary_missing" {
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

func TestFetchEngineRecreatesSafeSymlinks(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(engineTarGzWithLink(t, "libimpl.dylib", "libimpl.0.dylib")))}, nil
	}).Do
	s := installService(t)
	out := filepath.Join(t.TempDir(), "engine-new")
	if _, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", out, func(string) {}); e != nil {
		t.Fatalf("FetchEngine: %+v", e)
	}
	target, err := os.Readlink(filepath.Join(out, "dist", "libimpl.dylib"))
	if err != nil {
		t.Fatal("symlink missing:", err)
	}
	if target != "libimpl.0.dylib" {
		t.Fatalf("link target = %q", target)
	}
}

func TestFetchEngineRejectsEscapingSymlink(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(engineTarGzWithLink(t, "evil", "../../etc/passwd")))}, nil
	}).Do
	s := installService(t)
	_, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", filepath.Join(t.TempDir(), "engine-new"), func(string) {})
	if e == nil || e.Code != "engine_unpack_failed" {
		t.Fatalf("err = %+v", e)
	}
}

func TestFetchEngineOutputExists(t *testing.T) {
	stubInstall(t)
	installHTTPDo = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(engineTarGz(t, map[string]string{"a": "b"})))}, nil
	}).Do
	s := installService(t)
	out := t.TempDir()
	_, e := s.FetchEngine(context.Background(), "https://example.test/llama.tar.gz", out, func(string) {})
	if e == nil || e.Code != "output_exists" {
		t.Fatalf("err = %+v", e)
	}
}

