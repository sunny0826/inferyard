package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"inferyard/studio/internal/evidence"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func resourceRow(cycle, offset int, metric string, value any, missing any) evidence.Record {
	raw, _ := json.Marshal(map[string]any{
		"utc":         time.Date(2026, 10, 12, 0, 0, cycle, 950000000, time.UTC).Add(time.Duration(offset) * time.Millisecond).Format(time.RFC3339Nano),
		"metric_name": metric, "value": value, "missing_reason": missing,
		"read_finished_ns": int64(cycle+1)*1e9 + int64(offset)*1e6,
		"clock_id":         "clock", "server_pid": 42, "process_start_ticks": 1,
		"clock_ticks_per_second": 100, "capture": "periodic", "phase": "formal",
	})
	return evidence.Record{Raw: raw}
}

func TestResourceSamplingWindowMerge(t *testing.T) {
	a := newAggregate(nil)
	rows := []evidence.Record{}
	for cycle := 0; cycle < 2; cycle++ {
		rows = append(rows, resourceRow(cycle, 0, "system_mem_available", 1024+cycle, nil),
			resourceRow(cycle, 20, "service_rss", 256+cycle, nil),
			resourceRow(cycle, 80, "service_cpu_ticks", 100+50*cycle, nil),
			resourceRow(cycle, 90, "temperature", 40+cycle, nil))
	}
	points := a.resources(rows)
	if len(points) != 2 {
		t.Fatalf("want two cycles, got %+v", points)
	}
	for i, p := range points {
		if p.MemAvailableBytes == nil || *p.MemAvailableBytes != int64(1024+i) || p.ServiceRssBytes == nil || *p.ServiceRssBytes != int64(256+i) || p.TemperatureCelsius == nil || *p.TemperatureCelsius != float64(40+i) {
			t.Fatalf("unmerged cycle %d: %+v", i, p)
		}
	}
	if points[0].ServiceCpuPercent != nil || points[1].ServiceCpuPercent == nil || *points[1].ServiceCpuPercent != 50 {
		t.Fatal(points)
	}
	// Tail can stop between any two metric records. REST and SSE both upsert
	// complete revisions under the first metric's UTC, even across a second boundary.
	split := newAggregate(nil)
	var retained []ResourceSample
	for _, row := range rows {
		retained = appendResources(retained, split.resources([]evidence.Record{row}))
	}
	if !reflect.DeepEqual(retained, points) {
		t.Fatalf("split reads differ: %+v / %+v", retained, points)
	}
	missing := a.resources([]evidence.Record{resourceRow(2, 0, "system_mem_available", 9999, "sensor_unavailable"), resourceRow(2, 20, "service_rss", 300, nil)})
	if len(missing) != 1 || missing[0].MemAvailableBytes != nil || missing[0].ServiceCpuPercent != nil || missing[0].TemperatureCelsius != nil || missing[0].ServiceRssBytes == nil {
		t.Fatalf("missing fields carried forward: %+v", missing)
	}
}

func TestResourceSamplingWindowBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []evidence.Record
	}{
		{"time_gap", []evidence.Record{resourceRow(0, 0, "system_mem_available", 1, nil), resourceRow(1, 0, "service_rss", 2, nil)}},
		{"repeated_metric", []evidence.Record{resourceRow(0, 0, "system_mem_available", 1, nil), resourceRow(0, 100, "system_mem_available", 2, nil)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := newAggregate(nil).resources(tc.rows); len(got) != 2 {
				t.Fatal(got)
			}
		})
	}
	rows := []evidence.Record{resourceRow(0, 0, "system_mem_available", 1, nil), resourceRow(0, 0, "service_rss", 2, nil)}
	if got := newAggregate(nil).resources(rows); len(got) != 1 || got[0].MemAvailableBytes == nil || got[0].ServiceRssBytes == nil {
		t.Fatal(got)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func TestCorruptSettingsUseDefaults(t *testing.T) {
	for _, raw := range []string{`{`, `{"inferyard_path":"/must-not-survive","models_root":42}`, `[]`} {
		t.Run(fmt.Sprintf("case_%d", len(raw)), func(t *testing.T) {
			logs := captureLogs(t)
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := New(ctx, Options{SettingsPath: path})
			if err != nil {
				t.Fatal(err)
			}
			if got := s.userSettings(); !reflect.DeepEqual(got, UserSettings{}) {
				t.Fatalf("partial settings survived: %+v", got)
			}
			if !strings.Contains(logs.String(), "using defaults") || strings.Contains(logs.String(), raw) {
				t.Fatal(logs.String())
			}
			saved, err := os.ReadFile(path)
			if err != nil || string(saved) != raw {
				t.Fatal("startup overwrote corrupt settings")
			}
			if _, err := s.UpdateSettings(map[string]json.RawMessage{}); err != nil {
				t.Fatal(err)
			}
			saved, err = os.ReadFile(path)
			if err != nil || !json.Valid(saved) {
				t.Fatal("cannot save after recovery", err)
			}
		})
	}
}

func TestSettingsExistingDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	s := testService(t, nil)
	dir := filepath.Dir(s.settingsPath)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSettings(map[string]json.RawMessage{}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0700, s.settingsPath: 0600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s: %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}
