package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CandidateDescription is a read-only CLI description, never run eligibility.
type CandidateDescription struct {
	Definition string              `json:"definition"`
	Family     string              `json:"family"`
	Adapter    string              `json:"adapter"`
	Launch     CandidateLaunch     `json:"launch"`
	Assets     CandidateAssets     `json:"assets"`
	Endpoint   CandidateEndpoint   `json:"endpoint"`
	Conditions CandidateConditions `json:"conditions"`
	Generation CandidateGeneration `json:"generation"`
	Bound      bool                `json:"bound"`
}

type CandidateLaunch struct {
	Kind                   string            `json:"kind"`
	Argv                   []string          `json:"argv"`
	WorkingDirectory       *string           `json:"working_directory"`
	Environment            map[string]string `json:"environment"`
	Ownership              string            `json:"ownership"`
	LMSExecutable          string            `json:"lms_executable"`
	ModelsRoot             string            `json:"models_root"`
	ServedModel            string            `json:"served_model"`
	BackgroundArgv         []string          `json:"background_argv"`
	RequiresLoadedInstance bool              `json:"requires_loaded_instance"`
}

type CandidateFileAsset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type CandidateInstallationAsset struct {
	Path              string `json:"path"`
	SHA256            string `json:"sha256"`
	DeclarationSHA256 string `json:"declaration_sha256"`
}

type CandidateModelAsset struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	ContentSHA256  string `json:"content_sha256"`
	ManifestPath   string `json:"manifest_path"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type CandidateAssets struct {
	Installation *CandidateInstallationAsset `json:"installation"`
	Engine       *CandidateFileAsset         `json:"engine"`
	Model        CandidateModelAsset         `json:"model"`
	Template     CandidateFileAsset          `json:"template"`
}

type CandidateEndpoint struct {
	URL               string `json:"url"`
	ServerPID         int64  `json:"server_pid"`
	ProcessStartTicks int64  `json:"process_start_ticks"`
	APIKeyEnv         string `json:"api_key_env"`
	BindingPath       string `json:"binding_path"`
	BindingSHA256     string `json:"binding_sha256"`
}

type CandidateConditions struct {
	ContextSize int64 `json:"context_size"`
}

type CandidateGeneration struct {
	MaxTokens   int64   `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

var candidateDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var candidateVariable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var candidateRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var candidateWindowsPath = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\[^\\/]+[\\/][^\\/]+[\\/])`)

func candidateHash(value string) bool { return candidateDigest.MatchString(value) }
func localFamily(value string) bool {
	return slices.Contains([]string{"llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"}, value)
}
func candidateText(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\x00\r\n")
}
func candidatePath(value string) bool {
	return candidateText(value) && (filepath.IsAbs(value) || candidateWindowsPath.MatchString(value))
}

// candidateObject requires named fields and forbids null values. Optional fields
// are explicit, so an unknown field cannot accidentally select another branch.
func candidateObject(raw []byte, required, optional []string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, false
		}
	}
	for key, value := range fields {
		if (!slices.Contains(required, key) && !slices.Contains(optional, key)) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return fields, true
}

// ParseCandidateDescription validates the consumed CLI contract before any
// caller uses argv or asset identity. All failure envelopes are rejected.
func ParseCandidateDescription(raw []byte, exitCode int) (CandidateDescription, error) {
	var result CandidateDescription
	invalid := errors.New("invalid candidate description")
	if exitCode != 0 || !utf8.Valid(raw) {
		return result, invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return result, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return result, invalid
	}
	r, err := Parse(raw)
	if err != nil || !discoveryFields(raw, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") ||
		r.Command != "config describe" || r.Status != "prepared" || r.Completeness != "complete" || r.RunID != nil || r.EvidenceDir != nil || r.Limitations == nil || len(r.Limitations) != 0 {
		return result, invalid
	}
	if json.Unmarshal(r.Details, &result) != nil {
		return CandidateDescription{}, invalid
	}
	local := result.Definition == "local-engine-description.v2"
	if !local && result.Definition != "engine-description.v1" {
		return CandidateDescription{}, invalid
	}
	keys := []string{"schema_version", "definition", "launch", "assets", "generation", "conditions", "endpoint", "model_requests_sent"}
	if local {
		keys = append(keys, "family", "safety", "bound", "effective_parameters", "effective_template", "effective_unknown_reason")
	} else {
		keys = append(keys, "adapter")
	}
	if !discoveryFields(r.Details, keys...) {
		return CandidateDescription{}, invalid
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(r.Details, &fields)
	var version, requests *int
	if json.Unmarshal(fields["schema_version"], &version) != nil || version == nil || *version != 3 ||
		json.Unmarshal(fields["model_requests_sent"], &requests) != nil || requests == nil || *requests != 0 {
		return CandidateDescription{}, invalid
	}
	if local {
		if !localFamily(result.Family) || !bytes.Equal(fields["effective_parameters"], []byte("null")) || !bytes.Equal(fields["effective_template"], []byte("null")) ||
			!bytes.Equal(fields["effective_unknown_reason"], []byte(`"requires_live_engine_observation"`)) ||
			(!bytes.Equal(fields["bound"], []byte("true")) && !bytes.Equal(fields["bound"], []byte("false"))) || !validCandidateSafety(fields["safety"]) {
			return CandidateDescription{}, invalid
		}
	} else if !slices.Contains([]string{"prism_llama_server_v1", "llama_cpp_b11146_v1", "kvmem", "ninfer"}, result.Adapter) {
		return CandidateDescription{}, invalid
	}
	if !validCandidateLaunch(fields["launch"], result.Launch, local, result.Family) ||
		!validCandidateAssets(fields["assets"], result.Assets, local) || !validCandidateEndpoint(fields["endpoint"], result.Endpoint, local, result.Bound) ||
		!validCandidateParameters(fields, local) {
		return CandidateDescription{}, invalid
	}
	if !local && result.Launch.Argv[0] != result.Assets.Engine.Path {
		return CandidateDescription{}, invalid
	}
	return result, nil
}

func validCandidateLaunch(raw []byte, launch CandidateLaunch, local bool, family string) bool {
	keys := []string{"kind", "working_directory", "environment", "ownership"}
	lmstudio := local && family == "lmstudio"
	if lmstudio {
		keys = append(keys, "lms_executable", "models_root", "background_argv", "served_model", "requires_loaded_instance")
	} else {
		keys = append(keys, "argv")
	}
	if !discoveryFields(raw, keys...) || launch.Ownership != "operator" {
		return false
	}
	if local {
		if launch.WorkingDirectory == nil || !candidatePath(*launch.WorkingDirectory) || launch.Environment == nil {
			return false
		}
		if !validCandidateEnvironment(launch.Environment) {
			return false
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		var env map[string]json.RawMessage
		_ = json.Unmarshal(fields["environment"], &env)
		for _, value := range env {
			if bytes.Equal(value, []byte("null")) {
				return false
			}
		}
	} else if launch.WorkingDirectory != nil || launch.Environment != nil {
		return false
	}
	argv := launch.Argv
	if lmstudio {
		if launch.Kind != "lmstudio" || !candidatePath(launch.LMSExecutable) || !candidatePath(launch.ModelsRoot) || !candidateText(launch.ServedModel) || !launch.RequiresLoadedInstance {
			return false
		}
		argv = launch.BackgroundArgv
	} else if launch.Kind != "direct_process" {
		return false
	}
	if len(argv) == 0 || !candidatePath(argv[0]) {
		return false
	}
	for _, arg := range argv {
		if strings.ContainsRune(arg, '\x00') {
			return false
		}
	}
	// encoding/json decodes a null string element as ""; inspect the wire too.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	key := "argv"
	if lmstudio {
		key = "background_argv"
	}
	var args []*string
	if json.Unmarshal(fields[key], &args) != nil {
		return false
	}
	for _, arg := range args {
		if arg == nil {
			return false
		}
	}
	return true
}

func validCandidateAssets(raw []byte, assets CandidateAssets, local bool) bool {
	keys := []string{"model", "template"}
	if local {
		keys = append(keys, "installation")
	} else {
		keys = append(keys, "engine")
	}
	fields, ok := candidateObject(raw, keys, nil)
	if !ok || !discoveryFields(fields["template"], "path", "sha256") || !candidatePath(assets.Template.Path) || !candidateHash(assets.Template.SHA256) {
		return false
	}
	modelKeys := []string{"path", "kind", "content_sha256"}
	if local {
		modelKeys = append(modelKeys, "manifest_path", "manifest_sha256")
	}
	if !discoveryFields(fields["model"], modelKeys...) || !candidatePath(assets.Model.Path) || !candidateHash(assets.Model.ContentSHA256) {
		return false
	}
	if local {
		a := assets.Installation
		return a != nil && discoveryFields(fields["installation"], "path", "sha256", "declaration_sha256") && candidatePath(a.Path) && candidateHash(a.SHA256) && candidateHash(a.DeclarationSHA256) &&
			slices.Contains([]string{"gguf", "directory"}, assets.Model.Kind) && candidatePath(assets.Model.ManifestPath) && candidateHash(assets.Model.ManifestSHA256)
	}
	return assets.Engine != nil && discoveryFields(fields["engine"], "path", "sha256") && candidatePath(assets.Engine.Path) && candidateHash(assets.Engine.SHA256) && slices.Contains([]string{"gguf", "ninfer"}, assets.Model.Kind)
}

func validCandidateEndpoint(raw []byte, endpoint CandidateEndpoint, local, bound bool) bool {
	optional := []string{"api_key_env"}
	if local {
		optional = append(optional, "binding_path", "binding_sha256")
	}
	fields, ok := candidateObject(raw, []string{"url", "server_pid", "process_start_ticks"}, optional)
	if !ok || endpoint.ServerPID <= 0 || endpoint.ProcessStartTicks <= 0 {
		return false
	}
	u, err := url.Parse(endpoint.URL)
	if err != nil || !slices.Contains([]string{"http", "https"}, u.Scheme) || (u.Hostname() != "localhost" && !net.ParseIP(u.Hostname()).IsLoopback()) ||
		u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(endpoint.URL, "?#\r\n\t ") {
		return false
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return false
		}
	}
	if _, ok := fields["api_key_env"]; ok && !candidateVariable.MatchString(endpoint.APIKeyEnv) {
		return false
	}
	_, path := fields["binding_path"]
	_, hash := fields["binding_sha256"]
	return path == hash && (!local || bound == path) && (!path || (candidatePath(endpoint.BindingPath) && candidateHash(endpoint.BindingSHA256)))
}

func validCandidateParameters(fields map[string]json.RawMessage, local bool) bool {
	var conditions, generation map[string]json.RawMessage
	if json.Unmarshal(fields["conditions"], &conditions) != nil || conditions == nil || json.Unmarshal(fields["generation"], &generation) != nil || generation == nil {
		return false
	}
	var contextSize, maxTokens *int64
	var temperature *float64
	if json.Unmarshal(conditions["context_size"], &contextSize) != nil || contextSize == nil || *contextSize <= 0 ||
		json.Unmarshal(generation["max_tokens"], &maxTokens) != nil || maxTokens == nil || *maxTokens <= 0 || *maxTokens > *contextSize ||
		json.Unmarshal(generation["temperature"], &temperature) != nil || temperature == nil || *temperature < 0 {
		return false
	}
	if local && (!discoveryFields(fields["generation"], "temperature", "max_tokens") || *temperature != 0 || *maxTokens > 8192 || *contextSize > 32768) {
		return false
	}
	return true
}

func validCandidateEnvironment(environment map[string]string) bool {
	for key, value := range environment {
		switch key {
		case "SGLANG_USE_MLX", "PYTHONDONTWRITEBYTECODE", "HF_HUB_OFFLINE", "TRANSFORMERS_OFFLINE":
			if value != "1" {
				return false
			}
		case "SGLANG_RUN_ID":
			if !candidateRunID.MatchString(value) {
				return false
			}
		case "PROMETHEUS_MULTIPROC_DIR":
			if !candidatePath(value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func validCandidateSafety(raw []byte) bool {
	if !discoveryFields(raw, "interval_seconds", "max_temperature_celsius", "require_temperature", "check_environment") {
		return false
	}
	var value struct {
		Interval    *float64 `json:"interval_seconds"`
		Maximum     *float64 `json:"max_temperature_celsius"`
		Temperature *bool    `json:"require_temperature"`
		Environment *bool    `json:"check_environment"`
	}
	return json.Unmarshal(raw, &value) == nil && value.Interval != nil && *value.Interval >= 0.1 && *value.Interval <= 5 && value.Maximum != nil && *value.Maximum >= 1 && *value.Maximum <= 85 && value.Temperature != nil && *value.Temperature && value.Environment != nil && *value.Environment
}
