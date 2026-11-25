package e2e

import (
	"net/http"
	"os"
	"strconv"
	"testing"
)

func TestMain(m *testing.M) {
	args := os.Args[1:]
	if os.Getenv("INFERYARD_E2E_STANDALONE_FAKE") == "1" {
		serveStandaloneFakeEngine(args)
		return
	}
	for i, a := range args {
		if a == "--params-file" && i+1 < len(args) {
			serveFakeEngine(args[i+1])
			return
		}
	}
	os.Exit(m.Run())
}

// serveStandaloneFakeEngine parses actual argv passed by `serve prepare` (-m, --alias, -c, --host, --port)
// and reflects them in /v1/models and /props so E2E verifies real CLI argument propagation.
func serveStandaloneFakeEngine(args []string) {
	host, port, modelPath, alias := "127.0.0.1", "", "", ""
	var nCtx int64
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			break
		}
		switch args[i] {
		case "--host":
			host = args[i+1]
			i++
		case "--port":
			port = args[i+1]
			i++
		case "-m", "--model":
			modelPath = args[i+1]
			i++
		case "--alias", "--served-model-name":
			alias = args[i+1]
			i++
		case "-c", "--ctx-size", "--context-length", "--max-model-len":
			nCtx, _ = strconv.ParseInt(args[i+1], 10, 64)
			i++
		}
	}
	if port == "" || modelPath == "" || alias == "" || nCtx <= 0 {
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"data": []map[string]any{
			{"id": alias, "object": "model"},
		}})
	})
	mux.HandleFunc("/props", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"model_path": modelPath,
			"default_generation_settings": map[string]any{
				"n_ctx": nCtx,
			},
		})
	})
	_ = http.ListenAndServe(host+":"+port, mux)
	os.Exit(1)
}
