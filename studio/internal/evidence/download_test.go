package evidence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPollDownload(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "model.gguf.part")
	write(t, part, "123")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	err := PollDownload(ctx, dir, time.Millisecond, func(p DownloadProgress) error {
		calls++
		if len(p.Files) != 1 {
			t.Fatalf("%+v", p)
		}
		switch calls {
		case 1:
			if p.Files[0].Bytes != 3 || p.Complete {
				t.Fatalf("%+v", p)
			}
			appendText(t, part, "45")
		case 2:
			if p.Files[0].Bytes != 5 || p.Complete {
				t.Fatalf("%+v", p)
			}
			return os.Rename(part, filepath.Join(dir, "model.gguf"))
		case 3:
			if !p.Complete || p.Files[0].Bytes != 5 {
				t.Fatalf("%+v", p)
			}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("calls=%d %v", calls, err)
	}
}
func TestPollMissingEmptyAndRemoved(t *testing.T) {
	if err := PollDownload(context.Background(), filepath.Join(t.TempDir(), "missing"), time.Millisecond, nil); !os.IsNotExist(err) {
		t.Fatalf("missing: %v", err)
	}
	for _, part := range []bool{false, true} {
		dir := t.TempDir()
		if part {
			write(t, filepath.Join(dir, "model.part"), "data")
		}
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		err := PollDownload(ctx, dir, time.Millisecond, func(p DownloadProgress) error {
			calls++
			if p.Complete {
				t.Fatal("false completion")
			}
			if part && calls == 1 {
				if err := os.Remove(filepath.Join(dir, "model.part")); err != nil {
					t.Fatal(err)
				}
			}
			if calls == 2 {
				cancel()
			}
			return nil
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	}
	sentinel := errors.New("callback failed")
	if err := PollDownload(context.Background(), t.TempDir(), time.Millisecond, func(DownloadProgress) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func TestPollAlreadyPublishedTarget(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "model.gguf"), "done")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := PollDownload(ctx, dir, time.Millisecond, func(p DownloadProgress) error {
		if !p.Complete || len(p.Files) != 1 || p.Files[0].Bytes != 4 {
			t.Fatalf("%+v", p)
		}
		return nil
	}, "model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if err := PollDownload(ctx, dir, time.Millisecond, nil, "../outside"); err == nil {
		t.Fatal("invalid target accepted")
	}
}
