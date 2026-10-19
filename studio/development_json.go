//go:build studio_dev && !release

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
)

func readDevJSON(path string, value any) error {
	if err := canonicalDevPath(path); err != nil {
		return err
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("JSON path is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid JSON field")
		}
		if _, exists := fields[name]; exists {
			return fmt.Errorf("duplicate JSON field: %s", name)
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(raw, []byte("null")) {
			return fmt.Errorf("null JSON field: %s", name)
		}
		fields[name] = raw
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	t := reflect.TypeOf(value).Elem()
	for i := 0; i < t.NumField(); i++ {
		name := t.Field(i).Tag.Get("json")
		if name != "" {
			if _, ok := fields[name]; !ok {
				return fmt.Errorf("missing JSON field: %s", name)
			}
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	return strict.Decode(value)
}

func atomicDevJSON(path string, value any) error {
	if err := canonicalDevPath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".studio-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
