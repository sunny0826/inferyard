package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadLogTailNormalizesCRLFAndFiltersEmptyTrailingLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "engine.log")
	content := "line 1\r\n\r\n  line 2\r\nline 3\r\n\r\n   \r\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLogTail(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := "  line 2\nline 3"
	if got != want {
		t.Fatalf("tail = %q, want %q", got, want)
	}
}

func TestReadLogTailDefaultsToTwentyLinesAndCapsAt64KiB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")

	var b strings.Builder
	// Write > 100 KiB of prefix lines before the trailing 25 lines.
	prefixLine := strings.Repeat("A", 120) + "\n"
	for i := 0; i < 900; i++ {
		b.WriteString(prefixLine)
	}
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&b, "tail-%02d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLogTail(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 20 {
		t.Fatalf("len(lines) = %d, want 20 (%q)", len(lines), got)
	}
	if lines[0] != "tail-06" || lines[19] != "tail-25" {
		t.Fatalf("unexpected tail range: first=%q last=%q", lines[0], lines[19])
	}
}

func TestReadLogTailValidation(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadLogTail("", 20); err == nil {
		t.Fatal("expected error for empty path")
	}
	if _, err := ReadLogTail("bad\x00path", 20); err == nil {
		t.Fatal("expected error for NUL byte path")
	}
	if _, err := ReadLogTail(dir, 20); err == nil {
		t.Fatal("expected error for directory path")
	}
	emptyFile := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(emptyFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLogTail(emptyFile, 20)
	if err != nil || got != "" {
		t.Fatalf("empty file = (%q, %v), want (\"\", nil)", got, err)
	}
}

func TestServiceEngineLogTailWorkspaceBoundary(t *testing.T) {
	ws := t.TempDir()
	logsDir := filepath.Join(ws, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(logsDir, "engine-1.log")
	if err := os.WriteFile(inside, []byte("boot\nready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(ws, "outside.log")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := engineService(t, ws)
	// Absolute path inside <workspace>/logs
	got, err := s.EngineLogTail(inside, 20)
	if err != nil || got != "boot\nready" {
		t.Fatalf("inside abs = (%q, %v)", got, err)
	}
	// Relative filename inside <workspace>/logs (e.g. orphaned engine owner log_file)
	got, err = s.EngineLogTail("engine-1.log", 20)
	if err != nil || got != "boot\nready" {
		t.Fatalf("inside rel = (%q, %v)", got, err)
	}
	// Paths outside <workspace>/logs must be rejected
	if _, err := s.EngineLogTail(outside, 20); err == nil {
		t.Fatal("expected error for file outside workspace logs")
	}
	if _, err := s.EngineLogTail("../outside.log", 20); err == nil {
		t.Fatal("expected error for traversal outside workspace logs")
	}
	if _, err := s.EngineLogTail("", 20); err == nil {
		t.Fatal("expected error for empty path")
	}

	// Symlink escaping <workspace>/logs must also be rejected
	escapeLink := filepath.Join(logsDir, "escape.log")
	if err := os.Symlink(outside, escapeLink); err == nil {
		if _, err := s.EngineLogTail(escapeLink, 20); err == nil {
			t.Fatal("expected error for symlink escaping workspace logs")
		}
	}
}

func TestServiceEngineLogTailWithoutConfiguredWorkspace(t *testing.T) {
	s, err := New(context.Background(), Options{SettingsPath: filepath.Join(t.TempDir(), "settings.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	logFile := filepath.Join(t.TempDir(), "engine.log")
	if err := os.WriteFile(logFile, []byte("line-a\nline-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.EngineLogTail(logFile, 20)
	if err != nil || got != "line-a\nline-b" {
		t.Fatalf("unconfigured workspace tail = (%q, %v)", got, err)
	}
}

func TestOpenPathInOSUsesReplaceableLauncher(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "engine.log")
	if err := os.WriteFile(logFile, []byte("ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := openPathFunc
	t.Cleanup(func() { openPathFunc = orig })

	var calledCmd string
	var calledArgs []string
	openPathFunc = func(cmd string, args ...string) error {
		calledCmd = cmd
		calledArgs = append([]string(nil), args...)
		return nil
	}

	if err := OpenPathInOS(logFile); err != nil {
		t.Fatal(err)
	}
	wantCmd, err := openOSCommand(runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if calledCmd != wantCmd || len(calledArgs) != 1 || calledArgs[0] != logFile {
		t.Fatalf("openPathFunc(%q, %v), want (%q, [%q])", calledCmd, calledArgs, wantCmd, logFile)
	}

	if err := OpenPathInOS(""); err == nil {
		t.Fatal("expected error for empty path")
	}
	if err := OpenPathInOS(filepath.Join(t.TempDir(), "missing.log")); err == nil {
		t.Fatal("expected error for missing file")
	}

	for goos, want := range map[string]string{
		"darwin":  "open",
		"windows": "explorer",
		"linux":   "xdg-open",
	} {
		got, err := openOSCommand(goos)
		if err != nil || got != want {
			t.Fatalf("openOSCommand(%q) = (%q, %v), want %q", goos, got, err, want)
		}
	}
}
