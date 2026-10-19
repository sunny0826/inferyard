package inferyard

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
)

type speedSchema struct {
	Type       string                 `json:"type"`
	Const      json.RawMessage        `json:"const"`
	Enum       []json.RawMessage      `json:"enum"`
	Properties map[string]speedSchema `json:"properties"`
	Required   []string               `json:"required"`
	Additional *bool                  `json:"additionalProperties"`
	Items      *speedSchema           `json:"items"`
	AnyOf      []speedSchema          `json:"anyOf"`
	OneOf      []speedSchema          `json:"oneOf"`
	MinItems   int                    `json:"minItems"`
	MaxItems   int                    `json:"maxItems"`
}

// SupportsContextSpeedSchema checks the advertised wire shape, not the CLI version.
// Each operation queries again, so a replaced executable cannot inherit support.
func SupportsContextSpeedSchema(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	var root speedSchema
	if json.Unmarshal(raw, &root) != nil || len(root.OneOf) != 2 {
		return false
	}
	readable, failure := false, false
	for _, branch := range root.OneOf {
		if !schemaLiteral(branch.Properties["schema_version"], "3") || !schemaLiteral(branch.Properties["kind"], `"context_speed_result.v1"`) {
			return false
		}
		switch {
		case schemaLiteral(branch.Properties["read_state"], `"readable"`):
			samples := branch.Properties["samples"]
			readable = schemaShape(branch, reflect.TypeOf(ContextSpeedResult{})) && schemaLiteral(branch.Properties["profile"], `"context-speed.v1"`) && samples.MinItems == 5 && samples.MaxItems == 5
		case schemaLiteral(branch.Properties["read_state"], `"error"`):
			failure = schemaShape(branch, reflect.TypeOf(struct {
				Version int            `json:"schema_version"`
				Kind    string         `json:"kind"`
				State   string         `json:"read_state"`
				Error   SpeedReadError `json:"error"`
			}{}))
		default:
			return false
		}
	}
	return readable && failure
}

func schemaLiteral(s speedSchema, value string) bool {
	return string(s.Const) == value || len(s.Enum) == 1 && string(s.Enum[0]) == value
}

func schemaShape(s speedSchema, typ reflect.Type) bool {
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return len(s.OneOf) > 0
	}
	if typ.Kind() == reflect.Pointer {
		if len(s.AnyOf) != 2 {
			return false
		}
		for i, branch := range s.AnyOf {
			if branch.Type == "null" {
				return schemaShape(s.AnyOf[1-i], typ.Elem())
			}
		}
		return false
	}
	switch typ.Kind() {
	case reflect.Struct:
		if s.Type != "object" || s.Additional == nil || *s.Additional {
			return false
		}
		fields, required := 0, 0
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, optional, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			fields++
			child, ok := s.Properties[name]
			if !ok || !schemaShape(child, f.Type) {
				return false
			}
			if optional != "omitempty" {
				required++
				if !slices.Contains(s.Required, name) {
					return false
				}
			}
		}
		return len(s.Properties) == fields && len(s.Required) == required
	case reflect.Slice:
		return s.Type == "array" && s.Items != nil && schemaShape(*s.Items, typ.Elem())
	case reflect.String:
		return s.Type == "string"
	case reflect.Bool:
		return s.Type == "boolean"
	case reflect.Int, reflect.Int64:
		return s.Type == "integer"
	case reflect.Float64:
		return s.Type == "number"
	}
	return false
}
