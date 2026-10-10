package observe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"unicode/utf8"
)

// StrictJSON checks duplicate keys at every nesting level before decoding types.
func StrictJSON(data []byte, out any) error {
	if !utf8.Valid(data) || !validSurrogates(data) {
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

// encoding/json replaces unpaired UTF-16 escapes; evidence must reject them.
func validSurrogates(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		i++
		for i < len(data) && data[i] != '"' {
			if data[i] != '\\' {
				i++
				continue
			}
			i++
			if i >= len(data) {
				return false
			}
			if data[i] != 'u' {
				i++
				continue
			}
			if i+4 >= len(data) {
				return false
			}
			code, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if err != nil {
				return false
			}
			i += 5
			if code >= 0xDC00 && code <= 0xDFFF {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				if i+5 >= len(data) || data[i] != '\\' || data[i+1] != 'u' {
					return false
				}
				low, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				i += 6
			}
		}
	}
	return true
}
