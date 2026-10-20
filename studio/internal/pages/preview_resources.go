//go:build ui_debug && !release

package pages

import (
	"time"

	"inferyard/studio/internal/core"
)

func previewResourceSamples(seq int64, utc string, available, rss int64, cpu float64) []core.ResourceSample {
	stamp, _ := time.Parse(time.RFC3339Nano, utc)
	rows := []core.ResourceSample{}
	for i, item := range []struct {
		metric, name, unit string
		value              float64
	}{
		{"system_mem_available", "preview:available", "bytes", float64(available)},
		{"service_rss", "preview:rss", "bytes", float64(rss)},
		{"service_cpu_percent", "preview:cpu", "%", cpu},
		{"temperature", "AppleSMC:Tg0C", "celsius", 40 + cpu/20},
		{"temperature", "AppleSMC:Tp0C", "celsius", 50 + cpu/20},
	} {
		start := stamp.UnixNano() + int64(i)*1_000_000
		rows = append(rows, core.ResourceSample{
			Source: core.ResourceSource{Metric: item.metric, Name: item.name, Unit: item.unit, Collector: "preview", ClockID: "preview-clock"},
			Seq:    seq + int64(i), Utc: utc, ReadStartedNs: start, ReadFinishedNs: start + 100_000,
			Phase: "formal", Value: previewValue(item.value),
		})
	}
	return rows
}

func previewMultiSensorResources(s *viewState) {
	s.detailID = "multi-sensor-resources"
	s.detail = previewDetailFixture(s.detailID)
	s.detail.Task.State = "done"
	s.detail.Resources = nil
	for i := range 32 {
		utc := time.Date(2026, 10, 20, 9, 21, i, 0, time.UTC).Format(time.RFC3339Nano)
		rows := previewResourceSamples(int64(i*5+1), utc, 3<<30, 1<<28, 120+float64(i%12)*10)
		if i == 16 {
			rows[4].Value, rows[4].MissingReason = nil, previewValue("smc_source_unavailable")
		}
		s.detail.Resources = append(s.detail.Resources, rows...)
	}
	s.downloads = nil
}
