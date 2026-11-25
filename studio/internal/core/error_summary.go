package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxErrorDetailBytes = 512

var (
	oscEscapeRe         = regexp.MustCompile("(?:\x1b\\]|\u009d)[^\x07\x1b\u009c\u009d]*(?:\x07|\x1b\\\\|\u009c)")
	ansiEscapeRe        = regexp.MustCompile("(?:\x1b\\[|\u009b)[0-9;?]*[ -/]*[@-~]")
	pemPrivateKeyRe     = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)
	pemPrivateKeyTailRe = regexp.MustCompile(`(?s)\A.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	bearerSecretRe      = regexp.MustCompile(`(?i)\b(Bearer\s+)\S+`)
	tokenPrefixRe       = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{4,}|hf_[A-Za-z0-9_-]{4,})\b`)
	cliFlagSecretRe     = regexp.MustCompile(`(?i)(--(?:api-key|api-key-file|token|hf-token|password)|-hft)(\s+|=)(?:"(?:\\.|[^"\\\n])*"?|'(?:\\.|[^'\\\n])*'?|\S+)`)
	urlUserInfoRe       = regexp.MustCompile(`(https?://)([^/\s@]+@)`)
	envSecretAssignRe   = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:API_KEY|TOKEN|SECRET|PASSWORD)[A-Z0-9_]*)\s*=\s*(?:"(?:\\[\s\S]|[^"\\])*"?|'(?:\\[\s\S]|[^'\\])*'?|\S+)`)
)

func stripRawC1Sequences(s string) string {
	if strings.IndexByte(s, 0x9b) < 0 && strings.IndexByte(s, 0x9d) < 0 {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || size != 1 {
			out.WriteString(s[i : i+size])
			i += size
			continue
		}
		if s[i] == 0x9b {
			j := i + 1
			for j < len(s) && s[j] >= 0x30 && s[j] <= 0x3f {
				j++
			}
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
				j++
			}
			if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7e {
				i = j + 1
				continue
			}
			i++
			continue
		}
		if s[i] == 0x9d {
			j := i + 1
			for j < len(s) {
				if s[j] == 0x07 || s[j] == 0x9c {
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func stripControlChars(s string) string {
	s = stripRawC1Sequences(s)
	s = oscEscapeRe.ReplaceAllString(s, "")
	s = ansiEscapeRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func redactSensitiveText(s string) string {
	s = pemPrivateKeyRe.ReplaceAllString(s, "[REDACTED]")
	s = pemPrivateKeyTailRe.ReplaceAllString(s, "[REDACTED]")
	s = bearerSecretRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = cliFlagSecretRe.ReplaceAllString(s, "${1}${2}[REDACTED]")
	s = urlUserInfoRe.ReplaceAllString(s, "${1}[REDACTED]@")
	s = envSecretAssignRe.ReplaceAllString(s, "${1}=[REDACTED]")
	s = tokenPrefixRe.ReplaceAllString(s, "[REDACTED]")
	return s
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const ellipsis = "..."
	if maxBytes <= len(ellipsis) {
		return s[:maxBytes]
	}
	cut := maxBytes - len(ellipsis)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " \t\n") + ellipsis
}

func truncateUTF8TailBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const ellipsis = "..."
	if maxBytes <= len(ellipsis) {
		return s[len(s)-maxBytes:]
	}
	start := len(s) - (maxBytes - len(ellipsis))
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return ellipsis + strings.TrimLeft(s[start:], " \t\n")
}

func sanitizeErrorDetail(raw string) string {
	cleaned := strings.TrimSpace(stripControlChars(raw))
	if cleaned == "" {
		return ""
	}
	redacted := strings.TrimSpace(redactSensitiveText(cleaned))
	return truncateUTF8Bytes(redacted, maxErrorDetailBytes)
}

func extractCleanTailLines(raw string, maxLines int) []string {
	if maxLines <= 0 {
		return nil
	}
	cleaned := redactSensitiveText(stripControlChars(raw))
	rawLines := strings.Split(cleaned, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func summarizeLogTailLines(raw string, maxLines int) []string {
	lines := extractCleanTailLines(raw, maxLines)
	if len(lines) == 0 {
		return nil
	}
	bounded := truncateUTF8TailBytes(strings.Join(lines, "\n"), maxErrorDetailBytes)
	out := make([]string, 0, len(lines))
	for _, line := range strings.Split(bounded, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func summarizeLogTail(raw string, maxLines int) string {
	return strings.Join(summarizeLogTailLines(raw, maxLines), "\n")
}

func summarizeStderr(stderr []byte) string {
	cleaned := redactSensitiveText(stripControlChars(string(stderr)))
	rawLines := strings.Split(cleaned, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		return truncateUTF8TailBytes(lines[0], maxErrorDetailBytes)
	}
	var joined string
	if len(lines) <= 4 {
		joined = strings.Join(lines, "\n")
	} else {
		joined = strings.Join([]string{lines[0], lines[1], "...", lines[len(lines)-2], lines[len(lines)-1]}, "\n")
	}
	if len(joined) <= maxErrorDetailBytes {
		return joined
	}
	tailBudget := (maxErrorDetailBytes * 3) / 5
	tailPart := truncateUTF8TailBytes(lines[len(lines)-1], tailBudget)
	const sep = "\n...\n"
	headBudget := maxErrorDetailBytes - len(tailPart) - len(sep)
	headPart := truncateUTF8Bytes(lines[0], headBudget)
	return headPart + sep + tailPart
}

func apiErrorWithDetail(code, detail string) *ApiError {
	e := Err(code)
	if cleaned := sanitizeErrorDetail(detail); cleaned != "" {
		e.Details = map[string]any{"detail": cleaned}
	}
	return e
}
