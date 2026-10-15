package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// serveFakeEngine runs the prism_llama_server_v1 contract surface. Every
// endpoint is deterministic; prompt token counts are byte lengths so the
// /tokenize count and the completion usage always agree by construction.
func serveFakeEngine(paramsPath string) {
	raw, err := os.ReadFile(paramsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "params unreadable:", err)
		os.Exit(2)
	}
	var p fakeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		fmt.Fprintln(os.Stderr, "params invalid:", err)
		os.Exit(2)
	}
	template, err := os.ReadFile(p.TemplatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "template unreadable:", err)
		os.Exit(2)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": []map[string]any{
			{"id": p.DisplayName, "object": "model"},
		}})
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"build_info":                  fakeBuildInfo,
			"total_slots":                 1,
			"default_generation_settings": map[string]any{"n_ctx": p.NCtx},
			"model_path":                  p.ModelPath,
			"chat_template":               string(template),
		})
	})
	mux.HandleFunc("/slots", func(w http.ResponseWriter, _ *http.Request) {
		g := p.Generation
		writeJSON(w, []map[string]any{{
			"id": 0, "is_processing": false,
			"params": map[string]any{
				"seed":             g["seed"],
				"temperature":      g["temperature"],
				"top_k":            g["top_k"],
				"top_p":            g["top_p"],
				"min_p":            g["min_p"],
				"presence_penalty": g["presence_penalty"],
				"repeat_penalty":   g["repeat_penalty"],
				"max_tokens":       g["max_tokens"],
			},
		}})
	})
	mux.HandleFunc("/apply-template", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		readJSON(r, &body)
		writeJSON(w, map[string]any{"prompt": renderMessages(body["messages"])})
	})
	mux.HandleFunc("/tokenize", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		readJSON(r, &body)
		content, _ := body["content"].(string)
		tokens := make([]int, tokenCount(content))
		for i := range tokens {
			tokens[i] = i
		}
		writeJSON(w, map[string]any{"tokens": tokens})
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		readJSON(r, &body)
		usage := map[string]any{
			"prompt_tokens":     tokenCount(renderMessages(body["messages"])),
			"completion_tokens": tokenCount(fakeAnswer),
		}
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			writeSSE(w, flusher, map[string]any{"choices": []map[string]any{{
				"index": 0, "delta": map[string]any{"role": "assistant", "content": fakeAnswer},
			}}})
			writeSSE(w, flusher, map[string]any{
				"choices": []map[string]any{{
					"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
				}},
				"usage": usage,
			})
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		writeJSON(w, map[string]any{
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": fakeAnswer},
				"finish_reason": "stop",
			}},
			"usage": usage,
		})
	})

	host := argValue(os.Args[1:], "--host", "127.0.0.1")
	port := argValue(os.Args[1:], "--port", "8080")
	if err := http.ListenAndServe(host+":"+port, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func argValue(args []string, name, fallback string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return fallback
}

func renderMessages(raw any) string {
	messages, _ := raw.([]any)
	out := ""
	for _, m := range messages {
		if mm, ok := m.(map[string]any); ok {
			content, _ := mm["content"].(string)
			out += content + "\n"
		}
	}
	return out
}

func tokenCount(s string) int { return len([]byte(s)) }

func writeJSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher != nil {
		flusher.Flush()
	}
}

func readJSON(r *http.Request, v any) {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(data) == 0 {
		return
	}
	_ = json.Unmarshal(data, v)
}
