package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictJSONRejectsAmbiguousEvidence(t *testing.T) {
	for _, data := range []string{`{"a":1,"a":2}`, `{"a":[{"b":1,"b":2}]}`, `{"a":NaN}`, `{"ignored":1e999}`, `{"a":1} {}`, "{\"a\":\"\xff\"}"} {
		var value any
		if StrictJSON([]byte(data), &value) == nil {
			t.Fatal(data)
		}
	}
	var typed struct {
		A int `json:"a"`
	}
	if StrictJSON([]byte(`{"a":true}`), &typed) == nil {
		t.Fatal("boolean as integer")
	}
	if err := StrictJSON([]byte(`{"a":2}`), &typed); err != nil || typed.A != 2 {
		t.Fatal(typed, err)
	}
}

func TestReadBindingPinsRawFilesAndRefusesOldTypes(t *testing.T) {
	root := t.TempDir()
	run := `{"schema_version":3,"run_id":"r"}`
	config := `{"schema_version":3,"endpoint":{"server_pid":42,"process_start_ticks":8},"model":{"display_name":"declared"},"engine":{"backend":"cpu"}}`
	if err := os.WriteFile(filepath.Join(root, "run.json"), []byte(run), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.frozen.json")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	b, target, err := ReadBinding(root)
	if err != nil || target.PID != 42 || target.StartTicks != 8 || b.RunSHA256 != digest([]byte(run)) || b.ConfigSHA256 != digest([]byte(config)) {
		t.Fatal(b, target, err)
	}
	for _, bad := range []string{strings.Replace(config, `"schema_version":3`, `"schema_version":2`, 1), strings.Replace(config, `"server_pid":42`, `"server_pid":true`, 1), strings.Replace(config, `"process_start_ticks":8`, `"process_start_ticks":0`, 1), strings.Replace(config, `"server_pid":42`, `"server_pid":4294967298`, 1), config[:len(config)-1] + `,"endpoint":{}}`, strings.Repeat(" ", 1024*1024+1)} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadBinding(root); err == nil {
			t.Fatal("accepted malformed binding")
		}
	}
}
