package observe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type inputMember struct {
	Payload json.RawMessage `json:"payload"`
	SHA256  string          `json:"sha256"`
}

type inputEnvelope struct {
	Schema        int                    `json:"schema_version"`
	Layout        int                    `json:"evidence_layout_version"`
	RunID         string                 `json:"run_id"`
	Documents     map[string]inputMember `json:"documents"`
	Source        json.RawMessage        `json:"configuration_source"`
	MissingReason *string                `json:"configuration_source_missing_reason"`
}

func exactFields(raw json.RawMessage, names ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}

func readInputsBinding(root string) (Binding, Target, error) {
	var binding Binding
	var target Target
	fail := errors.New("benchmark_binding_invalid")
	raw, err := boundedFile(filepath.Join(root, "inputs.json"))
	if err != nil {
		return binding, target, err
	}
	var envelope inputEnvelope
	if StrictJSON(raw, &envelope) != nil || !exactFields(raw, "schema_version",
		"evidence_layout_version", "run_id", "documents", "configuration_source",
		"configuration_source_missing_reason") || envelope.Schema != 3 || envelope.Layout != 2 ||
		envelope.RunID == "" || len(envelope.Documents) != 5 {
		return binding, target, fail
	}
	// A new input container never authorizes fallback to snapshots beside it.
	for _, name := range []string{"run.json", "plan.json", "selection.json", "config.frozen.json", "bundle.json", "config.input.toml", "config.input.json",
		"parent.json", "service-binding.json", "service-reuse.json", "safety-policy.json",
		"environment.start.json", "environment.end.json", "service.props.json",
		"engine-capabilities.json", "recovery.json", "token-budgets.v2.json",
		"input-target-check.json", "collector.json", "observer.json", "boundary-observer.json",
		"effective-ordinary.json", "effective-stream.json", "identity.json", "safety-check.json",
		"execution-budget.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			return binding, target, fail
		}
	}
	paths, err := os.ReadDir(root)
	if err != nil {
		return binding, target, fail
	}
	for _, path := range paths {
		if strings.HasSuffix(path.Name(), ".request.json") || strings.HasPrefix(path.Name(), "client-http-clean-") {
			return binding, target, fail
		}
	}
	var members map[string]json.RawMessage
	json.Unmarshal(raw, &members)
	var documents map[string]json.RawMessage
	json.Unmarshal(members["documents"], &documents)
	for _, name := range []string{"run", "plan", "selection", "config", "bundle"} {
		member, ok := envelope.Documents[name]
		if !ok || !exactFields(documents[name], "payload", "sha256") {
			return binding, target, fail
		}
		canonical, err := memberBytes(member.Payload)
		if err != nil || digest(canonical) != member.SHA256 {
			return binding, target, fail
		}
		var core struct {
			Schema int `json:"schema_version"`
		}
		if json.Unmarshal(member.Payload, &core) != nil || core.Schema != 3 {
			return binding, target, fail
		}
	}
	if bytes.Equal(bytes.TrimSpace(envelope.Source), []byte("null")) {
		if envelope.MissingReason == nil || *envelope.MissingReason == "" {
			return binding, target, fail
		}
	} else {
		var source struct {
			Media    string  `json:"media_type"`
			Encoding string  `json:"encoding"`
			Content  *string `json:"content"`
			SHA256   string  `json:"sha256"`
			Bytes    *int    `json:"bytes"`
		}
		if envelope.MissingReason != nil || !exactFields(envelope.Source, "media_type", "encoding", "content", "sha256", "bytes") ||
			json.Unmarshal(envelope.Source, &source) != nil || source.Content == nil || source.Bytes == nil || source.Encoding != "base64" ||
			(source.Media != "application/toml" && source.Media != "application/json") {
			return binding, target, fail
		}
		data, err := base64.StdEncoding.Strict().DecodeString(*source.Content)
		if err != nil || base64.StdEncoding.EncodeToString(data) != *source.Content ||
			len(data) != *source.Bytes || digest(data) != source.SHA256 {
			return binding, target, fail
		}
	}
	runMember, configMember := envelope.Documents["run"], envelope.Documents["config"]
	var run struct {
		RunID string `json:"run_id"`
	}
	var config struct {
		Endpoint struct {
			PID   int    `json:"server_pid"`
			Start uint64 `json:"process_start_ticks"`
		} `json:"endpoint"`
		Model struct {
			Name *string `json:"display_name"`
		} `json:"model"`
		Engine struct {
			Backend *string `json:"backend"`
		} `json:"engine"`
	}
	if json.Unmarshal(runMember.Payload, &run) != nil || json.Unmarshal(configMember.Payload, &config) != nil ||
		run.RunID != envelope.RunID || config.Endpoint.PID <= 0 || config.Endpoint.PID > 2_147_483_647 || config.Endpoint.Start == 0 || config.Model.Name == nil || config.Engine.Backend == nil {
		return binding, target, fail
	}
	if _, err := os.Lstat(filepath.Join(root, "manifest.json")); err == nil {
		rawManifest, err := boundedFile(filepath.Join(root, "manifest.json"))
		var manifest struct {
			Schema int    `json:"schema_version"`
			Layout int    `json:"evidence_layout_version"`
			RunID  string `json:"run_id"`
			Files  map[string]struct {
				SHA256 string `json:"sha256"`
				Bytes  *int   `json:"bytes"`
			} `json:"files"`
		}
		if err != nil || StrictJSON(rawManifest, &manifest) != nil || manifest.Schema != 3 || manifest.Layout != 2 || manifest.RunID != run.RunID {
			return binding, target, fail
		}
		info, ok := manifest.Files["inputs.json"]
		if !ok || info.Bytes == nil || *info.Bytes != len(raw) || info.SHA256 != digest(raw) {
			return binding, target, fail
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return binding, target, fail
	}
	binding = Binding{Definition: "benchmark-inputs-binding.v1", InputsSHA256: digest(raw),
		RunID: run.RunID, RunMemberSHA256: runMember.SHA256, ConfigMemberSHA256: configMember.SHA256,
		ModelLabel: *config.Model.Name, Backend: *config.Engine.Backend}
	target = Target{PID: config.Endpoint.PID, StartTicks: config.Endpoint.Start}
	return binding, target, nil
}
