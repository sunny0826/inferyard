package inferyard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The CLI and Studio ship from one monorepo, so these tests read the CLI's
// frozen contract instead of skipping when a sibling checkout is absent.
const cliRoot = "../../../cli"

func TestConstantsMatchCLIContract(t *testing.T) {
	t.Run("context_speed_schema", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(cliRoot, "schemas", "context_speed_result.schema.json"))
		if err != nil {
			t.Fatalf("read CLI context-speed schema: %v", err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("parse CLI context-speed schema: %v", err)
		}
		// oneOf[0] is the readable document; oneOf[1] is the error projection.
		readable := at(t, schema, "oneOf").([]any)[0].(map[string]any)
		metadata := at(t, readable, "properties", "metadata", "properties").(map[string]any)
		adapter := at(t, metadata, "engine", "properties", "adapter", "enum")
		if got := enumStrings(t, adapter); !equalStrings(got, []string{ContextSpeedAdapter}) {
			t.Fatalf("adapter enum = %v, Go constant = %q", got, ContextSpeedAdapter)
		}
		bundle := at(t, metadata, "bundle", "properties").(map[string]any)
		if got := enumStrings(t, at(t, bundle, "bundle_id", "enum")); !equalStrings(got, []string{ContextSpeedBundleID}) {
			t.Fatalf("bundle id enum = %v, Go constant = %q", got, ContextSpeedBundleID)
		}
		if got := enumStrings(t, at(t, bundle, "version", "enum")); !equalStrings(got, []string{ContextSpeedBundleVersion}) {
			t.Fatalf("bundle version enum = %v, Go constant = %q", got, ContextSpeedBundleVersion)
		}
		if got := enumInts(t, at(t, metadata, "selected_context_tokens", "enum")); !equalInts(got, ContextSpeedTokens()) {
			t.Fatalf("context tokens enum = %v, Go constants = %v", got, ContextSpeedTokens())
		}
		caseIDs := enumStrings(t, at(t, readable, "properties", "samples", "items", "properties", "case_id", "enum"))
		expected := make([]string, 0, len(ContextSpeedTokens()))
		for _, tokens := range ContextSpeedTokens() {
			id, ok := ContextSpeedCaseID(tokens)
			if !ok {
				t.Fatalf("Go constants lack a case for %d tokens", tokens)
			}
			expected = append(expected, id)
		}
		if !equalStrings(caseIDs, expected) {
			t.Fatalf("case id enum = %v, Go mapping = %v", caseIDs, expected)
		}
		// The schema expresses the frozen sample count as counts.planned.
		planned := at(t, readable, "properties", "execution", "properties", "counts", "properties", "planned").(map[string]any)
		if got := int(planned["const"].(float64)); got != ContextSpeedSamples {
			t.Fatalf("planned sample count = %d, Go constant = %d", got, ContextSpeedSamples)
		}
	})

	t.Run("context_speed_case_mapping", func(t *testing.T) {
		source := readCLI(t, "src/inferyard/contracts/context_speed_profile.py")
		match := regexp.MustCompile(`(?m)^CASES = \{([^}]*)\}`).FindStringSubmatch(string(source))
		if match == nil {
			t.Fatal("CLI context-speed profile has no CASES mapping")
		}
		pairs := regexp.MustCompile(`(\d+):\s*"([^"]+)"`).FindAllStringSubmatch(match[1], -1)
		if len(pairs) != len(ContextSpeedTokens()) {
			t.Fatalf("CLI CASES = %v, Go constants = %v", pairs, ContextSpeedTokens())
		}
		for _, pair := range pairs {
			tokens, err := strconv.Atoi(pair[1])
			if err != nil {
				t.Fatalf("CLI CASES token %q: %v", pair[1], err)
			}
			id, ok := ContextSpeedCaseID(tokens)
			if !ok || id != pair[2] {
				t.Fatalf("CLI CASES[%d] = %q, Go mapping = %q (known %v)", tokens, pair[2], id, ok)
			}
		}
	})

	t.Run("default_bundles", func(t *testing.T) {
		source := readCLI(t, "src/inferyard/contracts/bundle.py")
		match := regexp.MustCompile(`(?m)^DEFAULT_BUNDLES = \(([^)]*)\)`).FindStringSubmatch(string(source))
		if match == nil {
			t.Fatal("CLI bundle contract has no DEFAULT_BUNDLES tuple")
		}
		var bundles []string
		for _, quoted := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(match[1], -1) {
			bundles = append(bundles, quoted[1])
		}
		if !equalStrings(bundles, DefaultBundles) {
			t.Fatalf("CLI DEFAULT_BUNDLES = %v, Go DefaultBundles = %v", bundles, DefaultBundles)
		}
	})
}

func readCLI(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cliRoot, path))
	if err != nil {
		t.Fatalf("read CLI source %s: %v", path, err)
	}
	return raw
}

func at(t *testing.T, value any, keys ...string) any {
	t.Helper()
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("schema path %v: %T is not an object", keys, value)
		}
		value, ok = object[key]
		if !ok {
			t.Fatalf("schema path %v: missing %q", keys, key)
		}
	}
	return value
}

func enumStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("enum is %T, want array", value)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("enum item %v is %T, want string", item, item)
		}
		out = append(out, text)
	}
	return out
}

func enumInts(t *testing.T, value any) []int {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("enum is %T, want array", value)
	}
	out := make([]int, 0, len(items))
	for _, item := range items {
		number, ok := item.(float64)
		if !ok {
			t.Fatalf("enum item %v is %T, want number", item, item)
		}
		out = append(out, int(number))
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
