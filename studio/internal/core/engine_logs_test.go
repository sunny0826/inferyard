package core

import (
	"os"
	"path/filepath"
	"testing"
)

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
