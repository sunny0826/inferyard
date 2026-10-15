package pages

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
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

func privatePathField(c *ui.Context, label string, value *string) ui.Element {
	ui.Text(c, "当前名称："+configuredValue(displayName(*value)))
	return ui.Field(c, label, func() {
		components.TextBox(c, value, components.TextBoxOptions{}).Password()
	})
}
