package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type ServePlan struct {
	SchemaVersion    int               `json:"schema_version"`
	Argv             []string          `json:"argv"`
	Environment      map[string]string `json:"environment"`
	WorkingDirectory string            `json:"working_directory"`
	Endpoint         string            `json:"endpoint"`
	ModelPath        string            `json:"model_path"`
	ModelKind        string            `json:"model_kind"`
	ContextSize      int64             `json:"context_size"`
	Family           string            `json:"family"`
	Adapter          string            `json:"adapter"`
	Local            bool              `json:"local"`
}

var (
	servePlanRequiredKeys = []string{
		"schema_version",
		"argv",
		"environment",
		"working_directory",
		"endpoint",
		"model_path",
		"model_kind",
		"context_size",
		"family",
		"adapter",
		"local",
	}
	servePlanModelKinds = []string{"gguf", "directory", "strata_pack"}
	servePlanFamilies   = []string{"llama-cpp", "mlx-lm", "vllm", "sglang", "polystrata"}
	servePlanAdapters   = []string{"local_openai_v1", "prism_llama_server_v1"}
)

func validLoopbackEndpoint(raw string) bool {
	if raw == "" || strings.ContainsRune(raw, 0) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || raw != "http://"+u.Host {
		return false
	}
	var portStr string
	switch {
	case strings.HasPrefix(u.Host, "127.0.0.1:"):
		portStr = strings.TrimPrefix(u.Host, "127.0.0.1:")
	case strings.HasPrefix(u.Host, "localhost:"):
		portStr = strings.TrimPrefix(u.Host, "localhost:")
	case strings.HasPrefix(u.Host, "[::1]:"):
		portStr = strings.TrimPrefix(u.Host, "[::1]:")
	default:
		return false
	}
	if portStr == "" || strings.HasPrefix(portStr, "0") {
		return false
	}
	for i := 0; i < len(portStr); i++ {
		if portStr[i] < '0' || portStr[i] > '9' {
			return false
		}
	}
	port, err := strconv.Atoi(portStr)
	return err == nil && port >= 1 && port <= 65535
}

func ParseServePlan(raw []byte) (ServePlan, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d); err != nil {
		return ServePlan{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		return ServePlan{}, errors.New("trailing data in serve plan")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ServePlan{}, err
	}
	if len(fields) != len(servePlanRequiredKeys) {
		return ServePlan{}, errors.New("unexpected or missing serve plan fields")
	}
	for _, key := range servePlanRequiredKeys {
		val, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(val), []byte("null")) {
			return ServePlan{}, errors.New("missing required serve plan field: " + key)
		}
	}
	var rawEnv map[string]json.RawMessage
	if err := json.Unmarshal(fields["environment"], &rawEnv); err != nil || rawEnv == nil {
		return ServePlan{}, errors.New("invalid serve plan environment")
	}
	for _, v := range rawEnv {
		trimmed := bytes.TrimSpace(v)
		var strVal string
		if len(trimmed) == 0 || trimmed[0] != '"' || json.Unmarshal(trimmed, &strVal) != nil {
			return ServePlan{}, errors.New("serve plan environment values must be strings")
		}
	}
	var plan ServePlan
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return ServePlan{}, err
	}
	if plan.SchemaVersion != 3 || len(plan.Argv) == 0 ||
		plan.WorkingDirectory == "" || plan.ModelPath == "" || plan.ContextSize <= 0 ||
		!slices.Contains(servePlanModelKinds, plan.ModelKind) ||
		!slices.Contains(servePlanFamilies, plan.Family) ||
		!slices.Contains(servePlanAdapters, plan.Adapter) ||
		!validLoopbackEndpoint(plan.Endpoint) ||
		plan.Environment == nil {
		return ServePlan{}, errors.New("invalid serve plan fields")
	}
	for _, arg := range plan.Argv {
		if arg == "" {
			return ServePlan{}, errors.New("empty serve plan argument")
		}
	}
	return plan, nil
}

func SupportsServePlanSchema(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	var doc struct {
		ID         string                     `json:"$id"`
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	if doc.ID != "urn:local-ai-bench:schema:v3:serve_plan" || doc.Type != "object" {
		return false
	}
	for _, key := range servePlanRequiredKeys {
		if _, ok := doc.Properties[key]; !ok {
			return false
		}
	}
	return true
}
