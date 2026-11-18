package core

import (
	"encoding/json"

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
