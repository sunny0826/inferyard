package e2e

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// artifactSchema maps workspace evidence files to their wire schema kind in
// cli/schemas. Mappings were verified against the CLI's own
// validate_document: events.jsonl lines are event, observations.jsonl lines
// are observation_record, memory.jsonl lines are sample, request-bodies.jsonl
// lines are prepared_request. Documents without a wire schema (preparation
// records, device preflight, report index, environment/schedule journals) are
// counted as skipped, not validated.
var artifactSchema = map[string]string{
	"inputs.json":          "inputs",
	"manifest.json":        "manifest",
	"plan.json":            "plan",
	"events.jsonl":         "event",
	"observations.jsonl":   "observation_record",
	"memory.jsonl":         "sample",
	"request-bodies.jsonl": "prepared_request",
}

// assertEvidenceContracts validates every schema-covered evidence document
// under workspace against the exported JSON Schemas in cli/schemas. A
// violation here is a CLI-side contract breach by construction: Studio never
// writes these files. JSON duplicate-key rejection stays with the CLI writer;
// encoding/json cannot observe duplicates.
func assertEvidenceContracts(t *testing.T, workspace string) {
	t.Helper()
	schemasDir := filepath.Join(repoRoot(t), "cli", "schemas")
	if _, err := os.Stat(schemasDir); err != nil {
		t.Fatalf("cli/schemas not found: %v", err)
	}
	schemas := map[string]*jsonschema.Schema{}
	compile := func(kind string) *jsonschema.Schema {
		if compiled, ok := schemas[kind]; ok {
			return compiled
		}
		raw, err := os.ReadFile(filepath.Join(schemasDir, kind+".schema.json"))
		if err != nil {
			t.Fatalf("schema %s: %v", kind, err)
		}
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatalf("schema %s parse: %v", kind, err)
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(kind+".schema.json", doc); err != nil {
			t.Fatalf("schema %s resource: %v", kind, err)
		}
		compiled, err := compiler.Compile(kind + ".schema.json")
		if err != nil {
			t.Fatalf("schema %s compile: %v", kind, err)
		}
		schemas[kind] = compiled
		return compiled
	}

	validated, skipped := 0, 0
	err := filepath.WalkDir(workspace, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		base := entry.Name()
		kind, ok := artifactSchema[base]
		if !ok && filepath.Base(filepath.Dir(path)) == "bundles" && strings.HasSuffix(base, ".json") {
			ok, kind = true, "bundle"
		}
		if !ok {
			skipped++
			return nil
		}
		schema := compile(kind)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return nil
		}
		if strings.HasSuffix(base, ".jsonl") {
			for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				validateContract(t, schema, path, i+1, line)
				validated++
			}
			return nil
		}
		validateContract(t, schema, path, 0, string(data))
		validated++
		return nil
	})
	if err != nil {
		t.Fatalf("walk workspace: %v", err)
	}
	if validated == 0 {
		t.Fatal("schema gate validated nothing; workspace layout changed?")
	}
	t.Logf("schema gate: %d documents/lines validated, %d files skipped (no wire schema)", validated, skipped)
}

func validateContract(t *testing.T, schema *jsonschema.Schema, path string, line int, raw string) {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Errorf("%s: invalid JSON: %v", path, err)
		return
	}
	if err := schema.Validate(doc); err != nil {
		where := path
		if line > 0 {
			where += ":L" + strconv.Itoa(line)
		}
		t.Errorf("contract violation in %s: %v", where, err)
	}
}
