package evidence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type DownloadFile struct {
	Name     string
	Bytes    int64
	Complete bool
}
type DownloadProgress struct {
	Files    []DownloadFile
	Complete bool
}

// PollDownload observes part files and remembers them until their corresponding
// regular targets appear. An empty directory never means completion. Callers
// cancel the context when the CLI fails; disappearing parts alone mean nothing.
// Optional target basenames also detect downloads that finish between polls.
// notify is synchronous. interval <= 0 selects one second.
func PollDownload(ctx context.Context, dir string, interval time.Duration, notify func(DownloadProgress) error, targets ...string) error {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	known := map[string]DownloadFile{}
	for _, name := range targets {
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) || strings.HasSuffix(name, ".part") {
			return fmt.Errorf("invalid download target: %q", name)
		}
		known[name] = DownloadFile{Name: name}
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".part") {
				name := strings.TrimSuffix(entry.Name(), ".part")
				info, err := entry.Info()
				if os.IsNotExist(err) {
					if _, ok := known[name]; !ok {
						known[name] = DownloadFile{Name: name}
					}
					continue
				}
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					continue
				}
				known[name] = DownloadFile{Name: name, Bytes: info.Size()}
			}
		}
		progress := DownloadProgress{Complete: len(known) > 0}
		for name, file := range known {
			info, err := os.Lstat(filepath.Join(dir, name))
			if err == nil {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("download target is not a regular file: %s", name)
				}
				file.Bytes, file.Complete = info.Size(), true
			} else if os.IsNotExist(err) {
				file.Complete = false
			} else {
				return err
			}
			known[name] = file
			progress.Files = append(progress.Files, file)
			progress.Complete = progress.Complete && file.Complete
		}
		sort.Slice(progress.Files, func(i, j int) bool { return progress.Files[i].Name < progress.Files[j].Name })
		if notify != nil {
			if err := notify(progress); err != nil {
				return err
			}
		}
		if progress.Complete {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
