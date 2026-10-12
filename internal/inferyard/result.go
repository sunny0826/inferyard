package inferyard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

type CommandResult struct {
	Command      string          `json:"command"`
	Status       string          `json:"status"`
	Completeness string          `json:"completeness"`
	RunID        *string         `json:"run_id"`
	EvidenceDir  *string         `json:"evidence_dir"`
	Limitations  []string        `json:"limitations"`
	Details      json.RawMessage `json:"details"`
}

func Parse(stdout []byte) (CommandResult, error) {
	var result CommandResult
	data := bytes.TrimSpace(stdout)
	if !utf8.Valid(data) || len(data) == 0 || data[0] != '{' {
		return result, errors.New("stdout must be one UTF-8 JSON object")
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, err
	}
	if result.Command == "" || result.Status == "" || result.Completeness == "" {
		return result, errors.New("missing command result fields")
	}
	return result, nil
}

type DevicePreflight struct {
	Kind                 string          `json:"kind"`
	Hardware             json.RawMessage `json:"hardware"`
	Models               json.RawMessage `json:"models"`
	Recommendation       json.RawMessage `json:"recommendation"`
	PlatformCapabilities json.RawMessage `json:"platform_capabilities"`
}

func ParseDeviceCheck(result CommandResult) (DevicePreflight, error) {
	var device DevicePreflight
	if result.Command != "device-check" {
		return device, errors.New("not a device-check result")
	}
	if err := json.Unmarshal(result.Details, &device); err != nil {
		return device, err
	}
	if device.Kind != "device_preflight.v1" {
		return device, errors.New("unsupported device preflight kind")
	}
	for name, field := range map[string]json.RawMessage{"hardware": device.Hardware, "models": device.Models, "recommendation": device.Recommendation, "platform_capabilities": device.PlatformCapabilities} {
		trimmed := bytes.TrimSpace(field)
		expected := byte('{')
		if name == "models" {
			expected = '['
		}
		if len(trimmed) == 0 || trimmed[0] != expected {
			return device, fmt.Errorf("invalid device field %s", name)
		}
	}
	return device, nil
}
func (c Client) DeviceCheck(ctx context.Context) (DevicePreflight, Execution, error) {
	out, err := c.Exec(ctx, "device-check", "--json")
	if err != nil {
		return DevicePreflight{}, out, err
	}
	result, err := Parse(out.Stdout)
	if err != nil {
		return DevicePreflight{}, out, err
	}
	device, err := ParseDeviceCheck(result)
	return device, out, err
}
