package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func postJSON(t *testing.T, url string, payload map[string]any) ([]byte, int) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return body, response.StatusCode
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	panic("non-numeric generation value")
}

// TestFakeEngineContract pins every adapter-facing guarantee the spike proved.
func TestFakeEngineContract(t *testing.T) {
	engine := StartFakeEngine(t)

	props := getJSON(t, engine.Endpoint+"/props")
	if props["build_info"] != fakeBuildInfo {
		t.Fatal("build_info mismatch")
	}
	if props["model_path"] != engine.Assets.Model {
		t.Fatal("model_path mismatch")
	}
	settings, ok := props["default_generation_settings"].(map[string]any)
	if !ok || settings["n_ctx"] != float64(fakeNCtx) {
		t.Fatal("n_ctx mismatch")
	}
	template, _ := props["chat_template"].(string)
	sum := sha256.Sum256([]byte(template))
	if hex.EncodeToString(sum[:]) != engine.Assets.TemplateSHA256 {
		t.Fatal("chat_template hash mismatch")
	}

	models := getJSON(t, engine.Endpoint+"/v1/models")
	data, _ := models["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["id"] != DisplayName {
		t.Fatal("models listing mismatch")
	}

	response, err := http.Get(engine.Endpoint + "/slots")
	if err != nil {
		t.Fatal(err)
	}
	var slots []map[string]any
	json.NewDecoder(response.Body).Decode(&slots)
	response.Body.Close()
	params := slots[0]["params"].(map[string]any)
	for key, want := range DefaultGeneration() {
		if params[key] != toFloat(want) {
			t.Fatalf("slots param %s = %v, want %v", key, params[key], want)
		}
	}

	messages := []map[string]any{{"role": "user", "content": "你好"}}
	rendered := renderMessages([]any{messages[0]})
	templateBody, _ := postJSON(t, engine.Endpoint+"/apply-template", map[string]any{
		"messages": messages, "add_generation_prompt": true,
	})
	var applied map[string]any
	json.Unmarshal(templateBody, &applied)
	if applied["prompt"] != rendered {
		t.Fatal("apply-template render mismatch")
	}
	tokenBody, _ := postJSON(t, engine.Endpoint+"/tokenize", map[string]any{
		"content": rendered, "add_special": true, "parse_special": true,
	})
	var tokenized map[string]any
	json.Unmarshal(tokenBody, &tokenized)
	want := len(tokenized["tokens"].([]any))
	if want != tokenCount(rendered) {
		t.Fatal("tokenize count mismatch")
	}

	chatBody, status := postJSON(t, engine.Endpoint+"/v1/chat/completions", map[string]any{
		"messages": messages, "stream": false,
	})
	if status != http.StatusOK {
		t.Fatal("chat status", status)
	}
	var chat map[string]any
	json.Unmarshal(chatBody, &chat)
	usage := chat["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(want) {
		t.Fatalf("prompt_tokens %v != tokenize count %d", usage["prompt_tokens"], want)
	}
	choices := chat["choices"].([]any)
	if choices[0].(map[string]any)["message"].(map[string]any)["content"] != fakeAnswer {
		t.Fatal("answer mismatch")
	}

	streamBody, status := postJSON(t, engine.Endpoint+"/v1/chat/completions", map[string]any{
		"messages": messages, "stream": true,
	})
	if status != http.StatusOK {
		t.Fatal("stream status", status)
	}
	text := string(streamBody)
	if !strings.Contains(text, `"finish_reason":"stop"`) || !strings.HasSuffix(strings.TrimSpace(text), "data: [DONE]") {
		t.Fatal("stream protocol incomplete", text)
	}
	if !strings.Contains(text, `"prompt_tokens"`) {
		t.Fatal("stream missing usage")
	}
}
