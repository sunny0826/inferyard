package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func TestCorruptSettingsUseDefaults(t *testing.T) {
	for _, raw := range []string{`{`, `{"inferyard_path":"/must-not-survive","models_root":42}`, `[]`} {
		t.Run(fmt.Sprintf("case_%d", len(raw)), func(t *testing.T) {
			logs := captureLogs(t)
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := New(ctx, Options{SettingsPath: path})
			if err != nil {
				t.Fatal(err)
			}
			if got := s.userSettings(); !reflect.DeepEqual(got, downloadNameDefaults(UserSettings{})) {
				t.Fatalf("partial settings survived: %+v", got)
			}
			if !strings.Contains(logs.String(), "using defaults") || strings.Contains(logs.String(), raw) {
				t.Fatal(logs.String())
			}
			saved, err := os.ReadFile(path)
			if err != nil || string(saved) != raw {
				t.Fatal("startup overwrote corrupt settings")
			}
			if _, err := s.UpdateSettings(map[string]json.RawMessage{}); err != nil {
				t.Fatal(err)
			}
			saved, err = os.ReadFile(path)
			if err != nil || !json.Valid(saved) {
				t.Fatal("cannot save after recovery", err)
			}
		})
	}
}

func TestSettingsExistingDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	s := testService(t, nil)
	dir := filepath.Dir(s.settingsPath)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettings(map[string]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, s.settingsPath: 0600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s: %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}
