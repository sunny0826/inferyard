package inferyard

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type prependedPathKey struct{}

// WithPrependedPath attaches directories to prepend to PATH when executing CLI commands.
func WithPrependedPath(ctx context.Context, dirs ...string) context.Context {
	merged := cleanUniqueDirs(append(append([]string(nil), dirs...), PrependedPaths(ctx)...))
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, prependedPathKey{}, merged)
}

// PrependedPaths returns a copy of extra PATH directories attached to ctx.
func PrependedPaths(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	dirs, ok := ctx.Value(prependedPathKey{}).([]string)
	if !ok || len(dirs) == 0 {
		return nil
	}
	return append([]string(nil), dirs...)
}

// PathContainsDir reports whether pathEnv already lists dir.
func PathContainsDir(pathEnv, dir string) bool {
	cleaned := strings.TrimSpace(dir)
	if cleaned == "" {
		return false
	}
	target := filepath.Clean(cleaned)
	for _, entry := range filepath.SplitList(pathEnv) {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		if samePathEntry(filepath.Clean(entry), target) {
			return true
		}
	}
	return false
}

// MergePrependedPath returns a copy of environ with dirs prepended to PATH without duplicates.
func MergePrependedPath(environ []string, dirs []string) []string {
	cleanDirs := cleanUniqueDirs(dirs)
	out := append([]string(nil), environ...)
	if len(cleanDirs) == 0 {
		return out
	}

	pathIdx := -1
	pathKey := "PATH"
	existingPath := ""
	for i, entry := range out {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if isPathKey(k) {
			pathIdx = i
			pathKey = k
			existingPath = v
			break
		}
	}

	combined := make([]string, 0, len(cleanDirs)+4)
	for _, d := range cleanDirs {
		combined = appendUniqueDir(combined, d)
	}
	for _, entry := range filepath.SplitList(existingPath) {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		combined = appendUniqueDir(combined, filepath.Clean(entry))
	}
	mergedEntry := pathKey + "=" + strings.Join(combined, string(os.PathListSeparator))
	if pathIdx >= 0 {
		out[pathIdx] = mergedEntry
	} else {
		out = append(out, mergedEntry)
	}
	return out
}

func cleanUniqueDirs(dirs []string) []string {
	var out []string
	for _, dir := range dirs {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			continue
		}
		out = appendUniqueDir(out, filepath.Clean(trimmed))
	}
	return out
}

func appendUniqueDir(list []string, dir string) []string {
	for _, existing := range list {
		if samePathEntry(existing, dir) {
			return list
		}
	}
	return append(list, dir)
}

func samePathEntry(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func isPathKey(key string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(key, "PATH")
	}
	return key == "PATH"
}
