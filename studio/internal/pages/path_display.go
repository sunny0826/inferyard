package pages

import (
	"path"
	"path/filepath"
	"strings"
)

func displayName(value string) string {
	if value == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(value, "\\", "/"))
}

func displayValue(value string) string {
	if filepath.IsAbs(value) || strings.HasPrefix(value, "\\\\") ||
		len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return displayName(value)
	}
	return value
}
