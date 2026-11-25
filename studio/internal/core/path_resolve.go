package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ptr[T any](v T) *T { return &v }

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return hex.EncodeToString(b[:])
}

func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var c T
	_ = json.Unmarshal(raw, &c)
	return c
}

func Resolve(root, path string) (string, error) {
	if root == "" || path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	root, e := canonical(root)
	if e != nil {
		return "", e
	}
	path, e = canonical(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("outside root")
	}
	return path, nil
}

func canonical(p string) (string, error) {
	p = filepath.Clean(p)
	resolved, e := filepath.EvalSymlinks(p)
	if e == nil {
		return resolved, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	parent := filepath.Dir(p)
	if parent == p {
		return "", e
	}
	r, e := canonical(parent)
	if e != nil {
		return "", e
	}
	return filepath.Join(r, filepath.Base(p)), nil
}
