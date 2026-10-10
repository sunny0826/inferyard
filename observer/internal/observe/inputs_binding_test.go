package observe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bindingInputs(t *testing.T) map[string]any {
	t.Helper()
	documents := map[string]any{}
	for _, name := range []string{"run", "plan", "selection", "config", "bundle"} {
		payload := map[string]any{"schema_version": 3}
		if name == "run" {
			payload["run_id"] = "新-run"
		}
		if name == "config" {
			payload["endpoint"] = map[string]any{"server_pid": 42, "process_start_ticks": 8}
			payload["model"] = map[string]any{"display_name": "<鹈鹕>"}
			payload["engine"] = map[string]any{"backend": "cpu"}
		}
		raw, _ := json.Marshal(payload)
		canonical, err := memberBytes(raw)
		if err != nil {
			t.Fatal(err)
		}
		documents[name] = map[string]any{"payload": payload, "sha256": digest(canonical)}
	}
	return map[string]any{"schema_version": 3, "evidence_layout_version": 2,
		"run_id": "新-run", "documents": documents, "configuration_source": nil,
		"configuration_source_missing_reason": "not_persisted"}
}

func writeBindingInputs(t *testing.T, root string, value map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inputs.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestInputsBindingPinsContainerAndMembers(t *testing.T) {
	root := t.TempDir()
	inputs := bindingInputs(t)
	raw := writeBindingInputs(t, root, inputs)
	binding, target, err := ReadBinding(root)
	if err != nil || binding.Definition != "benchmark-inputs-binding.v1" ||
		binding.InputsSHA256 != digest(raw) || binding.RunSHA256 != "" || binding.ConfigSHA256 != "" ||
		binding.ModelLabel != "<鹈鹕>" || target.PID != 42 || target.StartTicks != 8 {
		t.Fatal(binding, target, err)
	}
	output, _ := json.Marshal(binding)
	var fields map[string]any
	json.Unmarshal(output, &fields)
	if len(fields) != 7 {
		t.Fatal(string(output))
	}
	for _, name := range []string{"run", "config"} {
		expected := inputs["documents"].(map[string]any)[name].(map[string]any)["sha256"]
		actual := binding.RunMemberSHA256
		if name == "config" {
			actual = binding.ConfigMemberSHA256
		}
		if actual != expected {
			t.Fatal(actual, expected)
		}
	}
}

func TestInputsBindingRejectsCorruptionWithoutLegacyFallback(t *testing.T) {
	for _, damage := range []string{"member_hash", "member_payload", "missing_member", "unknown", "mixed", "manifest_conflict", "source_hash", "source_missing_reason", "bool_pid", "unicode", "duplicate", "nonfinite"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			inputs := bindingInputs(t)
			docs := inputs["documents"].(map[string]any)
			config := docs["config"].(map[string]any)
			switch damage {
			case "member_hash":
				config["sha256"] = strings.Repeat("0", 64)
			case "member_payload":
				config["payload"].(map[string]any)["ignored"] = "changed"
			case "missing_member":
				delete(docs, "bundle")
			case "unknown":
				inputs["evidence_layout_version"] = 99
			case "mixed":
				os.WriteFile(filepath.Join(root, "run.json"), []byte(`{}`), 0600)
			case "manifest_conflict":
				os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"schema_version":3,"evidence_layout_version":1,"run_id":"新-run"}`), 0600)
			case "source_hash":
				inputs["configuration_source_missing_reason"] = nil
				inputs["configuration_source"] = map[string]any{"media_type": "application/toml", "encoding": "base64", "content": "YQ==", "sha256": strings.Repeat("0", 64), "bytes": 1}
			case "source_missing_reason":
				inputs["configuration_source_missing_reason"] = nil
			case "bool_pid":
				config["payload"].(map[string]any)["endpoint"].(map[string]any)["server_pid"] = true
				raw, _ := json.Marshal(config["payload"])
				canonical, _ := memberBytes(raw)
				config["sha256"] = digest(canonical)
			}
			raw := writeBindingInputs(t, root, inputs)
			if damage == "unicode" || damage == "duplicate" || damage == "nonfinite" {
				suffix := map[string]string{"unicode": `,"ignored":"\ud800"}`, "duplicate": `,"run_id":"again"}`, "nonfinite": `,"ignored":1e999}`}[damage]
				raw = append(raw[:len(raw)-1], []byte(suffix)...)
				os.WriteFile(filepath.Join(root, "inputs.json"), raw, 0600)
			}
			if _, _, err := ReadBinding(root); err == nil {
				t.Fatal("accepted corrupt inputs")
			}
		})
	}
}

func TestInputsBindingRejectsHistoricalSources(t *testing.T) {
	for _, name := range []string{"requests.jsonl", "migration.json", "token-budgets.json"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeBindingInputs(t, root, bindingInputs(t))
			if err := os.WriteFile(filepath.Join(root, name), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ReadBinding(root); err == nil {
				t.Fatal("accepted historical source", name)
			}
		})
	}
}

func TestInputsBindingChecksSealedPhysicalContainer(t *testing.T) {
	root := t.TempDir()
	raw := writeBindingInputs(t, root, bindingInputs(t))
	manifest := map[string]any{"schema_version": 3, "evidence_layout_version": 2, "run_id": "新-run",
		"files": map[string]any{"inputs.json": map[string]any{"sha256": digest(raw), "bytes": len(raw)}}}
	writeManifest := func() {
		value, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	if _, _, err := ReadBinding(root); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"sha256", "bytes"} {
		info := manifest["files"].(map[string]any)["inputs.json"].(map[string]any)
		saved := info[field]
		if field == "bytes" {
			info[field] = nil
		} else {
			info[field] = strings.Repeat("0", 64)
		}
		writeManifest()
		if _, _, err := ReadBinding(root); err == nil {
			t.Fatal(field)
		}
		info[field] = saved
	}
}

func TestInputsBindingDoesNotTurnNullSourceFieldsIntoEmptyBytes(t *testing.T) {
	for _, field := range []string{"content", "bytes"} {
		root := t.TempDir()
		inputs := bindingInputs(t)
		source := map[string]any{"media_type": "application/json", "encoding": "base64",
			"content": "", "bytes": 0, "sha256": digest(nil)}
		inputs["configuration_source"] = source
		inputs["configuration_source_missing_reason"] = nil
		writeBindingInputs(t, root, inputs)
		if _, _, err := ReadBinding(root); err != nil {
			t.Fatal(err)
		}
		source[field] = nil
		writeBindingInputs(t, root, inputs)
		if _, _, err := ReadBinding(root); err == nil {
			t.Fatal(field)
		}
	}
}
