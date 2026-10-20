package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"inferyard/studio/internal/evidence"
)

func resourceRow(seq int64, metric, source string, seconds float64, value any, change func(object)) evidence.Record {
	start := int64(seconds*1e9) + 1e9
	unit := "bytes"
	if metric == "service_cpu_ticks" {
		unit = "ticks"
	} else if metric == "temperature" {
		unit = "celsius"
	}
	m := object{
		"utc":         time.Unix(0, start).UTC().Format(time.RFC3339Nano),
		"metric_name": metric, "source": source, "collector": "fixture", "unit": unit,
		"value": value, "missing_reason": nil, "read_started_ns": start, "read_finished_ns": start + 1_000_000,
		"clock_id": "clock", "server_pid": 42, "process_start_ticks": 1, "boot_id": "boot",
		"clock_ticks_per_second": 100, "capture": "periodic", "phase": "formal", "request_id": "case",
		"raw_cpu": object{"user_ticks": value, "system_ticks": 0},
	}
	if change != nil {
		change(m)
	}
	raw, _ := json.Marshal(m)
	return evidence.Record{Seq: seq, Raw: raw}
}

func TestResourceObservationsPreserveSourcesAndTailBoundaries(t *testing.T) {
	var rows []evidence.Record
	for cycle := range 2 {
		for i, source := range []string{"memory", "rss", "cpu", "AppleSMC:Tp0C", "AppleSMC:Tg0C"} {
			metric := []string{"system_mem_available", "service_rss", "service_cpu_ticks", "temperature", "temperature"}[i]
			rows = append(rows, resourceRow(int64(cycle*5+i+1), metric, source, float64(cycle), 100+cycle*150+i, nil))
		}
	}
	points := newAggregate(nil).resources(rows)
	if len(points) != 10 || points[3].Source.Name != "AppleSMC:Tp0C" || points[4].Source.Name != "AppleSMC:Tg0C" {
		t.Fatalf("same-time observations were merged: %+v", points)
	}
	if points[2].Value != nil || points[7].Value == nil || *points[7].Value != 150 {
		t.Fatal("single-core CPU percentage lost", points)
	}
	split := newAggregate(nil)
	var retained []ResourceSample
	for _, row := range rows {
		retained = appendResources(retained, split.resources([]evidence.Record{row}))
	}
	if !reflect.DeepEqual(retained, points) {
		t.Fatal("incremental observations differ")
	}
	missing := newAggregate(nil).resources([]evidence.Record{
		resourceRow(11, "temperature", "AppleSMC:Tp0C", 2, 99, func(m object) { m["missing_reason"] = "source_changed" }),
	})
	if len(missing) != 1 || missing[0].Value != nil || str(missing[0].MissingReason) != "source_changed" {
		t.Fatal("missing sensor became a measurement", missing)
	}
}

func TestResourceCPURejectionAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		change       func(object)
	}{
		{"pid", "source_changed", func(m object) { m["server_pid"] = 43 }},
		{"start", "source_changed", func(m object) { m["process_start_ticks"] = 2 }},
		{"boot", "source_changed", func(m object) { m["boot_id"] = "new-boot" }},
		{"scale", "source_changed", func(m object) { m["clock_ticks_per_second"] = 1000 }},
		{"missing_identity", "source_changed", func(m object) { delete(m, "boot_id") }},
		{"reset", "counter_reset", func(m object) { m["value"] = 50 }},
		{"component_reset", "counter_reset", func(m object) { m["raw_cpu"] = object{"user_ticks": 50, "system_ticks": 200} }},
		{"overlap", "overlapping_counter_reads", func(m object) { m["read_started_ns"] = 1e9 }},
		{"missing", "permission_denied", func(m object) { m["missing_reason"] = "permission_denied" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newAggregate(nil)
			points := a.resources([]evidence.Record{
				resourceRow(1, "service_cpu_ticks", "cpu", 0, 100, nil),
				resourceRow(2, "service_cpu_ticks", "cpu", 1, 250, tc.change),
			})
			if len(points) != 2 || points[1].Value != nil || str(points[1].MissingReason) != tc.reason {
				t.Fatal(points)
			}
		})
	}
	a := newAggregate(nil)
	points := a.resources([]evidence.Record{
		resourceRow(1, "service_cpu_ticks", "cpu", 0, 100, nil),
		resourceRow(2, "service_cpu_ticks", "cpu", 1, nil, func(m object) { m["missing_reason"] = "permission_denied" }),
		resourceRow(3, "service_cpu_ticks", "cpu", 2, 400, nil),
		resourceRow(4, "service_cpu_ticks", "cpu", 3, 550, nil),
	})
	if points[2].Value != nil || points[3].Value == nil || *points[3].Value != 150 {
		t.Fatal("CPU recovery crossed a missing sample", points)
	}
}

func TestResourceCPUContinuesAcrossRequestsAndPhases(t *testing.T) {
	points := newAggregate(nil).resources([]evidence.Record{
		resourceRow(1, "service_cpu_ticks", "cpu", 0, 100, nil),
		resourceRow(2, "service_cpu_ticks", "cpu", 1, 250, func(m object) { m["request_id"] = "next" }),
		resourceRow(3, "service_cpu_ticks", "cpu", 2, 400, func(m object) {
			m["phase"], m["request_id"] = "residual", nil
		}),
	})
	for _, point := range points[1:] {
		if point.Value == nil || *point.Value != 150 || point.MissingReason != nil {
			t.Fatalf("continuous process CPU lost at request/phase boundary: %+v", point)
		}
	}
	if str(points[1].RequestID) != "next" || points[2].Phase != "residual" || points[2].RequestID != nil {
		t.Fatal("observation metadata lost", points)
	}
}

func TestResourceCPUUsesReadMidpointAndConfiguredInterval(t *testing.T) {
	raw := []byte(`{"documents":{"config":{"payload":{"telemetry":{"interval_ms":5000}}}}}`)
	a := newAggregate(raw)
	points := a.resources([]evidence.Record{
		resourceRow(1, "service_cpu_ticks", "cpu", 0, 100, func(m object) { m["read_finished_ns"] = 2e9 }),
		resourceRow(2, "service_cpu_ticks", "cpu", 5, 1200, func(m object) { m["read_finished_ns"] = 8e9 }),
		resourceRow(3, "service_cpu_ticks", "cpu", 20, 1500, nil),
	})
	if points[1].Value == nil || *points[1].Value != 200 || str(points[2].MissingReason) != "sampling_gap" {
		t.Fatal("midpoint/interval calculation changed", points)
	}
}

func TestResourcesRetain600PerSourceWithoutDroppingCPU(t *testing.T) {
	var rows []evidence.Record
	for cycle := range 610 {
		for sensor := range 60 {
			rows = append(rows, resourceRow(int64(cycle*61+sensor+1), "temperature", fmt.Sprintf("sensor-%d", sensor), float64(cycle), 40+sensor, nil))
		}
		rows = append(rows, resourceRow(int64((cycle+1)*61), "service_cpu_ticks", "cpu", float64(cycle), cycle*150, nil))
	}
	all := appendResources(nil, newAggregate(nil).resources(rows))
	counts := map[ResourceSource]int{}
	for _, point := range all {
		counts[point.Source]++
		if point.ReadStartedNs < 11e9 {
			t.Fatal("old sample retained", point.Seq)
		}
	}
	if len(all) != 61*600 || len(counts) != 61 {
		t.Fatal("one source displaced another", len(all), len(counts))
	}
	for source, count := range counts {
		if count != 600 {
			t.Fatal(source, count)
		}
	}
}

func TestRecordedNativeResourceProjection(t *testing.T) {
	run := os.Getenv("INFERYARD_TEST_RESOURCE_RUN")
	if run == "" {
		t.Skip("set INFERYARD_TEST_RESOURCE_RUN to a new native diagnostic run")
	}
	inputs, err := os.ReadFile(filepath.Join(run, "inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := evidence.Tail(filepath.Join(run, "memory.jsonl"), evidence.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	points := newAggregate(inputs).resources(rows)
	cpu := 0
	temperature := map[string]int{}
	for _, point := range points {
		if point.Value == nil {
			continue
		}
		if point.Source.Metric == "service_cpu_percent" {
			cpu++
		}
		if point.Source.Metric == "temperature" {
			temperature[point.Source.Name]++
		}
	}
	if cpu == 0 || len(temperature) == 0 {
		t.Fatalf("native observations did not reach the display: cpu=%d temperature_sources=%d", cpu, len(temperature))
	}
	t.Logf("native evidence projected: %d CPU percentages, %d temperature sources, %d observations", cpu, len(temperature), len(points))
}
