package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

const AssetScanDefinition = "asset-scan.v1"

type ScannedModel struct {
	Path          string  `json:"path"`
	Name          string  `json:"name"`
	Format        string  `json:"format"`
	SizeBytes     *int64  `json:"size_bytes"`
	Architecture  *string `json:"architecture"`
	ContextLength *int64  `json:"context_length"`
	Available     bool    `json:"available"`
	Reason        *string `json:"reason"`
}

type ScannedEngine struct {
	Path       string  `json:"path"`
	Receipt    *string `json:"receipt"`
	ModifiedNs int64   `json:"modified_ns"`
}

type ScannedRoot struct {
	Path           string `json:"path"`
	Kind           string `json:"kind"`
	DiskUsageBytes int64  `json:"disk_usage_bytes"`
	DiskFreeBytes  *int64 `json:"disk_free_bytes"`
}

type AssetScan struct {
	Definition        string          `json:"definition"`
	Models            []ScannedModel  `json:"models"`
	Engines           []ScannedEngine `json:"engines"`
	Roots             []ScannedRoot   `json:"roots,omitempty"`
	TotalModelBytes   *int64          `json:"total_model_bytes,omitempty"`
	ModelRequestsSent int             `json:"model_requests_sent"`
}

var (
	assetScanModelKeys  = []string{"path", "name", "format", "size_bytes", "architecture", "context_length", "available", "reason"}
	assetScanEngineKeys = []string{"path", "receipt", "modified_ns"}
	assetScanRootKeys   = []string{"path", "kind", "disk_usage_bytes", "disk_free_bytes"}
)

func ParseAssetScan(raw []byte) (AssetScan, error) {
	invalid := errors.New("invalid asset scan result")
	data := bytes.TrimSpace(raw)
	if len(data) == 0 || data[0] != '{' || !utf8.Valid(data) {
		return AssetScan{}, invalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(d); err != nil {
		return AssetScan{}, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return AssetScan{}, invalid
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(data, &top) != nil {
		return AssetScan{}, invalid
	}
	if _, hasDef := top["definition"]; !hasDef {
		if _, hasCmd := top["command"]; hasCmd {
			r, err := Parse(data)
			if err != nil || r.Command != "assets scan" || r.Status != "scanned" || r.Completeness != "complete" {
				return AssetScan{}, invalid
			}
			details := bytes.TrimSpace(r.Details)
			if len(details) == 0 || details[0] != '{' {
				return AssetScan{}, invalid
			}
			data = details
		}
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return AssetScan{}, invalid
	}
	expected := []string{"definition", "models", "engines", "model_requests_sent"}
	_, hasRoots := obj["roots"]
	if hasRoots {
		expected = append(expected, "roots")
	}
	_, hasTotal := obj["total_model_bytes"]
	if hasTotal {
		if bytes.Equal(bytes.TrimSpace(obj["total_model_bytes"]), []byte("null")) {
			return AssetScan{}, invalid
		}
		expected = append(expected, "total_model_bytes")
	}
	if !discoveryFields(data, expected...) {
		return AssetScan{}, invalid
	}
	var wire struct {
		Definition        string            `json:"definition"`
		Models            []json.RawMessage `json:"models"`
		Engines           []json.RawMessage `json:"engines"`
		Roots             []json.RawMessage `json:"roots"`
		TotalModelBytes   *int64            `json:"total_model_bytes"`
		ModelRequestsSent *int              `json:"model_requests_sent"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.Definition != AssetScanDefinition ||
		wire.Models == nil || wire.Engines == nil || wire.ModelRequestsSent == nil || *wire.ModelRequestsSent != 0 {
		return AssetScan{}, invalid
	}
	if hasRoots && wire.Roots == nil {
		return AssetScan{}, invalid
	}
	if hasTotal && (wire.TotalModelBytes == nil || *wire.TotalModelBytes < 0) {
		return AssetScan{}, invalid
	}
	out := AssetScan{
		Definition:        wire.Definition,
		Models:            make([]ScannedModel, 0, len(wire.Models)),
		Engines:           make([]ScannedEngine, 0, len(wire.Engines)),
		TotalModelBytes:   wire.TotalModelBytes,
		ModelRequestsSent: *wire.ModelRequestsSent,
	}
	if hasRoots {
		out.Roots = make([]ScannedRoot, 0, len(wire.Roots))
		for _, rawRoot := range wire.Roots {
			root, err := parseScannedRoot(rawRoot)
			if err != nil {
				return AssetScan{}, invalid
			}
			out.Roots = append(out.Roots, root)
		}
	}
	for _, rawModel := range wire.Models {
		model, err := parseScannedModel(rawModel)
		if err != nil {
			return AssetScan{}, invalid
		}
		out.Models = append(out.Models, model)
	}
	for _, rawEngine := range wire.Engines {
		engine, err := parseScannedEngine(rawEngine)
		if err != nil {
			return AssetScan{}, invalid
		}
		out.Engines = append(out.Engines, engine)
	}
	return out, nil
}

func parseScannedModel(raw json.RawMessage) (ScannedModel, error) {
	invalid := errors.New("invalid scanned model")
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return ScannedModel{}, invalid
	}
	for k := range obj {
		if !slices.Contains(assetScanModelKeys, k) {
			return ScannedModel{}, invalid
		}
	}
	for _, req := range []string{"path", "format", "available"} {
		if _, ok := obj[req]; !ok {
			return ScannedModel{}, invalid
		}
	}
	availRaw := bytes.TrimSpace(obj["available"])
	if !bytes.Equal(availRaw, []byte("true")) && !bytes.Equal(availRaw, []byte("false")) {
		return ScannedModel{}, invalid
	}
	var m ScannedModel
	if json.Unmarshal(raw, &m) != nil {
		return ScannedModel{}, invalid
	}
	if strings.TrimSpace(m.Path) == "" || (m.Format != "gguf" && m.Format != "directory") {
		return ScannedModel{}, invalid
	}
	if nameRaw, hasName := obj["name"]; hasName && (bytes.Equal(bytes.TrimSpace(nameRaw), []byte("null")) || strings.TrimSpace(m.Name) == "") {
		return ScannedModel{}, invalid
	}
	if m.SizeBytes != nil && *m.SizeBytes < 0 {
		return ScannedModel{}, invalid
	}
	if m.ContextLength != nil && *m.ContextLength <= 0 {
		return ScannedModel{}, invalid
	}
	if m.Available && m.Reason != nil && *m.Reason != "" {
		return ScannedModel{}, invalid
	}
	if !m.Available && (m.Reason == nil || strings.TrimSpace(*m.Reason) == "") {
		return ScannedModel{}, invalid
	}
	return m, nil
}

func parseScannedEngine(raw json.RawMessage) (ScannedEngine, error) {
	invalid := errors.New("invalid scanned engine")
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return ScannedEngine{}, invalid
	}
	for k := range obj {
		if !slices.Contains(assetScanEngineKeys, k) {
			return ScannedEngine{}, invalid
		}
	}
	for _, req := range []string{"path", "modified_ns"} {
		if _, ok := obj[req]; !ok {
			return ScannedEngine{}, invalid
		}
	}
	if bytes.Equal(bytes.TrimSpace(obj["modified_ns"]), []byte("null")) {
		return ScannedEngine{}, invalid
	}
	var e ScannedEngine
	if json.Unmarshal(raw, &e) != nil {
		return ScannedEngine{}, invalid
	}
	if strings.TrimSpace(e.Path) == "" || e.ModifiedNs < 0 {
		return ScannedEngine{}, invalid
	}
	if e.Receipt != nil && strings.TrimSpace(*e.Receipt) == "" {
		return ScannedEngine{}, invalid
	}
	return e, nil
}

func parseScannedRoot(raw json.RawMessage) (ScannedRoot, error) {
	invalid := errors.New("invalid scanned root")
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return ScannedRoot{}, invalid
	}
	for k := range obj {
		if !slices.Contains(assetScanRootKeys, k) {
			return ScannedRoot{}, invalid
		}
	}
	for _, req := range []string{"path", "kind", "disk_usage_bytes"} {
		v, ok := obj[req]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ScannedRoot{}, invalid
		}
	}
	var r ScannedRoot
	if json.Unmarshal(raw, &r) != nil {
		return ScannedRoot{}, invalid
	}
	if strings.TrimSpace(r.Path) == "" || (r.Kind != "models" && r.Kind != "engines") || r.DiskUsageBytes < 0 {
		return ScannedRoot{}, invalid
	}
	if r.DiskFreeBytes != nil && *r.DiskFreeBytes < 0 {
		return ScannedRoot{}, invalid
	}
	return r, nil
}
