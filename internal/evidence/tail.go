package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

var ErrTruncated = errors.New("evidence stream truncated; restart with an empty cursor")

// Cursor commits only complete validated lines. Persist both fields to resume.
// It belongs to one append-only file and must not be shared between streams.
type Cursor struct {
	Offset int64
	Seq    int64
}
type Record struct {
	Seq       int64           `json:"seq"`
	Phase     string          `json:"phase"`
	EventType string          `json:"event_type,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// Tail reads events.jsonl or memory.jsonl. A half-written final line is left for
// the next call. On error, returned records and cursor cover the valid prefix.
func Tail(path string, cursor Cursor) ([]Record, Cursor, error) {
	if cursor.Offset < 0 || cursor.Seq < 0 {
		return nil, cursor, errors.New("negative cursor")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, cursor, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, cursor, err
	}
	if info.Size() < cursor.Offset {
		return nil, cursor, ErrTruncated
	}
	if cursor.Offset > 0 {
		var last [1]byte
		if _, err = f.ReadAt(last[:], cursor.Offset-1); err != nil {
			return nil, cursor, err
		}
		if last[0] != '\n' {
			return nil, cursor, errors.New("cursor is not at a line boundary")
		}
	}
	if _, err = f.Seek(cursor.Offset, io.SeekStart); err != nil {
		return nil, cursor, err
	}
	reader := bufio.NewReader(f)
	var records []Record
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			return records, cursor, nil
		}
		if err != nil {
			return records, cursor, err
		}
		var record Record
		trimmed := bytes.TrimSpace(line)
		if !utf8.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
			return records, cursor, fmt.Errorf("invalid JSONL at offset %d", cursor.Offset)
		}
		if err = json.Unmarshal(trimmed, &record); err != nil {
			return records, cursor, fmt.Errorf("offset %d: %w", cursor.Offset, err)
		}
		if record.Seq <= cursor.Seq {
			return records, cursor, fmt.Errorf("non-increasing seq %d at offset %d", record.Seq, cursor.Offset)
		}
		record.Raw = append(json.RawMessage(nil), trimmed...)
		records = append(records, record)
		cursor.Offset += int64(len(line))
		cursor.Seq = record.Seq
	}
}
