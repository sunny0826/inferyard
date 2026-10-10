package observe

import (
	"encoding/json"
	"os"
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
	encoded, _ := json.Marshal(binding)
	t.Log(string(encoded))
}
