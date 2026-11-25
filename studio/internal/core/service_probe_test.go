package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServiceEndpointProbeSuccessAndErrors(t *testing.T) {
	t.Run("success_200", func(t *testing.T) {
		var gotPath string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		}))
		defer srv.Close()

		res := TestServiceEndpoint(context.Background(), srv.URL+"/v1/", "qwen3-8b")
		if !res.OK || res.StatusCode != 200 || res.Reply != "OK" || res.Error != "" {
			t.Fatalf("unexpected success probe result: %+v", res)
		}
		if gotPath != "/v1/chat/completions" {
			t.Fatalf("path=%q want /v1/chat/completions", gotPath)
		}
		if gotBody["model"] != "qwen3-8b" {
			t.Fatalf("model=%v want qwen3-8b", gotBody["model"])
		}
	})

	t.Run("reasoning_only_200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"Thinking: user asked for OK."}}]}`))
		}))
		defer srv.Close()

		res := TestServiceEndpoint(context.Background(), srv.URL+"/v1", "qwen3.8-flash-next-q2_0")
		if !res.OK || res.StatusCode != 200 || res.Reply != "Thinking: user asked for OK." {
			t.Fatalf("expected reasoning_content fallback reply, got %+v", res)
		}
	})

	t.Run("server_500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"model crashed"}`))
		}))
		defer srv.Close()

		res := TestServiceEndpoint(context.Background(), srv.URL+"/v1", "qwen3-8b")
		if res.OK || res.StatusCode != 500 || res.Error == "" {
			t.Fatalf("unexpected 500 probe result: %+v", res)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		res := TestServiceEndpoint(ctx, srv.URL+"/v1", "qwen3-8b")
		if res.OK || res.Error == "" {
			t.Fatalf("expected timeout failure, got %+v", res)
		}
	})

	t.Run("invalid_url", func(t *testing.T) {
		res := TestServiceEndpoint(context.Background(), "not-a-url", "m")
		if res.OK || res.Error == "" {
			t.Fatalf("expected invalid URL error, got %+v", res)
		}
	})
}
