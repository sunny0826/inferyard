package core

import (
	"inferyard/studio/internal/evidence"
)

const resourceWindow = 600

func (a *aggregate) resources(records []evidence.Record) []ResourceSample {
	points := []ResourceSample{}
	for _, r := range records {
		m := document(r.Raw)
		metric := str(text(m["metric_name"]))
		if metric != "system_mem_available" && metric != "service_rss" && metric != "temperature" && metric != "service_cpu_ticks" {
			continue
		}
		if metric == "service_cpu_ticks" && m["capture"] != "periodic" {
			continue
		}
		start, end := integer(m["read_started_ns"]), integer(m["read_finished_ns"])
		if start == nil || end == nil || *start < 0 || *end < *start {
			continue
		}
		point := ResourceSample{
			Source: ResourceSource{
				Metric: metric, Collector: str(text(m["collector"])), Name: str(text(m["source"])),
				ClockID: str(text(m["clock_id"])), Unit: str(text(m["unit"])), Semantics: str(text(m["semantics"])),
			},
			Seq: r.Seq, Utc: str(text(m["utc"])), ReadStartedNs: *start, ReadFinishedNs: *end,
			Phase: str(text(m["phase"])), RequestID: text(m["request_id"]),
			Value: number(m["value"]), MissingReason: text(m["missing_reason"]),
		}
		if metric == "service_cpu_ticks" {
			previous := a.cpuPrevious[point.Source]
			a.cpuPrevious[point.Source] = m
			point.Value, point.MissingReason = cpuPercent(m, previous, int64(a.resourceGap))
			point.Source.Metric, point.Source.Unit = "service_cpu_percent", "%"
		}
		if point.MissingReason != nil {
			point.Value = nil
		}
		points = append(points, point)
	}
	return points
}

func cpuPercent(current, previous object, maximumGap int64) (*float64, *string) {
	missing := func(reason string) (*float64, *string) { return nil, ptr(reason) }
	if reason := text(current["missing_reason"]); reason != nil {
		return nil, reason
	}
	if previous == nil {
		return missing("baseline_required")
	}
	if previous["missing_reason"] != nil || previous["value"] == nil {
		return missing("baseline_required")
	}
	for _, k := range []string{"collector", "clock_id", "source", "server_pid", "process_start_ticks", "boot_id", "clock_ticks_per_second"} {
		if current[k] == nil || previous[k] == nil || current[k] != previous[k] {
			return missing("source_changed")
		}
	}
	v, old, hz := number(current["value"]), number(previous["value"]), number(current["clock_ticks_per_second"])
	start, end := number(current["read_started_ns"]), number(current["read_finished_ns"])
	oldStart, oldEnd := number(previous["read_started_ns"]), number(previous["read_finished_ns"])
	if v == nil || old == nil || hz == nil || *hz <= 0 || start == nil || end == nil || oldStart == nil || oldEnd == nil {
		return missing("invalid_counter")
	}
	if *end < *start || *oldEnd < *oldStart || *start < *oldEnd {
		return missing("overlapping_counter_reads")
	}
	if *v < *old {
		return missing("counter_reset")
	}
	for _, k := range []string{"user_ticks", "system_ticks"} {
		value, last := number(obj(current["raw_cpu"])[k]), number(obj(previous["raw_cpu"])[k])
		if value == nil || last == nil {
			return missing("invalid_counter")
		}
		if *value < *last {
			return missing("counter_reset")
		}
	}
	delta := (*start + *end - *oldStart - *oldEnd) / 2
	if delta <= 0 || delta > float64(maximumGap) {
		return missing("sampling_gap")
	}
	return ptr((*v - *old) / *hz / (delta / 1e9) * 100), nil
}

func appendResources(dst, points []ResourceSample) []ResourceSample {
	all := append(dst, points...)
	counts := map[ResourceSource]int{}
	keep := make([]bool, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		key := all[i].Source
		counts[key]++
		keep[i] = counts[key] <= resourceWindow
	}
	retained := all[:0]
	for i, point := range all {
		if keep[i] {
			retained = append(retained, point)
		}
	}
	return retained
}
