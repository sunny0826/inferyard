package observe

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPythonInputsBindingInterop(t *testing.T) {
	root := os.Getenv("INFERYARD_TEST_BINDING_ROOT")
	expectedPath := os.Getenv("INFERYARD_TEST_BINDING_EXPECTED")
	if root == "" || expectedPath == "" {
		t.Skip("explicit Python-generated fixture required")
	}
	raw, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Binding Binding `json:"binding"`
		Target  Target  `json:"target"`
	}
	if err := StrictJSON(raw, &expected); err != nil {
		t.Fatal(err)
	}
	binding, target, err := ReadBinding(root)
	if err != nil || binding != expected.Binding || target != expected.Target {
		t.Fatal(binding, target, err)
	}
	// Preserve Python declarations while changing the actual member payload.
	// No manifest is copied: rejection must come from the member hash itself.
	inputs, err := os.ReadFile(filepath.Join(root, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	var documents map[string]json.RawMessage
	var member map[string]json.RawMessage
	if StrictJSON(inputs, &envelope) != nil ||
		json.Unmarshal(envelope["documents"], &documents) != nil ||
		json.Unmarshal(documents["config"], &member) != nil {
		t.Fatal("invalid Python input fixture")
	}
	payload := bytes.TrimSpace(member["payload"])
	member["payload"] = append(append([]byte(nil), payload[:len(payload)-1]...),
		[]byte(`,"interop_tamper":"changed"}`)...)
	documents["config"], _ = json.Marshal(member)
	envelope["documents"], _ = json.Marshal(documents)
	changed, _ := json.Marshal(envelope)
	tampered := t.TempDir()
	if err := os.WriteFile(filepath.Join(tampered, "inputs.json"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadBinding(tampered); err == nil {
		t.Fatal("trusted declaration hash instead of Python member payload")
	}
	encoded, _ := json.Marshal(binding)
	t.Log(string(encoded))
}
