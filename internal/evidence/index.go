// Package evidence reads evidence without modifying or certifying its contents.
// PollDownload is retained as a tested standalone CLI-boundary observation helper.
// Studio uses its core download sampler to combine task state and transfer rates;
// PollDownload remains available for consumers needing only part-file progress.
package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type SealState string

const (
	Sealed  SealState = "sealed"
	Partial SealState = "partial"
)

type Run struct {
	ID        string
	Path      string
	BatchPath string
	State     SealState
	Inputs    json.RawMessage
	Manifest  json.RawMessage
}
type Batch struct {
	Path     string
	Metadata json.RawMessage
	Runs     []Run
}
type ResultIndex struct {
	Runs    []Run
	Batches []Batch
	Skipped int
	Error   error
}

// Index recognizes layout2 runs and batch.json/runs containers, including when
// root itself is a run or batch. Sealed is a declaration, not hash verification.
// Missing roots and malformed runs/batches are skipped and counted.
// Filesystem traversal failures are counted and also reported in Error.
func Index(root string) ResultIndex {
	index := ResultIndex{}
	var walk func(string, string)
	walk = func(dir, batch string) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			index.Skipped++
			if err != nil && !os.IsNotExist(err) && index.Error == nil {
				index.Error = err
			}
			return
		}
		if exists(filepath.Join(dir, "inputs.json")) {
			run, err := readRun(dir, batch)
			if err != nil {
				index.Skipped++
				return
			}
			index.Runs = append(index.Runs, run)
			return
		}
		if exists(filepath.Join(dir, "batch.json")) {
			raw, err := readObject(filepath.Join(dir, "batch.json"))
			if err != nil {
				index.Skipped++
				return
			}
			start := len(index.Runs)
			walk(filepath.Join(dir, "runs"), dir)
			runs := append([]Run(nil), index.Runs[start:]...)
			index.Batches = append(index.Batches, Batch{Path: dir, Metadata: raw, Runs: runs})
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			index.Skipped++
			if err != nil && !os.IsNotExist(err) && index.Error == nil {
				index.Error = err
			}
			return
		}
		children := 0
		for _, entry := range entries {
			if entry.IsDir() {
				children++
				walk(filepath.Join(dir, entry.Name()), batch)
			} else if entry.Type()&os.ModeSymlink != 0 {
				index.Skipped++
			}
		}
		if children == 0 {
			index.Skipped++
		}
	}
	walk(root, "")
	return index
}
func exists(path string) bool { _, err := os.Lstat(path); return !os.IsNotExist(err) }
func readObject(path string) (json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("not an object: %s", path)
	}
	return raw, nil
}
func readRun(dir, batch string) (Run, error) {
	run := Run{Path: dir, BatchPath: batch, State: Partial}
	raw, err := readObject(filepath.Join(dir, "inputs.json"))
	if err != nil {
		return run, err
	}
	var inputs struct {
		SchemaVersion int                        `json:"schema_version"`
		Layout        int                        `json:"evidence_layout_version"`
		RunID         string                     `json:"run_id"`
		Documents     map[string]json.RawMessage `json:"documents"`
	}
	if err = json.Unmarshal(raw, &inputs); err != nil {
		return run, err
	}
	if inputs.SchemaVersion != 3 || inputs.Layout != 2 || inputs.RunID == "" || len(inputs.Documents["run"]) == 0 {
		return run, fmt.Errorf("invalid layout2 inputs")
	}
	run.ID, run.Inputs = inputs.RunID, raw
	if !exists(filepath.Join(dir, "manifest.json")) {
		return run, nil
	}
	raw, err = readObject(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return run, err
	}
	var manifest struct {
		SchemaVersion int    `json:"schema_version"`
		Layout        int    `json:"evidence_layout_version"`
		RunID         string `json:"run_id"`
		Sealed        *bool  `json:"sealed"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return run, err
	}
	if manifest.SchemaVersion != 3 || manifest.Layout != 2 || manifest.RunID != run.ID || manifest.Sealed == nil {
		return run, fmt.Errorf("invalid manifest identity or seal")
	}
	run.Manifest = raw
	if *manifest.Sealed {
		run.State = Sealed
	}
	return run, nil
}
