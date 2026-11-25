package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"inferyard/studio/internal/core"
)

func copyExecutableFile(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

const windowsFakeServerCS = `using System;
using System.IO;
using System.Net;
using System.Net.Sockets;
using System.Text;

class S {
  static string Esc(string s) {
    return s.Replace("\\", "\\\\").Replace("\"", "\\\"");
  }
  static void Main(string[] a) {
    string host = "127.0.0.1", model = "", alias = "";
    int port = 0; long nctx = 0;
    for (int i = 0; i + 1 < a.Length; i++) {
      if (a[i] == "--host") { host = a[++i]; }
      else if (a[i] == "--port") { int.TryParse(a[++i], out port); }
      else if (a[i] == "-m" || a[i] == "--model") { model = a[++i]; }
      else if (a[i] == "--alias") { alias = a[++i]; }
      else if (a[i] == "-c" || a[i] == "--ctx-size") { long.TryParse(a[++i], out nctx); }
    }
    if (port <= 0 || model == "" || alias == "" || nctx <= 0) Environment.Exit(2);
    var l = new TcpListener(IPAddress.Parse(host), port);
    l.Start();
    for (;;) {
      using (var c = l.AcceptTcpClient())
      using (var ns = c.GetStream())
      using (var r = new StreamReader(ns, Encoding.ASCII)) {
        string line = r.ReadLine();
        if (line == null) continue;
        string[] parts = line.Split(' ');
        string path = parts.Length > 1 ? parts[1] : "/";
        string h;
        while ((h = r.ReadLine()) != null && h.Length > 0) {}
        string body = "{}";
        if (path == "/health") body = "{\"status\":\"ok\"}";
        else if (path == "/v1/models") body = "{\"data\":[{\"id\":\"" + Esc(alias) + "\",\"object\":\"model\"}]}";
        else if (path == "/props") body = "{\"model_path\":\"" + Esc(model) + "\",\"default_generation_settings\":{\"n_ctx\":" + nctx + "}}";
        byte[] b = Encoding.UTF8.GetBytes(body);
        byte[] resp = Encoding.ASCII.GetBytes("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: " + b.Length + "\r\n\r\n");
        ns.Write(resp, 0, resp.Length);
        ns.Write(b, 0, b.Length);
      }
    }
  }
}
`

func setupWindowsCommunityFake(t *testing.T, workspace string) {
	t.Helper()
	profilePath := filepath.Join("..", "..", "..", "cli", "src", "inferyard", "data", "community", "profile", "prism-b10743-adfffbe-win-cpu-x64.json")
	rawProfile, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read Windows community profile: %v", err)
	}
	sum := sha256.Sum256(rawProfile)
	profileDigest := hex.EncodeToString(sum[:])

	var profile struct {
		Profile                string            `json:"profile"`
		Platform               string            `json:"platform"`
		Architecture           string            `json:"architecture"`
		Mode                   string            `json:"mode"`
		EngineRelease          string            `json:"engine_release"`
		Archives               json.RawMessage   `json:"archives"`
		EngineBinary           string            `json:"engine_binary"`
		RuntimeLibraryManifest string            `json:"runtime_library_manifest"`
		Libraries              map[string]string `json:"libraries"`
		Members                []struct {
			Role  string `json:"role"`
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		} `json:"members"`
	}
	if err := json.Unmarshal(rawProfile, &profile); err != nil {
		t.Fatalf("parse Windows community profile: %v", err)
	}

	root := filepath.Join(workspace, "engines", "fake-llama")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(profile.RuntimeLibraryManifest))
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, manifestPath, profile.Libraries)

	var exeBytes int64
	for _, m := range profile.Members {
		if m.Role == "" || m.Path == "" {
			t.Fatalf("unexpected empty role/path in profile member: %+v", m)
		}
		rel := m.Role + "/" + m.Path
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if filepath.ToSlash(rel) == filepath.ToSlash(profile.EngineBinary) {
			exeBytes = m.Bytes
			continue
		}
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(m.Bytes); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		_ = f.Close()
	}
	if exeBytes <= 0 {
		t.Fatalf("profile %q missing engine binary member %q", profile.Profile, profile.EngineBinary)
	}

	csc := filepath.Join(os.Getenv("SystemRoot"), "Microsoft.NET", "Framework64", "v4.0.30319", "csc.exe")
	srcPath := filepath.Join(t.TempDir(), "fake_server.cs")
	if err := os.WriteFile(srcPath, []byte(windowsFakeServerCS), 0o600); err != nil {
		t.Fatal(err)
	}
	exePath := filepath.Join(root, filepath.FromSlash(profile.EngineBinary))
	out, err := exec.Command(csc, "/nologo", "/optimize+", "/target:exe", "/out:"+exePath, srcPath).CombinedOutput()
	if err != nil {
		t.Fatalf("compile Windows fake server with csc.exe failed: %v\n%s", err, string(out))
	}
	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if exeBytes <= 0 || info.Size() > exeBytes {
		t.Fatalf("compiled fake llama-server.exe size=%d exceeds profile member size=%d", info.Size(), exeBytes)
	}
	if err := os.Truncate(exePath, exeBytes); err != nil {
		t.Fatalf("pad fake llama-server.exe to %d bytes: %v", exeBytes, err)
	}

	writeJSONFile(t, filepath.Join(root, "runtime-receipt.json"), map[string]any{
		"kind":                     "community_runtime_receipt.v1",
		"schema_version":           3,
		"profile":                  profile.Profile,
		"profile_sha256":           profileDigest,
		"platform":                 profile.Platform,
		"architecture":             profile.Architecture,
		"mode":                     profile.Mode,
		"engine_release":           profile.EngineRelease,
		"archives":                 profile.Archives,
		"engine_binary":            profile.EngineBinary,
		"runtime_library_manifest": profile.RuntimeLibraryManifest,
		"engine_sha256":            profile.Libraries["llama-server.exe"],
		"libraries":                profile.Libraries,
		"version":                  map[string]any{"argv": []string{"--version"}, "returncode": 0, "stdout": "ok", "stderr": ""},
		"model_requests_sent":      0,
		"ready_to_run":             false,
	})
}

func TestStandaloneServeLifecycle(t *testing.T) {
	cli := ResolveCLI(t)
	env := NewEnv(t, cli)
	ctx := context.Background()

	assets := writeFakeAssets(t)
	placeFakeModel(t, env.Models, assets.Model)
	wantModelPath := filepath.Join(env.Models, filepath.Base(assets.Model))
	if eval, err := filepath.EvalSymlinks(wantModelPath); err == nil {
		wantModelPath = eval
	}

	if runtime.GOOS == "windows" {
		setupWindowsCommunityFake(t, env.Workspace)
	} else {
		t.Setenv("INFERYARD_E2E_STANDALONE_FAKE", "1")
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		copyExecutableFile(t, binary, filepath.Join(env.Workspace, "engines", "fake-llama", "llama-server"))
	}

	service, err := core.New(ctx, env.Options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = service.Shutdown(shutdownCtx)
	}()

	mustJSON := func(v any) json.RawMessage {
		data, _ := json.Marshal(v)
		return data
	}
	if _, e := service.UpdateSettings(map[string]json.RawMessage{
		"inferyard_path": mustJSON(cli),
		"workspace_root": mustJSON(env.Workspace),
	}); e != nil {
		t.Fatal(e)
	}
	_ = service.RefreshAssets()

	if !service.ServeSupported(ctx) {
		t.Fatalf("expected synced CLI to support standalone serve")
	}

	var modelName, engineName string
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		reg := service.Registry()
		for _, m := range reg.Models {
			if m.Available && strings.HasSuffix(m.Name, ".gguf") {
				modelName = m.Name
			}
		}
		for _, e := range reg.Engines {
			if strings.Contains(e.Path, "fake-llama") || strings.Contains(e.Name, "fake-llama") {
				engineName = e.Name
			}
		}
		if modelName != "" && engineName != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if modelName == "" || engineName == "" {
		t.Fatalf("failed to discover fake assets in registry: model=%q engine=%q", modelName, engineName)
	}

	// 1. Start standalone with non-benchmark-tier context size (2048 <= fakeNCtx 4096) without any benchmark task.
	const wantCtx int64 = 2048
	state, apiErr := service.StartStandalone(ctx, core.StandaloneStartRequest{
		Model:       modelName,
		Engine:      engineName,
		ContextSize: wantCtx,
	})
	if apiErr != nil {
		t.Fatalf("StartStandalone failed: %+v", apiErr)
	}
	if state.Origin != core.EngineOriginStandalone {
		t.Fatalf("origin=%q want standalone", state.Origin)
	}
	waitEngineRunning(t, service, 30*time.Second)
	running := service.Engine()

	// Verify /v1/models returns the exact served model alias from CLI argv.
	resp, err := http.Get(running.Endpoint + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &modelsResp) != nil ||
		len(modelsResp.Data) != 1 || modelsResp.Data[0].ID != DisplayName {
		t.Fatalf("unexpected /v1/models response: status=%d body=%s (want exact id %q)", resp.StatusCode, string(body), DisplayName)
	}

	// Verify /props reflects the actual -m model path and -c 2048 context size passed by `serve prepare`.
	propsResp, err := http.Get(running.Endpoint + "/props")
	if err != nil {
		t.Fatalf("GET /props failed: %v", err)
	}
	propsBody, _ := io.ReadAll(propsResp.Body)
	_ = propsResp.Body.Close()
	var props struct {
		ModelPath string `json:"model_path"`
		Settings  struct {
			NCtx int64 `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if propsResp.StatusCode != http.StatusOK || json.Unmarshal(propsBody, &props) != nil {
		t.Fatalf("unexpected /props response: status=%d body=%s", propsResp.StatusCode, string(propsBody))
	}
	gotModelPath := props.ModelPath
	if eval, err := filepath.EvalSymlinks(gotModelPath); err == nil {
		gotModelPath = eval
	}
	if props.Settings.NCtx != wantCtx || filepath.Clean(gotModelPath) != filepath.Clean(wantModelPath) {
		t.Fatalf("fake engine received wrong args: n_ctx=%d (want %d), model=%q (want %q)", props.Settings.NCtx, wantCtx, gotModelPath, wantModelPath)
	}

	// Verify owner file records origin == "standalone".
	ownerFile := filepath.Join(env.Workspace, "logs", "engine-owner.json")
	rawOwner, err := os.ReadFile(ownerFile)
	if err != nil {
		t.Fatalf("expected engine-owner.json while running: %v", err)
	}
	if !strings.Contains(string(rawOwner), `"origin":"standalone"`) {
		t.Fatalf("owner file missing standalone origin: %s", string(rawOwner))
	}

	// 2. Stop and verify owner file is removed.
	if _, stopErr := service.StopEngine(); stopErr != nil {
		t.Fatalf("StopEngine failed: %+v", stopErr)
	}
	stopDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(stopDeadline) {
		if service.Engine().State == "stopped" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if service.Engine().State != "stopped" {
		t.Fatalf("engine did not stop, state=%+v", service.Engine())
	}
	if _, err := os.Stat(ownerFile); !os.IsNotExist(err) {
		t.Fatalf("expected owner file removed after stop, err=%v", err)
	}

	// 3. Second start of the same combination reuses serve-plan.json without re-running `serve prepare`.
	countServePrepare := func() int {
		n := 0
		for _, rec := range env.Recorder.Records() {
			if len(rec.Argv) >= 3 && rec.Argv[1] == "serve" && rec.Argv[2] == "prepare" {
				n++
			}
		}
		return n
	}
	if got := countServePrepare(); got != 1 {
		t.Fatalf("expected 1 serve prepare call after first start, got %d", got)
	}
	if _, apiErr := service.StartStandalone(ctx, core.StandaloneStartRequest{
		Model:       modelName,
		Engine:      engineName,
		ContextSize: wantCtx,
	}); apiErr != nil {
		t.Fatalf("2nd StartStandalone failed: %+v", apiErr)
	}
	waitEngineRunning(t, service, 30*time.Second)
	if got := countServePrepare(); got != 1 {
		t.Fatalf("expected reuse to skip serve prepare, calls=%d", got)
	}

	if _, stopErr := service.StopEngine(); stopErr != nil {
		t.Fatalf("2nd StopEngine failed: %+v", stopErr)
	}
}
