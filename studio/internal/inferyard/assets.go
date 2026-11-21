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

type AssetScan struct {
	Definition        string          `json:"definition"`
	Models            []ScannedModel  `json:"models"`
	Engines           []ScannedEngine `json:"engines"`
	ModelRequestsSent int             `json:"model_requests_sent"`
}

var (
	assetScanModelKeys  = []string{"path", "name", "format", "size_bytes", "architecture", "context_length", "available", "reason"}
	assetScanEngineKeys = []string{"path", "receipt", "modified_ns"}
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
	if !discoveryFields(data, "definition", "models", "engines", "model_requests_sent") {
		return AssetScan{}, invalid
	}
	var wire struct {
		Definition        string            `json:"definition"`
		Models            []json.RawMessage `json:"models"`
		Engines           []json.RawMessage `json:"engines"`
		ModelRequestsSent *int              `json:"model_requests_sent"`
	}
	if json.Unmarshal(data, &wire) != nil || wire.Definition != AssetScanDefinition ||
		wire.Models == nil || wire.Engines == nil || wire.ModelRequestsSent == nil || *wire.ModelRequestsSent != 0 {
		return AssetScan{}, invalid
	}
	out := AssetScan{
		Definition:        wire.Definition,
		Models:            make([]ScannedModel, 0, len(wire.Models)),
		Engines:           make([]ScannedEngine, 0, len(wire.Engines)),
		ModelRequestsSent: *wire.ModelRequestsSent,
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
