package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

func kvview(c *ui.Context, key string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	var data any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&data) != nil {
		return
	}
	c.Key(key)
	container := ui.Column(c)
	open := ui.Local(container, "expanded", func() map[string]*bool { return map[string]*bool{} })
	container.Children(func() {
		if values, ok := data.(map[string]any); ok && stringMap(values) {
			ui.Form(c, func() {
				for _, key := range kvKeys(values) {
					ui.Field(c.Key(key), key, func() { ui.Text(c, kvValue(key, values[key])).Selectable() })
				}
			})
			return
		}
		ui.Tree(c, func() { kvChildren(c, data, "", *open) })
	})
}

func stringMap(values map[string]any) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if _, ok := value.(string); !ok {
			return false
		}
	}
	return true
}

func kvKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func kvChildren(c *ui.Context, data any, path string, open map[string]*bool) {
	switch value := data.(type) {
	case map[string]any:
		if len(value) == 0 {
			ui.Text(c, "无").Selectable()
		}
		for _, key := range kvKeys(value) {
			kvNode(c, key, value[key], path, open)
		}
	case []any:
		if len(value) == 0 {
			ui.Text(c, "无").Selectable()
		}
		for i, item := range value {
			kvNode(c, fmt.Sprintf("[%d]", i), item, path, open)
		}
	default:
		ui.Text(c, kvValue("", value)).Selectable()
	}
}

func kvNode(c *ui.Context, label string, value any, parent string, open map[string]*bool) {
	encoded, _ := json.Marshal(label)
	path := parent + "/" + string(encoded)
	switch value.(type) {
	case map[string]any, []any:
		if open[path] == nil {
			expanded := true
			open[path] = &expanded
		}
		ui.TreeItem(c.Key(path), label, open[path], func() { kvChildren(c, value, path, open) })
	default:
		ui.TreeItem(c.Key(path), label, nil, nil).Children(func() {
			ui.Text(c, kvValue(label, value)).Selectable()
		})
	}
}

func kvValue(key string, value any) string {
	if value == nil {
		return "缺测"
	}
	if strings.HasSuffix(key, "_bytes") || key == "bytes" {
		if n, ok := value.(json.Number); ok {
			if size, err := n.Int64(); err == nil {
				return formatBytes(&size)
			}
		}
	}
	return fmt.Sprint(value)
}
