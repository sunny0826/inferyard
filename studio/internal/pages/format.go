package pages

import (
	"fmt"
	"time"
)

func formatBytes(value *int64) string {
	if value == nil {
		return "缺测"
	}
	v := *value
	switch {
	case v >= 1073741824 || v <= -1073741824:
		return fmt.Sprintf("%.2f GiB（%d bytes）", float64(v)/1073741824, v)
	case v >= 1048576 || v <= -1048576:
		return fmt.Sprintf("%.2f MiB（%d bytes）", float64(v)/1048576, v)
	default:
		return fmt.Sprintf("%d bytes", v)
	}
}

// formatDurationMs keeps short cases in integer milliseconds and switches to
// seconds from one second on, so a case table never shows a bare "1240.00".
// A missing measurement stays "缺测"; it is not the same as "不适用" ("—").
func formatDurationMs(value *float64) string {
	if value == nil {
		return "缺测"
	}
	if *value < 1000 {
		return fmt.Sprintf("%.0f ms", *value)
	}
	return fmt.Sprintf("%.2f s", *value/1000)
}

// formatTimestamp renders a recorded UTC timestamp in the local timezone for
// primary UI text. Raw evidence tables keep formatTimestampUTC.
func formatTimestamp(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	return raw
}

// formatTimestampUTC keeps the UTC instant and its trailing Z for raw evidence
// tables, where the time basis must stay explicit.
func formatTimestampUTC(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC().Format("2006-01-02T15:04:05Z")
	}
	return raw
}
