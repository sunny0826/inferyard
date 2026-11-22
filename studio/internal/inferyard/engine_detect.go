package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const (
	LocalEngineDetectDefinition = "local-engine-detect.v1"
	LocalEngineInitDefinition   = "local-engine-init.v1"
)

type DetectedEngine struct {
	Family           string   `json:"family"`
	Executable       string   `json:"executable"`
	WorkingDirectory string   `json:"working_directory"`
	RuntimeRoots     []string `json:"runtime_roots"`
	ModelsRoot       *string  `json:"models_root"`
	DataDir          *string  `json:"data_dir,omitempty"`
	Version          *string  `json:"version,omitempty"`
	ModelConfigs     []string `json:"model_configs,omitempty"`
	Label            string   `json:"label"`
}

type EngineDetectResult struct {
	Definition        string           `json:"definition"`
	Candidates        []DetectedEngine `json:"candidates"`
	ModelRequestsSent int              `json:"model_requests_sent"`
}

type InitInstallationResult struct {
	Definition        string          `json:"definition"`
	Out               string          `json:"out"`
	Family            string          `json:"family"`
	Installation      json.RawMessage `json:"installation"`
	Verified          bool            `json:"verified"`
	ModelRequestsSent int             `json:"model_requests_sent"`
}

func unwrapStrictCommandDetails(raw []byte, command, status string) ([]byte, error) {
	invalid := errors.New("invalid command payload")
	data := bytes.TrimSpace(raw)
	if len(data) == 0 || data[0] != '{' || !utf8.Valid(data) {
		return nil, invalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(d); err != nil {
		return nil, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, invalid
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return nil, invalid
	}
	if _, hasDef := top["definition"]; hasDef {
		return data, nil
	}
	if _, hasCmd := top["command"]; !hasCmd {
		return nil, invalid
	}
	if !discoveryFields(data, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") {
		return nil, invalid
	}
	r, err := Parse(data)
	if err != nil || r.Command != command || r.Status != status || r.Completeness != "complete" ||
		r.RunID != nil || r.EvidenceDir != nil || r.Limitations == nil || len(r.Limitations) != 0 {
		return nil, invalid
	}
	details := bytes.TrimSpace(r.Details)
	if len(details) == 0 || details[0] != '{' {
		return nil, invalid
	}
	return details, nil
}

func isStrictBoolJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return bytes.Equal(trimmed, []byte("true")) || bytes.Equal(trimmed, []byte("false"))
}

func ParseEngineDetect(raw []byte) (EngineDetectResult, error) {
	invalid := errors.New("invalid engine detect result")
	data, err := unwrapStrictCommandDetails(raw, "engine detect", "detected")
	if err != nil || !discoveryFields(data, "definition", "candidates", "model_requests_sent") {
		return EngineDetectResult{}, invalid
	}
	var wire struct {
		Definition        string            `json:"definition"`
		Candidates        []json.RawMessage `json:"candidates"`
		ModelRequestsSent *int              `json:"model_requests_sent"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.Definition != LocalEngineDetectDefinition ||
		wire.Candidates == nil || wire.ModelRequestsSent == nil || *wire.ModelRequestsSent != 0 {
		return EngineDetectResult{}, invalid
	}
	out := EngineDetectResult{
		Definition:        wire.Definition,
		Candidates:        make([]DetectedEngine, 0, len(wire.Candidates)),
		ModelRequestsSent: *wire.ModelRequestsSent,
	}
	for _, rawCandidate := range wire.Candidates {
		cand, err := parseDetectedEngine(rawCandidate)
		if err != nil {
			return EngineDetectResult{}, invalid
		}
		out.Candidates = append(out.Candidates, cand)
	}
	return out, nil
}

func parseDetectedEngine(raw json.RawMessage) (DetectedEngine, error) {
	invalid := errors.New("invalid detected engine")
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return DetectedEngine{}, invalid
	}
	required := []string{"family", "executable", "working_directory", "runtime_roots", "models_root", "label"}
	optional := []string{"data_dir", "version", "model_configs"}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return DetectedEngine{}, invalid
		}
	}
	for key := range fields {
		found := false
		for _, allowed := range required {
			if key == allowed {
				found = true
				break
			}
		}
		if !found {
			for _, allowed := range optional {
				if key == allowed {
					found = true
					break
				}
			}
		}
		if !found {
			return DetectedEngine{}, invalid
		}
	}
	var c DetectedEngine
	if json.Unmarshal(raw, &c) != nil {
		return DetectedEngine{}, invalid
	}
	if !localFamily(c.Family) || !candidatePath(c.Executable) || !candidatePath(c.WorkingDirectory) || !candidateText(c.Label) {
		return DetectedEngine{}, invalid
	}
	if len(c.RuntimeRoots) == 0 {
		return DetectedEngine{}, invalid
	}
	for _, root := range c.RuntimeRoots {
		if !candidatePath(root) {
			return DetectedEngine{}, invalid
		}
	}
	if c.ModelsRoot != nil {
		if c.Family != "lmstudio" || !candidatePath(*c.ModelsRoot) {
			return DetectedEngine{}, invalid
		}
	}
	if c.DataDir != nil {
		if c.Family != PolystrataFamily || !candidatePath(*c.DataDir) {
			return DetectedEngine{}, invalid
		}
	}
	if c.Version != nil {
		if c.Family != PolystrataFamily || !candidateText(*c.Version) {
			return DetectedEngine{}, invalid
		}
	}
	if _, hasConfigs := fields["model_configs"]; hasConfigs {
		if c.Family != PolystrataFamily {
			return DetectedEngine{}, invalid
		}
		for _, cfg := range c.ModelConfigs {
			if !candidatePath(cfg) {
				return DetectedEngine{}, invalid
			}
		}
	}
	return c, nil
}

func ParseInitInstallation(raw []byte) (InitInstallationResult, error) {
	invalid := errors.New("invalid engine init-installation result")
	data, err := unwrapStrictCommandDetails(raw, "engine init-installation", "prepared")
	if err != nil || !discoveryFields(data, "definition", "out", "family", "installation", "verified", "model_requests_sent") {
		return InitInstallationResult{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || !isStrictBoolJSON(fields["verified"]) {
		return InitInstallationResult{}, invalid
	}
	var wire struct {
		Definition        string          `json:"definition"`
		Out               string          `json:"out"`
		Family            string          `json:"family"`
		Installation      json.RawMessage `json:"installation"`
		Verified          bool            `json:"verified"`
		ModelRequestsSent *int            `json:"model_requests_sent"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.Definition != LocalEngineInitDefinition ||
		!candidatePath(wire.Out) || !localFamily(wire.Family) ||
		wire.ModelRequestsSent == nil || *wire.ModelRequestsSent != 0 {
		return InitInstallationResult{}, invalid
	}
	var decl struct {
		Definition string `json:"definition"`
		Family     string `json:"family"`
	}
	trimmedDecl := bytes.TrimSpace(wire.Installation)
	if len(trimmedDecl) == 0 || trimmedDecl[0] != '{' ||
		json.Unmarshal(trimmedDecl, &decl) != nil ||
		decl.Definition != "local-engine-installation.v1" || decl.Family != wire.Family {
		return InitInstallationResult{}, invalid
	}
	return InitInstallationResult{
		Definition:        wire.Definition,
		Out:               wire.Out,
		Family:            wire.Family,
		Installation:      trimmedDecl,
		Verified:          wire.Verified,
		ModelRequestsSent: *wire.ModelRequestsSent,
	}, nil
}
