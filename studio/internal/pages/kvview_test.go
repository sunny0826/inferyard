package pages

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFormatBytes(t *testing.T) {
	for _, tc := range []struct {
		value *int64
		want  string
	}{
		{nil, "缺测"},
		{ptr(int64(0)), "0 bytes"},
		{ptr(int64(512)), "512 bytes"},
		{ptr(int64(1048576)), "1.00 MiB（1048576 bytes）"},
		{ptr(int64(1073741824)), "1.00 GiB（1073741824 bytes）"},
	} {
		if got := formatBytes(tc.value); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}

func TestKVTreeExpansionAndFrames(t *testing.T) {
	data := map[string]any{"hardware": map[string]any{"memory_total_bytes": int64(2147483648), "memory_free_bytes": nil}, "models": []any{map[string]any{"name": "fixture.gguf"}}, "empty": []any{}}
	tt := ui.NewTester(func(c *ui.Context) { kvview(c, "kv-test", data) }, 1100, 800)
	for _, want := range []string{"hardware", "memory_total_bytes", "2.00 GiB", "缺测", "models", "[0]", "fixture.gguf", "无"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q: %v", want, tt.Texts())
		}
	}
	if tt.HasText("hardware / memory_total_bytes") {
		t.Fatal("flattened path remains")
	}
	if err := tt.Click("hardware"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if tt.HasText("memory_total_bytes") {
		t.Fatal("collapse did not survive frame", tt.Texts())
	}
	data["aaa"] = "inserted sibling"
	tt.Frame()
	if tt.HasText("memory_total_bytes") {
		t.Fatal("sibling insertion changed expansion identity")
	}
	tt.Key(0, ui.KeyRight)
	if !tt.HasText("memory_total_bytes") {
		t.Fatal("keyboard expansion failed", tt.Texts())
	}
	delete(data, "hardware")
	tt.Frame()
	data["hardware"] = map[string]any{"memory_available_bytes": int64(1048576)}
	tt.Frame()
	if !tt.HasText("1.00 MiB") {
		t.Fatal("returning node lost data", tt.Texts())
	}
}

func TestKVFormAndJSONNumbers(t *testing.T) {
	for _, value := range []any{map[string]string{"path": "/workspace"}, json.RawMessage(`{"exact":9007199254740993,"disk_free_bytes":1073741824,"null":null,"flag":false}`)} {
		tt := ui.NewTester(func(c *ui.Context) { kvview(c, "kv-test", value) }, 1000, 700)
		if _, ok := value.(map[string]string); ok {
			if !tt.HasText("path") || !tt.HasText("/workspace") {
				t.Fatal(tt.Texts())
			}
		} else {
			for _, want := range []string{"9007199254740993", "1.00 GiB", "缺测", "false"} {
				if !tt.HasText(want) {
					t.Fatal(want, tt.Texts())
				}
			}
		}
	}
	for _, value := range []any{math.NaN(), math.Inf(1), json.RawMessage(`{"bad":NaN}`)} {
		tt := ui.NewTester(func(c *ui.Context) { kvview(c, "kv-test", value) }, 500, 500)
		if len(tt.Texts()) != 0 {
			t.Fatal("invalid JSON rendered", tt.Texts())
		}
	}
}
