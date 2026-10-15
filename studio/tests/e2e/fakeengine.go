package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Fake engine behavior constants proven by the dist/e2e-spike experiments:
// token accounting is byte-length based so /tokenize and usage always agree.
const (
	fakeBuildInfo = "b10743-adfffbe41"
	fakeAnswer    = "假引擎固定应答"
	fakeNCtx      = 4096
)

// FakeAssets are the on-disk declarations a candidate references: dummy model
// weights, a chat template, the params file the fake engine serves back, and
// the runtime library manifest. All hashes are real.
type FakeAssets struct {
	Dir            string
	Model          string
	Template       string
	Params         string
	Manifest       string
	ModelSHA256    string
	TemplateSHA256 string
}

// DisplayName is the single model id the fake engine serves.
const DisplayName = "Fake E2E Model"

// DefaultGeneration mirrors the candidate's frozen generation block.
func DefaultGeneration() map[string]any {
	return map[string]any{
		"seed": 42, "temperature": 0.7, "top_k": 20, "top_p": 0.8,
		"min_p": 0.0, "presence_penalty": 0.0, "repeat_penalty": 1.0, "max_tokens": 128,
	}
}

// StartupArgs returns the exact argv the candidate must declare; only --host
// and --port values may be rewritten by config bind.
func StartupArgs(modelPath, paramsPath, port string) []string {
	return []string{
		"-m", modelPath,
		"--host", "127.0.0.1", "--port", port,
		"-ngl", "0", "-t", "4", "-tb", "4", "--reasoning", "off",
		"--no-webui", "--no-cache-prompt", "--no-cache-idle-slots", "--cache-ram", "0",
		"--params-file", paramsPath,
	}
}

func writeFakeAssets(t *testing.T) *FakeAssets {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	assets := &FakeAssets{
		Dir:      dir,
		Model:    filepath.Join(dir, "fake-model.gguf"),
		Template: filepath.Join(dir, "chat-template.jinja"),
		Params:   filepath.Join(dir, "fake-engine-params.json"),
		Manifest: filepath.Join(dir, "engine-sha256.json"),
	}
	if err := os.WriteFile(assets.Model, fakeGGUF(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assets.Template, []byte("{{ messages }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, assets.Params, fakeParams{
		DisplayName: DisplayName, ModelPath: assets.Model, TemplatePath: assets.Template,
		NCtx: fakeNCtx, Generation: DefaultGeneration(),
	})
	assets.ModelSHA256 = sha256File(t, assets.Model)
	assets.TemplateSHA256 = sha256File(t, assets.Template)
	return assets
}

// FakeEngine is a running fake llama-server process (the test binary itself).
type FakeEngine struct {
	PID      int
	Endpoint string
	Args     []string
	Assets   *FakeAssets

	cmd *exec.Cmd
}

// StartFakeEngine writes the fake assets and launches the test binary in
// engine mode, waiting for /health. Cleanup kills the process.
func StartFakeEngine(t *testing.T) *FakeEngine {
	t.Helper()
	assets := writeFakeAssets(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, assets.Manifest, map[string]string{filepath.Base(binary): sha256File(t, binary)})
	port := freePort(t)
	args := StartupArgs(assets.Model, assets.Params, port)
	cmd := exec.Command(binary, args...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	engine := &FakeEngine{
		PID: cmd.Process.Pid, Endpoint: "http://127.0.0.1:" + port,
		Args: args, Assets: assets, cmd: cmd,
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(engine.Endpoint + "/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return engine
			}
		}
		if cmd.ProcessState != nil {
			t.Fatal("fake engine exited before health")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fake engine did not become healthy")
	return nil
}

type fakeParams struct {
	DisplayName  string         `json:"display_name"`
	ModelPath    string         `json:"model_path"`
	TemplatePath string         `json:"template_path"`
	NCtx         int            `json:"n_ctx"`
	Generation   map[string]any `json:"generation"`
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sha256Of(data))
}

func sha256Of(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
