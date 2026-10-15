package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// WriteCandidate fabricates the config_candidate.v1 artifact pair
// (candidate.toml + preparation.json) that CLI `config create` would produce.
// This is the deliberate fixture boundary of the chain test: candidate
// production needs real engine assets and is covered separately; the chain
// under test is bind -> run -> report.
func WriteCandidate(t *testing.T, workspace, bundle string, assets *FakeAssets, binaryPath, port string) string {
	t.Helper()
	bundlePath := filepath.Join(workspace, "bundles", bundle+".json")
	version := readBundleVersion(t, bundlePath)
	dir := filepath.Join(workspace, "candidates", bundle+"-e2e")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "candidate.toml")); err == nil {
		t.Fatalf("candidate already exists: %s", dir)
	}
	binarySHA := sha256File(t, binaryPath)
	args := StartupArgs(assets.Model, assets.Params, port)
	argsJSON, _ := json.Marshal(args)
	quote := strconv.Quote
	content := `schema_version = 3
[bundle]
path = ` + quote(bundlePath) + `
version = ` + quote(version) + `

[conditions]
ac_online = true
background_load = "e2e chain test"
cache_policy = "disabled"
context_size = 4096
allow_unknown_environment = true
environment_admission = { definition = "environment-admission.v2", required_fields = ["ac_online"] }
epp = "balance_performance"
governor = "powersave"
profile = "balanced"
model_loaded = true
slots = 1
threads = 4
threads_batch = 4

[device]
id = "E2E-CHAIN"
client_position = "same_host_native"

[endpoint]
url = "http://127.0.0.1:` + port + `"
server_pid = 1
process_start_ticks = 1

[engine]
adapter = "prism_llama_server_v1"
backend = "cpu"
release = "e2e-fake-engine"
binary_path = ` + quote(binaryPath) + `
binary_sha256 = "` + binarySHA + `"
runtime_library_manifest = ` + quote(assets.Manifest) + `
startup_args = ` + string(argsJSON) + `

[generation]
max_tokens = 128
min_p = 0.0
presence_penalty = 0.0
reasoning_mode = "off"
repeat_penalty = 1.0
seed = 42
seed_support = "supported"
stop = []
temperature = 0.7
top_k = 20
top_p = 0.8

[model]
display_name = ` + quote(DisplayName) + `
local_path = ` + quote(assets.Model) + `
packing = "Q4_K_M"
repo = "fake/e2e"
revision = "e2e"
sha256 = "` + assets.ModelSHA256 + `"
template_path = ` + quote(assets.Template) + `
template_sha256 = "` + assets.TemplateSHA256 + `"

[output]
root = ` + quote(filepath.Join(workspace, "results")) + `
min_available_memory_bytes = 1073741824
min_disk_bytes = 5368709120
`
	candidatePath := filepath.Join(dir, "candidate.toml")
	if err := os.WriteFile(candidatePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	preparation := map[string]any{
		"kind":                "config_candidate.v1",
		"schema_version":      3,
		"candidate":           candidatePath,
		"bundle":              bundlePath,
		"out":                 dir,
		"model":               assets.Model,
		"template":            assets.Template,
		"mode":                "cpu",
		"ready_to_run":        false,
		"model_requests_sent": 0,
		"limitations":         []string{},
	}
	writeJSONFile(t, filepath.Join(dir, "preparation.json"), preparation)
	return dir
}

func readBundleVersion(t *testing.T, bundlePath string) string {
	t.Helper()
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil || bundle.Version == "" {
		t.Fatal("bundle version unreadable", bundlePath)
	}
	return bundle.Version
}
