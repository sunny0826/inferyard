package inferyard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseServePlanAndSchemaSupport(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "..", "cli", "schemas", "serve_plan.schema.json")
	rawSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read exported serve_plan schema: %v", err)
	}
	if !SupportsServePlanSchema(rawSchema) {
		t.Fatalf("expected exported serve_plan schema to be supported")
	}
	if SupportsServePlanSchema([]byte(`{"$id":"wrong","type":"object","properties":{}}`)) {
		t.Fatalf("expected wrong schema to be rejected")
	}

	valid := []byte(`{
		"schema_version": 3,
		"argv": ["/usr/bin/llama-server", "-m", "/models/a.gguf", "-c", "8192"],
		"environment": {},
		"working_directory": "/usr/bin",
		"endpoint": "http://127.0.0.1:48857",
		"model_path": "/models/a.gguf",
		"model_kind": "gguf",
		"context_size": 8192,
		"family": "llama-cpp",
		"adapter": "prism_llama_server_v1",
		"local": false
	}`)
	plan, err := ParseServePlan(valid)
	if err != nil {
		t.Fatalf("ParseServePlan valid failed: %v", err)
	}
	if plan.ContextSize != 8192 || plan.Family != "llama-cpp" || plan.Local {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	for _, bad := range []string{
		`{"schema_version":2,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":[""],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","model_kind":"gguf","context_size":0,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1"}`,
		`{"schema_version":3,"argv":["x"],"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:1","model_path":"/m","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://0.0.0.0:8080","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.2:8080","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://localhost:8080?","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:8080","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":true,"LOCAL":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{"K":null},"working_directory":"/tmp","endpoint":"http://127.0.0.1:8080","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:0","model_path":"/m","model_kind":"gguf","context_size":1,"family":"llama-cpp","adapter":"prism_llama_server_v1","local":false}`,
		`{"schema_version":3,"argv":["x"],"environment":{},"working_directory":"/tmp","endpoint":"http://127.0.0.1:8080","model_path":"/m","model_kind":"gguf","context_size":1,"family":"lmstudio","adapter":"local_openai_v1","local":true}`,
	} {
		if _, err := ParseServePlan([]byte(bad)); err == nil {
			t.Fatalf("expected ParseServePlan to reject %s", bad)
		}
	}
}
