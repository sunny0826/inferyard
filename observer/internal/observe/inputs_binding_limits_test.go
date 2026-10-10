package observe

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func setBindingMember(t *testing.T, inputs map[string]any, name string, padding string) int {
	t.Helper()
	member := inputs["documents"].(map[string]any)[name].(map[string]any)
	member["payload"].(map[string]any)["padding"] = padding
	raw, err := json.Marshal(member["payload"])
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := memberBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	member["sha256"] = digest(canonical)
	return len(canonical)
}

func TestInputsBindingAcceptsLargeAggregate(t *testing.T) {
	root := t.TempDir()
	inputs := bindingInputs(t)
	source := []byte("# " + strings.Repeat("x", 1<<20) + "\n")
	inputs["configuration_source"] = map[string]any{
		"media_type": "application/toml", "encoding": "base64",
		"content": base64.StdEncoding.EncodeToString(source), "sha256": digest(source), "bytes": len(source),
	}
	inputs["configuration_source_missing_reason"] = nil
	raw := writeBindingInputs(t, root, inputs)
	if len(raw) <= 1<<20 {
		t.Fatal("aggregate did not exceed legacy file cap")
	}
	binding, target, err := ReadBinding(root)
	if err != nil || binding.InputsSHA256 != digest(raw) || target.PID != 42 {
		t.Fatal(binding, target, err)
	}
}

func TestInputsBindingLimitsCanonicalRunAndConfigMembers(t *testing.T) {
	for _, name := range []string{"run", "config"} {
		for _, size := range []int{1 << 20, (1 << 20) + 1} {
			t.Run(name+"-"+strconv.Itoa(size), func(t *testing.T) {
				root := t.TempDir()
				inputs := bindingInputs(t)
				overhead := setBindingMember(t, inputs, name, "")
				if setBindingMember(t, inputs, name, strings.Repeat("x", size-overhead)) != size {
					t.Fatal("incorrect canonical size")
				}
				writeBindingInputs(t, root, inputs)
				_, _, err := ReadBinding(root)
				if size == 1<<20 && err != nil {
					t.Fatal(err)
				}
				if size > 1<<20 && (err == nil || err.Error() != "binding_member_too_large") {
					t.Fatal("accepted oversized logical member", err)
				}
			})
		}
	}
}

func TestInputsBindingMemberLimitUsesCanonicalBytes(t *testing.T) {
	root := t.TempDir()
	inputs := bindingInputs(t)
	size := setBindingMember(t, inputs, "config", strings.Repeat("<&>", 200000))
	if size >= 1<<20 {
		t.Fatal(size)
	}
	raw := writeBindingInputs(t, root, inputs)
	if len(raw) <= 1<<20 {
		t.Fatal("escaped raw JSON did not exceed member cap")
	}
	if _, _, err := ReadBinding(root); err != nil {
		t.Fatal(err)
	}
}

func TestInputsBindingLimitsOnlyRunAndConfigMembers(t *testing.T) {
	root := t.TempDir()
	inputs := bindingInputs(t)
	setBindingMember(t, inputs, "bundle", strings.Repeat("x", (1<<20)+1))
	writeBindingInputs(t, root, inputs)
	if _, _, err := ReadBinding(root); err != nil {
		t.Fatal(err)
	}
}

func TestInputsBindingWrapperLimit(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(strconv.Itoa(extra), func(t *testing.T) {
			root := t.TempDir()
			raw := writeBindingInputs(t, root, bindingInputs(t))
			f, err := os.OpenFile(filepath.Join(root, "inputs.json"), os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				t.Fatal(err)
			}
			remaining := (64 << 20) + extra - len(raw)
			block := strings.Repeat(" ", 65536)
			for remaining > 0 {
				size := min(remaining, len(block))
				if _, err := f.WriteString(block[:size]); err != nil {
					t.Fatal(err)
				}
				remaining -= size
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			binding, _, err := ReadBinding(root)
			if extra == 0 {
				if err != nil || binding.InputsSHA256 == digest(raw) {
					t.Fatal("exactly 64 MiB must bind the complete physical wrapper", binding, err)
				}
			} else if err == nil || err.Error() != "binding_file_too_large" {
				t.Fatal("accepted oversized wrapper", err)
			}
		})
	}
}

func TestLegacyBindingFilesKeepOneMiBLimit(t *testing.T) {
	run := `{"schema_version":3,"run_id":"r"}`
	config := `{"schema_version":3,"endpoint":{"server_pid":42,"process_start_ticks":8},"model":{"display_name":"declared"},"engine":{"backend":"cpu"}}`
	for _, name := range []string{"run.json", "config.frozen.json"} {
		root := t.TempDir()
		for filename, raw := range map[string]string{"run.json": run, "config.frozen.json": config} {
			if filename == name {
				raw += strings.Repeat(" ", (1<<20)+1-len(raw))
			}
			if err := os.WriteFile(filepath.Join(root, filename), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := ReadBinding(root); err == nil || err.Error() != "binding_file_too_large" {
			t.Fatal(name, err)
		}
	}
}

func TestStrictJSONKeepsDepthBound(t *testing.T) {
	var value any
	allowed := strings.Repeat("[", 128) + "0" + strings.Repeat("]", 128)
	if err := StrictJSON([]byte(allowed), &value); err != nil {
		t.Fatal(err)
	}
	denied := "[" + allowed + "]"
	if err := StrictJSON([]byte(denied), &value); err == nil || err.Error() != "json_nesting_limit" {
		t.Fatal("accepted excessive depth", err)
	}
}
