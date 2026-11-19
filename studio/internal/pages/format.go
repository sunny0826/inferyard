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

func formatDurationMs(value *float64) string {
	if value == nil {
		return "缺测"
	}
	return fmt.Sprintf("%.2f", *value)
}

func formatTimestamp(raw string) string {
	if raw == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UTC().Format("2006-01-02T15:04:05Z")
	}
	return raw
}
