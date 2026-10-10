package observe

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// memberBytes mirrors Python json_bytes: sorted UTF-8 keys, compact JSON and LF.
func memberBytes(raw json.RawMessage) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := canonicalValue(&out, value); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func canonicalString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte("0123456789abcdef"[r>>4])
				out.WriteByte("0123456789abcdef"[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func canonicalNumber(value json.Number) (string, error) {
	raw := value.String()
	if !strings.ContainsAny(raw, ".eE") {
		integer, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return "", errors.New("invalid_integer")
		}
		return integer.String(), nil
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return "", errors.New("non_finite_json_number")
	}
	scientific := strconv.FormatFloat(number, 'e', -1, 64)
	exponent, _ := strconv.Atoi(scientific[strings.LastIndex(scientific, "e")+1:])
	if exponent >= -4 && exponent < 16 {
		fixed := strconv.FormatFloat(number, 'f', -1, 64)
		if !strings.Contains(fixed, ".") {
			fixed += ".0"
		}
		return fixed, nil
	}
	return scientific, nil
}

func canonicalValue(out *bytes.Buffer, value any) error {
	switch item := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(item))
	case string:
		canonicalString(out, item)
	case json.Number:
		number, err := canonicalNumber(item)
		if err != nil {
			return err
		}
		out.WriteString(number)
	case []any:
		out.WriteByte('[')
		for i, child := range item {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := canonicalValue(out, child); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			canonicalString(out, key)
			out.WriteByte(':')
			if err := canonicalValue(out, item[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return errors.New("invalid_json_value")
	}
	return nil
}
