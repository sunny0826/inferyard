package core

import (
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/inferyard"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDownloadNamesSettings(t *testing.T) {
	s := testService(t, func(_ context.Context, _ string, args ...string) (inferyard.Execution, error) {
		v, _ := version(args)
		return v, nil
	})
	if s.Settings().Settings.ModelDownloadName != "model" || s.Settings().Settings.EngineDownloadName != "engine" {
		t.Fatal(s.Settings())
	}
	for _, bad := range []string{`""`, `"..."`, `"a/b"`, `"a b"`, "null", "true", "42"} {
		for _, key := range []string{"model_download_name", "engine_download_name"} {
			if _, e := s.UpdateSettings(map[string]json.RawMessage{key: []byte(bad)}); e == nil {
				t.Fatal(key, bad)
			}
		}
	}
	if _, e := s.UpdateSettings(map[string]json.RawMessage{"model_download_name": []byte(`"custom"`), "engine_download_name": []byte(`"runtime"`)}); e != nil {
		t.Fatal(e)
	}
	raw, err := os.ReadFile(s.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved UserSettings
	if json.Unmarshal(raw, &saved) != nil || saved.ModelDownloadName != "custom" || saved.EngineDownloadName != "runtime" {
		t.Fatal(string(raw))
	}
	for path, mode := range map[string]os.FileMode{s.settingsPath: 0600, filepath.Dir(s.settingsPath): 0700} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != mode {
			t.Fatal(path, st, err)
		}
	}
}

func TestModelAutomaticNamesAndToken(t *testing.T) {
	calls := make(chan []string, 4)
	s := testService(t, func(ctx context.Context, _ string, args ...string) (inferyard.Execution, error) {
		if v, ok := version(args); ok {
			return v, nil
		}
		calls <- args
		<-ctx.Done()
		return result(130), ctx.Err()
	})
	t.Cleanup(func() { s.Shutdown(context.Background()) })
	root := t.TempDir()
	s.settings.ModelsRoot = &root
	s.settings.ModelDownloadName = "weights"
	if err := os.WriteFile(filepath.Join(root, "weights"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "weights-2")); err != nil {
		t.Fatal(err)
	}
	a, e := s.CreateModelDownload(CreateDownloadRequest{SourceUrl: DefaultModelURL}, "HF_TOKEN")
	if e != nil || a.OutName != "weights-3" {
		t.Fatal(a, e)
	}
	args := <-calls
	i := slices.Index(args, "--token-env")
	if i < 0 || args[i+1] != "HF_TOKEN" {
		t.Fatal(args)
	}
	if _, e := s.CancelDownload(a.TaskId); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return s.Downloads()[0].State == "cancelled" })
	b, e := s.CreateDownload(CreateDownloadRequest{SourceUrl: DefaultModelURL})
	if e != nil || b.OutName != "weights-4" {
		t.Fatal(b, e)
	}
	raw, _ := json.Marshal(s.Downloads())
	if strings.Contains(string(raw), "HF_TOKEN") {
		t.Fatal("token env leaked")
	}
}

func TestEngineAutomaticNames(t *testing.T) {
	s := engineDownloadService(t)
	s.settings.EngineDownloadName = "runtime"
	installHTTPDo = func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	root := filepath.Join(*s.settings.WorkspaceRoot, "engines")
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	a, e := s.CreateEngineDownload(engineSource, "")
	if e != nil || a.OutName != "runtime-2" {
		t.Fatal(a, e)
	}
	b, e := s.CreateEngineDownload(engineSource, "")
	if e != nil || b.OutName != "runtime-3" {
		t.Fatal(b, e)
	}
}

func TestDownloadNameExhaustion(t *testing.T) {
	root := t.TempDir()
	s := &Service{}
	for n := 1; n <= 1000; n++ {
		name := "model"
		if n > 1 {
			name = fmt.Sprintf("model-%d", n)
		}
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, e := s.downloadOutputLocked(root, "", "model"); e == nil || e.Code != "output_name_exhausted" {
		t.Fatal(e)
	}
	if err := os.Remove(filepath.Join(root, "model-1000")); err != nil {
		t.Fatal(err)
	}
	if name, _, e := s.downloadOutputLocked(root, "", "model"); e != nil || name != "model-1000" {
		t.Fatal(name, e)
	}
	reserved, err := Resolve(root, "model-1000")
	if err != nil {
		t.Fatal(err)
	}
	s.downloads = []*downloadEntry{{Task: DownloadTask{OutDir: reserved, State: "failed"}}}
	if _, _, e := s.downloadOutputLocked(root, "", "model"); e == nil || e.Code != "output_name_exhausted" {
		t.Fatal("ignored reserved name", e)
	}
}
