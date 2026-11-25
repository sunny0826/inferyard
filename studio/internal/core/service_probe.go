package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const serviceProbeTimeout = 15 * time.Second

// ServiceTestResult reports the outcome of a single minimal connectivity check
// against an OpenAI-compatible /chat/completions endpoint.
type ServiceTestResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	LatencyMs  int64  `json:"latency_ms"`
	Reply      string `json:"reply,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TestServiceEndpoint sends a single minimal chat completion request to
// strings.TrimRight(baseURL, "/") + "/chat/completions" to verify reachability.
func (s *Service) TestServiceEndpoint(ctx context.Context, baseURL, model string) ServiceTestResult {
	return TestServiceEndpoint(ctx, baseURL, model)
}

// TestServiceEndpoint verifies an OpenAI-compatible chat completions endpoint
// without persisting any conversation history.
func TestServiceEndpoint(ctx context.Context, baseURL, model string) ServiceTestResult {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return ServiceTestResult{OK: false, Error: "base URL is required"}
	}
	trimmed = localHealthEndpoint(trimmed)
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ServiceTestResult{OK: false, Error: "invalid base URL"}
	}
	target := trimmed + "/chat/completions"

	payload := map[string]any{
		"model": strings.TrimSpace(model),
		"messages": []map[string]string{
			{"role": "user", "content": "Reply OK"},
		},
		"max_tokens":  16,
		"temperature": 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ServiceTestResult{OK: false, Error: err.Error()}
	}

	reqCtx, cancel := context.WithTimeout(ctx, serviceProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return ServiceTestResult{OK: false, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: serviceProbeTimeout}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return ServiceTestResult{
			OK:        false,
			LatencyMs: latency,
			Error:     err.Error(),
		}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return ServiceTestResult{
			OK:         false,
			StatusCode: resp.StatusCode,
			LatencyMs:  latency,
			Error:      readErr.Error(),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 240 {
			msg = msg[:240]
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return ServiceTestResult{
			OK:         false,
			StatusCode: resp.StatusCode,
			LatencyMs:  latency,
			Error:      msg,
		}
	}

	var parsedResp struct {
		Choices []struct {
			Text    string `json:"text"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	reply := ""
	if json.Unmarshal(raw, &parsedResp) == nil && len(parsedResp.Choices) > 0 {
		reply = strings.TrimSpace(parsedResp.Choices[0].Message.Content)
		if reply == "" {
			reply = strings.TrimSpace(parsedResp.Choices[0].Text)
		}
	}
	return ServiceTestResult{
		OK:         true,
		StatusCode: resp.StatusCode,
		LatencyMs:  latency,
		Reply:      reply,
	}
}

func checkEngineHealth(client *http.Client, endpoint string, local bool, family string) bool {
	return checkEngineHealthContext(context.Background(), client, endpoint, local, family)
}

func checkEngineHealthContext(ctx context.Context, client *http.Client, endpoint string, local bool, family string) bool {
	endpoint = localHealthEndpoint(endpoint)
	if family == "polystrata" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return false
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if err != nil {
			return false
		}
		var payload struct {
			Service string `json:"service"`
			Loaded  *bool  `json:"loaded"`
		}
		return json.Unmarshal(raw, &payload) == nil && payload.Service == "strata" && payload.Loaded != nil && *payload.Loaded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+engineHealthPath(local), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func startingProbeInterval(elapsed time.Duration) time.Duration {
	switch {
	case elapsed < 5*time.Second:
		return 150 * time.Millisecond
	case elapsed < 15*time.Second:
		return 500 * time.Millisecond
	default:
		return time.Second
	}
}

func unloadPolystrata(endpoint string) bool {
	endpoint = localHealthEndpoint(endpoint)
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/unload", strings.NewReader("{}"))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func engineHealthPath(local bool) string {
	if local {
		return "/v1/models"
	}
	return "/health"
}
