package pages

import (
	"math"
	"strings"
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

func TestResourceSameUTCKeepsDistinctObservations(t *testing.T) {
	samples := []core.ResourceSample{
		chartSample(3, "temperature", "sensor-a", 2, ptr(42.0)),
		chartSample(1, "temperature", "sensor-a", 0, ptr(40.0)),
		chartSample(2, "temperature", "sensor-b", 0, ptr(55.0)),
		chartSample(4, "temperature", "sensor-a", 2, ptr(43.0)),
	}
	groups := resourceSeriesData(samples)
	if len(groups) != 2 || len(groups[0].points) != 3 || *groups[0].points[2].Value != 43 || *groups[1].points[0].Value != 55 {
		t.Fatal("same UTC overwrote evidence", groups)
	}
}

func TestDenseMemoryOnlyResourcesStayCompact(t *testing.T) {
	s := testState()
	var samples []core.ResourceSample
	for i := range 158 {
		samples = append(samples,
			chartSample(int64(i*2+1), "system_mem_available", "available", i, ptr(float64(3<<30))),
			chartSample(int64(i*2+2), "service_rss", "rss", i, ptr(float64(1<<28))))
	}
	for _, group := range resourceSeriesData(samples) {
		for _, label := range resourceLineData(group, ui.Color{}).Labels {
			if len(label) > 9 || strings.Contains(label, "2026") {
				t.Fatalf("unreadable axis label %q", label)
			}
		}
	}
	tt := ui.NewTester(func(c *ui.Context) { s.resourceView(c, samples) }, 1000, 1400)
	for _, label := range []string{"服务 CPU %曲线", "传感器温度 °C曲线"} {
		if _, ok := tt.Find(label); ok {
			t.Fatalf("unrecorded metric became a plot: %s", label)
		}
	}
	for _, label := range []string{"服务 CPU % · 此运行未记录该指标", "传感器温度 °C · 此运行未记录该指标"} {
		if !tt.HasText(label) {
			t.Fatal("missing unavailable state", tt.Texts())
		}
	}
}

func TestResourceLinePointerAndKeyboardSelection(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, 1120, 2100)
	bounds, ok := tt.Find("可用内存 GiB曲线")
	if !ok {
		t.Fatal("missing memory chart")
	}
	hasDetail := func(fragment string) bool {
		for _, text := range tt.Texts() {
			if strings.Contains(text, fragment) {
				return true
			}
		}
		return false
	}
	tt.Move(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	if !hasDetail("01:00:01Z · 缺测：sensor_unavailable") {
		t.Fatal("hover lost missing sample reason", tt.Texts())
	}
	tt.Move(1200, 2200)
	if hasDetail("01:00:01Z · 缺测：sensor_unavailable") {
		t.Fatal("hover exit retained detail")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("可用内存 GiB曲线") {
		t.Fatal("chart is not keyboard focusable")
	}
	tt.Key(0, ui.KeyEnd)
	if !hasDetail("01:00:09Z ·") {
		t.Fatal("End did not select last sample", tt.Texts())
	}
	tt.Key(0, ui.KeyLeft)
	if !hasDetail("01:00:01Z · 缺测：sensor_unavailable") {
		t.Fatal("keyboard skipped missing sample")
	}
	tt.Key(0, ui.KeyEscape)
	if hasDetail("01:00:01Z · 缺测：sensor_unavailable") {
		t.Fatal("Escape did not clear selection")
	}
}

func TestResourceTemperatureSourcePicker(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, 1120, 2300)
	if err := tt.Click("传感器温度 °C来源"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("AppleSMC:Tp0C"); err != nil {
		t.Fatal(err, tt.Texts())
	}
	bounds, ok := tt.Find("传感器温度 °C曲线")
	if !ok {
		t.Fatal("temperature chart missing after source change")
	}
	tt.Move(bounds.X+bounds.W*.95, bounds.Y+bounds.H/2)
	found := false
	for _, text := range tt.Texts() {
		found = found || strings.Contains(text, "70 celsius")
	}
	if !found {
		t.Fatal("source selection did not update measurements", tt.Texts())
	}
}
