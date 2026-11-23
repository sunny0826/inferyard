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
		return fmt.Sprintf("%.2f GiB", float64(v)/1073741824)
	case v >= 1048576 || v <= -1048576:
		return fmt.Sprintf("%.2f MiB", float64(v)/1048576)
	default:
		return fmt.Sprintf("%d bytes", v)
	}
}

func formatDurationMs(value *float64) string {
	if value == nil {
		return "缺测"
	}
	if *value < 1000 {
		return fmt.Sprintf("%.0f ms", *value)
	}
	return fmt.Sprintf("%.2f s", *value/1000)
}

func formatTimestamp(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.Local().Format("2006-01-02 15:04:05")
	}
	return raw
}

func formatTimestampUTC(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC().Format("2006-01-02T15:04:05Z")
	}
	return raw
}
