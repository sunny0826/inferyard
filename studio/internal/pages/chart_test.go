package pages

import (
	"math"

	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func chartSample(seq int64, metric, source string, seconds int, value *float64) core.ResourceSample {
	unit := "bytes"
	if metric == "service_cpu_percent" {
		unit = "%"
	} else if metric == "temperature" {
		unit = "celsius"
	}
	sample := core.ResourceSample{
		Source: core.ResourceSource{Metric: metric, Name: source, Unit: unit, ClockID: "clock", Collector: "fixture"},
		Utc:    time.Date(2026, 10, 14, 1, 0, seconds, 0, time.UTC).Format(time.RFC3339Nano),
		Seq:    seq, ReadStartedNs: int64(seconds+1) * 1e9, ReadFinishedNs: int64(seconds+1)*1e9 + 1e6, Value: value,
	}
	if value == nil {
		sample.MissingReason = ptr("sensor_unavailable")
	}
	return sample
}

func chartSamples() []core.ResourceSample {
	var samples []core.ResourceSample
	for i, seconds := range []int{0, 1, 9} {
		for j, item := range []struct {
			metric, source string
			value          float64
		}{
			{"system_mem_available", "fixture:available", float64(1+i/2) * 1073741824},
			{"service_rss", "fixture:rss", 536870912},
			{"service_cpu_percent", "fixture:cpu", float64(120 + i*10)},
			{"temperature", "AppleSMC:Tg0C", float64(40 + i*5)},
			{"temperature", "AppleSMC:Tp0C", float64(60 + i*5)},
		} {
			value := ptr(item.value)
			if i == 1 {
				value = nil
			}
			samples = append(samples, chartSample(int64(i*5+j+1), item.metric, item.source, seconds, value))
		}
	}
	return samples
}

func TestResourceLineDataPreservesSourceGapsAndUnits(t *testing.T) {
	groups := resourceSeriesData(chartSamples())
	if len(groups) != 5 {
		t.Fatal("sensor sources merged", groups)
	}
	for _, group := range groups {
		data := resourceLineData(group, ui.Hex("#123456"))
		values := data.Series[0].Values
		if len(values) != 3 || !math.IsNaN(values[1]) || data.Labels[2] != "00:09" {
			t.Fatal("lost source gap or elapsed time", data)
		}
		if group.source.Metric == "system_mem_available" && (values[0] != 1 || values[2] != 2) {
			t.Fatal("memory units", values)
		}
		if group.source.Name == "AppleSMC:Tp0C" && values[2] != 70 {
			t.Fatal("mixed temperature sources", values)
		}
	}
	if finite(nil) || finite(ptr(math.NaN())) || finite(ptr(math.Inf(1))) {
		t.Fatal("missing / non-finite point accepted")
	}
}
