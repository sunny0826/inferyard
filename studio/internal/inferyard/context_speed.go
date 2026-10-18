package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

var errSpeedResult = errors.New("invalid context speed result")
var speedHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var speedID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

// ParseContextSpeed rejects malformed projections before any rows are displayed.
// A readable partial batch is data (exit 3); command cancellation is not a batch.
func ParseContextSpeed(raw []byte, exitCode int) (ContextSpeedResult, error) {
	fail := func() (ContextSpeedResult, error) { return ContextSpeedResult{}, errSpeedResult }
	if !utf8.Valid(raw) {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSON(d) != nil {
		return fail()
	}
	if _, err := d.Token(); err != io.EOF {
		return fail()
	}
	if !discoveryFields(raw, "command", "status", "completeness", "run_id", "evidence_dir", "limitations", "details") {
		return fail()
	}
	r, err := Parse(raw)
	if err != nil || r.Command != "speed-results" || r.RunID != nil || r.Limitations == nil {
		return fail()
	}
	var header struct {
		ReadState string `json:"read_state"`
	}
	if json.Unmarshal(r.Details, &header) != nil {
		return fail()
	}
	if header.ReadState == "error" {
		var wire struct {
			SchemaVersion int            `json:"schema_version"`
			Kind          string         `json:"kind"`
			ReadState     string         `json:"read_state"`
			Error         SpeedReadError `json:"error"`
		}
		if !speedShape(r.Details, reflect.TypeOf(wire)) || json.Unmarshal(r.Details, &wire) != nil || wire.SchemaVersion != 3 || wire.Kind != "context_speed_result.v1" ||
			(exitCode != 2 && exitCode != 4) || r.Status != "error" || r.Completeness != "incomplete" {
			return fail()
		}
		return ContextSpeedResult{SchemaVersion: 3, Kind: wire.Kind, ReadState: "error", ReadError: &wire.Error}, nil
	}
	var value ContextSpeedResult
	if !speedShape(r.Details, reflect.TypeOf(value)) || json.Unmarshal(r.Details, &value) != nil || r.EvidenceDir == nil || *r.EvidenceDir == "" {
		return fail()
	}
	if value.SchemaVersion != 3 || value.Kind != "context_speed_result.v1" || value.ReadState != "readable" || value.Profile != "context-speed.v1" || !validSpeedResult(value) {
		return fail()
	}
	switch exitCode {
	case 0:
		if r.Status != "finished" || r.Completeness != "complete" || value.Execution.StopReason.Value == nil || *value.Execution.StopReason.Value != "plan_finished" {
			return fail()
		}
		for _, s := range value.Samples {
			if (s.ExecutionState != "completed" && s.ExecutionState != "failed") || s.Run == nil || s.Run.Integrity != "sealed" {
				return fail()
			}
		}
	case 3:
		if r.Status != "partial" || r.Completeness != "incomplete" {
			return fail()
		}
	default:
		return fail()
	}
	return value, nil
}

// Requiredness is checked before decoding: absent fields must never become zero values.
func speedShape(raw json.RawMessage, typ reflect.Type) bool {
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return len(raw) != 0
	}
	if typ.Kind() == reflect.Pointer {
		if bytes.Equal(raw, []byte("null")) {
			return true
		}
		return speedShape(raw, typ.Elem())
	}
	if bytes.Equal(raw, []byte("null")) {
		return false
	}
	switch typ.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return false
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name, optional, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			v, ok := obj[name]
			if !ok && optional == "omitempty" {
				continue
			}
			if !ok || !speedShape(v, f.Type) {
				return false
			}
			delete(obj, name)
		}
		return len(obj) == 0
	case reflect.Slice:
		var rows []json.RawMessage
		if json.Unmarshal(raw, &rows) != nil || rows == nil {
			return false
		}
		for _, row := range rows {
			if !speedShape(row, typ.Elem()) {
				return false
			}
		}
		return true
	case reflect.String:
		var v string
		return json.Unmarshal(raw, &v) == nil && v != ""
	default:
		dst := reflect.New(typ)
		if json.Unmarshal(raw, dst.Interface()) != nil {
			return false
		}
		v := dst.Elem()
		if v.Kind() == reflect.Int || v.Kind() == reflect.Int64 {
			return v.Int() >= 0
		}
		if v.Kind() == reflect.Float64 {
			return v.Float() >= 0
		}
		return true
	}
}

func speedPath(path string) bool {
	if path == "" || strings.ContainsAny(path, `\:`) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func validSpeedRef(r SpeedRef) bool {
	if !speedPath(r.Path) || !speedHash.MatchString(r.SHA256) {
		return false
	}
	if len(r.Member) == 0 {
		return true
	}
	var m struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Seq       int64  `json:"seq"`
		RequestID string `json:"request_id"`
		SHA256    string `json:"sha256"`
	}
	if json.Unmarshal(r.Member, &m) != nil || !speedHash.MatchString(m.SHA256) {
		return false
	}
	switch m.Kind {
	case "input":
		return discoveryFields(r.Member, "kind", "name", "sha256") && containsSpeed([]string{"run", "plan", "selection", "config", "bundle"}, m.Name)
	case "observation":
		return discoveryFields(r.Member, "kind", "seq", "sha256") && m.Seq > 0
	case "request":
		return discoveryFields(r.Member, "kind", "request_id", "sha256") && speedID.MatchString(m.RequestID)
	}
	return false
}

func containsSpeed(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func validSpeedValue[T any](v SpeedValue[T]) bool {
	if (v.Value == nil) != (v.MissingReason != nil) || (v.Value != nil && len(v.EvidenceRefs) == 0) {
		return false
	}
	for _, ref := range v.EvidenceRefs {
		if !validSpeedRef(ref) {
			return false
		}
	}
	return true
}
