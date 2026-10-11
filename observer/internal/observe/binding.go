package observe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// StrictJSON checks duplicate keys at every nesting level before decoding types.
func StrictJSON(data []byte, out any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid_utf8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 128 {
			return errors.New("json_nesting_limit")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		if number, ok := tok.(json.Number); ok {
			if _, err := number.Float64(); err != nil {
				return errors.New("non_finite_json_number")
			}
		}
		if delim, ok := tok.(json.Delim); ok {
			if delim != '{' && delim != '[' {
				return errors.New("unexpected_json_delimiter")
			}
			seen := map[string]bool{}
			for d.More() {
				if delim == '{' {
					key, err := d.Token()
					if err != nil {
						return err
					}
					name, ok := key.(string)
					if !ok || seen[name] {
						return errors.New("duplicate_json_key")
					}
					seen[name] = true
				}
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing_json_value")
	}
	return json.Unmarshal(data, out)
}

func boundedFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("binding_file_unreadable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("binding_not_regular_file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return nil, errors.New("binding_file_too_large")
	}
	return data, nil
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func ReadBinding(root string) (Binding, Target, error) {
	var b Binding
	var target Target
	rawRun, err := boundedFile(filepath.Join(root, "run.json"))
	if err != nil {
		return b, target, err
	}
	rawConfig, err := boundedFile(filepath.Join(root, "config.frozen.json"))
	if err != nil {
		return b, target, err
	}
	var run struct {
		Schema int    `json:"schema_version"`
		RunID  string `json:"run_id"`
	}
	var config struct {
		Schema   int `json:"schema_version"`
		Endpoint struct {
			PID   int    `json:"server_pid"`
			Start uint64 `json:"process_start_ticks"`
		} `json:"endpoint"`
		Model struct {
			Name string `json:"display_name"`
		} `json:"model"`
		Engine struct {
			Backend string `json:"backend"`
		} `json:"engine"`
	}
	if StrictJSON(rawRun, &run) != nil || StrictJSON(rawConfig, &config) != nil || run.Schema != 3 ||
		config.Schema != 3 || run.RunID == "" || config.Endpoint.PID <= 0 ||
		config.Endpoint.PID > 2_147_483_647 || config.Endpoint.Start == 0 {
		return b, target, errors.New("benchmark_binding_invalid")
	}
	b = Binding{run.RunID, digest(rawRun), digest(rawConfig), config.Model.Name, config.Engine.Backend}
	target = Target{PID: config.Endpoint.PID, StartTicks: config.Endpoint.Start}
	return b, target, nil
}
