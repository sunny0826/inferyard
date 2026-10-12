package evidence

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func appendText(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}
func TestTailAppendPartialResume(t *testing.T) {
	for _, name := range []string{"events.jsonl", "memory.jsonl"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			first := "{\"seq\":1,\"phase\":\"probe\"}\n"
			write(t, path, first+`{"seq":2`)
			rows, cursor, err := Tail(path, Cursor{})
			if err != nil || len(rows) != 1 || cursor.Offset != int64(len(first)) || cursor.Seq != 1 {
				t.Fatalf("%+v %+v %v", rows, cursor, err)
			}
			rows, next, err := Tail(path, cursor)
			if err != nil || len(rows) != 0 || next != cursor {
				t.Fatalf("partial consumed: %+v %v", next, err)
			}
			appendText(t, path, "}\n{\"seq\":3}\n")
			rows, next, err = Tail(path, Cursor{Offset: cursor.Offset, Seq: cursor.Seq})
			if err != nil || len(rows) != 2 || rows[0].Seq != 2 || next.Seq != 3 {
				t.Fatalf("%+v %+v %v", rows, next, err)
			}
			rows, _, err = Tail(path, next)
			if err != nil || len(rows) != 0 {
				t.Fatal("duplicate delivery")
			}
			write(t, path, first)
			if _, _, err = Tail(path, next); !errors.Is(err, ErrTruncated) {
				t.Fatalf("truncation: %v", err)
			}
		})
	}
}
func TestTailRejectsCorruption(t *testing.T) {
	for _, bad := range []string{"not json\n", "null\n", "{}\n", "{\"seq\":1}\n", "{\"seq\":0}\n"} {
		path := filepath.Join(t.TempDir(), "events.jsonl")
		write(t, path, "{\"seq\":1}\n"+bad)
		rows, cursor, err := Tail(path, Cursor{})
		if err == nil || len(rows) != 1 || cursor.Seq != 1 {
			t.Fatalf("%q %+v %+v %v", bad, rows, cursor, err)
		}
	}
}
func TestFixtureTail(t *testing.T) {
	for _, name := range []string{"events.jsonl", "memory.jsonl"} {
		rows, cursor, err := Tail(filepath.Join("testdata", "run", name), Cursor{})
		if err != nil || len(rows) != 2 || cursor.Seq != 2 {
			t.Fatalf("%s %+v %v", name, cursor, err)
		}
	}
}
