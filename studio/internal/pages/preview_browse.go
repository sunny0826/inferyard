//go:build ui_debug && !release

package pages

import (
	"path/filepath"
)

func browseChoice(root, key string, current string) string {
	if current != "" {
		return filepath.Join(filepath.Dir(current), "selected", filepath.Base(current))
	}
	switch key {
	case "cli":
		return filepath.Join(root, "tools", "bin", "inferyard")
	case "installation":
		return filepath.Join(root, "engines", "installation.json")
	case "workspace":
		return filepath.Join(root, "workspace")
	case "results":
		return filepath.Join(root, "results")
	case "models":
		return filepath.Join(root, "models")
	}
	return filepath.Join(root, key)
}
