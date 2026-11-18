package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"

	"os"
	"path/filepath"

	"testing"
	"time"
)

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
