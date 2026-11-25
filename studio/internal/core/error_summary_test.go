package core

import (
	"context"
	"inferyard/studio/internal/inferyard"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestSanitizeErrorDetailRedactsAndTruncatesUTF8(t *testing.T) {
	raw := "\x1b[31mERROR\x1b[0m \x1b]8;;https://example.com\x07link\x1b]8;;\x07 \u009b31mred\u009b0m \x9b32mrawbyte\x9b0m: auth failed Bearer tok_secret_123 sk-abc12345 hf_xyz98765 " +
		"--api-key mykey --password \"alpha\\\"beta gamma\" --token=tok2 https://tokenonly@example.com/v1 https://user:pass@example.com/v1 API_TOKEN=\"partA\\\"partB partC\" SECRET_KEY=\"unclosed tail\x00\x07"
	got := sanitizeErrorDetail(raw)
	for _, leaked := range []string{"tok_secret_123", "sk-abc12345", "hf_xyz98765", "mykey", "alpha", "beta", "gamma", "tok2", "tokenonly", "user:pass", "partA", "partB", "partC", "unclosed", "tail", "31m", "32m", "\x1b", "\u009b", "\x9b", "\x00", "\x07", "https://example.com"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("expected %q redacted/stripped, got %q", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] placeholder, got %q", got)
	}

	// Multiline PEM private key in env assignment must be fully redacted across lines.
	pemRaw := "APP_SECRET=\"-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcw\nSECOND_SECRET_LINE\n-----END PRIVATE KEY-----\"\nfatal: bad key"
	pemClean := sanitizeErrorDetail(pemRaw)
	for _, leaked := range []string{"MIIEvQIBADAN", "SECOND_SECRET_LINE"} {
		if strings.Contains(pemClean, leaked) {
			t.Fatalf("expected multiline PEM body %q redacted, got %q", leaked, pemClean)
		}
	}

	// UTF-8 characters containing 0x9D/0x9B continuation bytes (e.g., 保 = E4 BF 9D) must remain intact.
	zhMsg := sanitizeErrorDetail("保存失败: disk full \x9b32mrawbyte\x9b0m")
	if zhMsg != "保存失败: disk full rawbyte" {
		t.Fatalf("UTF-8 continuation bytes corrupted: got %q", zhMsg)
	}

	// Truncated PEM tail without BEGIN header and backslash-newline env secret must both be redacted.
	pemTail := sanitizeErrorDetail("TRUNCATED_KEY_BODY_LINE\n-----END PRIVATE KEY-----\nexit 1")
	if strings.Contains(pemTail, "TRUNCATED_KEY_BODY_LINE") || !strings.Contains(pemTail, "exit 1") {
		t.Fatalf("expected truncated PEM tail redacted while keeping trailing error, got %q", pemTail)
	}
	contEnv := sanitizeErrorDetail("API_TOKEN=\"line1\\\nline2_secret\"\nfatal: auth")
	if strings.Contains(contEnv, "line2_secret") || !strings.Contains(contEnv, "fatal: auth") {
		t.Fatalf("expected backslash-newline env secret redacted, got %q", contEnv)
	}

	// Multibyte UTF-8 truncation must stay <= 512 bytes and valid UTF-8.
	longUTF8 := strings.Repeat("模型启动失败日志", 100)
	truncated := sanitizeErrorDetail(longUTF8)
	if len(truncated) > maxErrorDetailBytes {
		t.Fatalf("len=%d exceeds max %d", len(truncated), maxErrorDetailBytes)
	}
	if !utf8.ValidString(truncated) {
		t.Fatalf("truncated string is not valid UTF-8: %q", truncated)
	}
	if !strings.HasSuffix(truncated, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", truncated)
	}

	// Long first line in stderr must NOT crowd out the final error line.
	longFirst := strings.Repeat("noise ", 120) + "\nfinal fatal: model file corrupted\n"
	summary := summarizeStderr([]byte(longFirst))
	if len(summary) > maxErrorDetailBytes {
		t.Fatalf("stderr summary len=%d exceeds %d", len(summary), maxErrorDetailBytes)
	}
	if !strings.Contains(summary, "final fatal: model file corrupted") {
		t.Fatalf("expected final error line preserved in stderr summary, got %q", summary)
	}
}

func TestStartStandaloneServePrepareStderrInErrorDetails(t *testing.T) {
	var prepareCalls, totalCalls atomic.Int32
	s, _, model, engine := setupStandaloneTestService(t, true, &prepareCalls, &totalCalls)
	origExec := s.exec
	s.exec = func(ctx context.Context, path string, args ...string) (inferyard.Execution, error) {
		if len(args) >= 2 && args[0] == "serve" && args[1] == "prepare" {
			return inferyard.Execution{
				ExitCode: 1,
				Stderr:   []byte("\x1b[31mTraceback line 1\x1b[0m\nmiddle noise\nmore noise\nmiddle 3\nValueError: unsupported gguf quant with Bearer secret123\n"),
			}, nil
		}
		return origExec(ctx, path, args...)
	}

	_, apiErr := s.StartStandalone(context.Background(), StandaloneStartRequest{
		Model: model, Engine: engine, ContextSize: 8192,
	})
	if apiErr == nil || apiErr.Code != "cli_error" {
		t.Fatalf("expected cli_error, got %+v", apiErr)
	}
	detail, _ := apiErr.Details["detail"].(string)
	if !strings.Contains(detail, "ValueError: unsupported gguf quant") {
		t.Fatalf("expected stderr summary in Details[detail], got %+v", apiErr.Details)
	}
	if strings.Contains(detail, "secret123") || strings.Contains(detail, "\x1b") {
		t.Fatalf("expected redacted/cleaned detail, got %q", detail)
	}
}

func TestStartManagedServiceCmdStartErrorIncludesDetail(t *testing.T) {
	s, workspace, _ := setupMultiServiceTestEnv(t, map[string]int64{"m.gguf": 4}, nil)
	badExe := filepath.Join(t.TempDir(), "not-exec")
	if err := os.WriteFile(badExe, []byte("not a valid binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	startup := Startup{
		Kind:             "direct_process",
		Path:             badExe,
		Args:             []string{"--host", "127.0.0.1", "--port", strconv.Itoa(port)},
		WorkingDirectory: filepath.Dir(badExe),
		Endpoint:         "http://127.0.0.1:" + strconv.Itoa(port),
	}
	_, apiErr := s.startManagedServiceLocked(startup, "slug-bad-exe", "svc-bad-exe", port, workspace)
	if apiErr == nil || apiErr.Code != "engine_start_failed" {
		t.Fatalf("expected engine_start_failed, got %+v", apiErr)
	}
	detail, _ := apiErr.Details["detail"].(string)
	if detail == "" {
		t.Fatalf("expected non-empty Details[detail] on cmd.Start failure, got %+v", apiErr.Details)
	}
}

func TestMonitorEngineStartingCrashIncludesSanitizedLogTail(t *testing.T) {
	t.Setenv("ENGINE_HELPER", "1")
	t.Setenv("ENGINE_HELPER_EXIT", "1")
	t.Setenv("ENGINE_HELPER_CRASH_LOG", "line 1\nline 2\nfatal: cuda OOM with sk-secret9999\n"+strings.Repeat("\x1b[0m\n", 25))
	port := freePort(t)
	workspace, candidate := engineWorkspace(t, port)
	s := engineService(t, workspace)

	if _, apiErr := s.StartEngine(candidate); apiErr != nil {
		t.Fatalf("StartEngine failed: %+v", apiErr)
	}
	st := pollEngine(t, s, "failed")
	if st.Error == nil || st.Error.Code != "engine_exited" {
		t.Fatalf("expected engine_exited error, got %+v", st.Error)
	}
	tail, _ := st.Error.Details["log_tail"].(string)
	if !strings.Contains(tail, "fatal: cuda OOM") {
		t.Fatalf("expected log_tail in EngineState.Error.Details even after 25 ANSI-only trailing lines, got %+v", st.Error.Details)
	}
	if strings.Contains(tail, "sk-secret9999") {
		t.Fatalf("expected secret redacted in log_tail, got %q", tail)
	}
	noticeTail, _ := readExitNoticeLogAndHint(st.LogPath, 5)
	if joined := strings.Join(noticeTail, "\n"); !strings.Contains(joined, "fatal: cuda OOM") || strings.Contains(joined, "sk-secret9999") {
		t.Fatalf("expected ServiceExitNotice readExitNoticeLogAndHint sanitized & redacted, got %q", joined)
	}

	// Legacy persisted exit notices with raw credentials & oversized 4 KiB line are sanitized & bounded <= 512B on load.
	legacyJSON := `[{"id":"old-1","model":"m","engine":"e","log_tail":["` + strings.Repeat("x", 4096) + ` Bearer old_live_token"]}]`
	parsed := parseExitNoticesBytes([]byte(legacyJSON))
	if len(parsed) != 1 || len(parsed[0].LogTail) == 0 {
		t.Fatalf("expected 1 sanitized legacy notice, got %+v", parsed)
	}
	joinedLegacy := strings.Join(parsed[0].LogTail, "\n")
	if len(joinedLegacy) > maxErrorDetailBytes || strings.Contains(joinedLegacy, "old_live_token") {
		t.Fatalf("expected legacy LogTail <= %dB and redacted, len=%d val=%q", maxErrorDetailBytes, len(joinedLegacy), joinedLegacy)
	}

	// OOM hint detection must run before 512B truncation so trailing stack traces do not hide OOM.
	oomLog := filepath.Join(t.TempDir(), "oom.log")
	oomContent := "CUDA out of memory while allocating kv cache\n" + strings.Repeat("at frame_trace_line_padding_0123456789_abcdefghijklmnop\n", 10)
	if err := os.WriteFile(oomLog, []byte(oomContent), 0o600); err != nil {
		t.Fatal(err)
	}
	oomTail, oomHint := readExitNoticeLogAndHint(oomLog, exitNoticeLogTailLines)
	if oomHint != ExitHintOOM {
		t.Fatalf("expected ExitHintOOM before 512B truncation, got %q", oomHint)
	}
	if len(strings.Join(oomTail, "\n")) > maxErrorDetailBytes {
		t.Fatalf("expected bounded LogTail <= %dB, got %d", maxErrorDetailBytes, len(strings.Join(oomTail, "\n")))
	}
}
