package pages

import (
	"math"

	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
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

func TestResourceViewFormatsValuesToTwoDecimalsAndReadableDates(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = oldLocal })

	samples := []core.ResourceSample{
		{
			Source: core.ResourceSource{Metric: "system_mem_available", Name: "fixture:available", Unit: "bytes", ClockID: "clock", Collector: "fixture"},
			Utc:    "2026-10-20T09:21:05.571234Z", Seq: 1, ReadStartedNs: 1e9, ReadFinishedNs: 1e9 + 1e6,
			Value: ptr(3250585600.0), // 3.02734375 GiB -> 3.03 GiB
		},
		{
			Source: core.ResourceSource{Metric: "service_rss", Name: "fixture:rss", Unit: "bytes", ClockID: "clock", Collector: "fixture"},
			Utc:    "2026-10-20T09:21:05.571234Z", Seq: 2, ReadStartedNs: 1e9, ReadFinishedNs: 1e9 + 1e6,
			Value: ptr(1932735283.2), // 1.800000000... GiB -> 1.80 GiB
		},
		{
			Source: core.ResourceSource{Metric: "service_cpu_percent", Name: "fixture:cpu", Unit: "%", ClockID: "clock", Collector: "fixture"},
			Utc:    "2026-10-20T09:21:05.571234Z", Seq: 3, ReadStartedNs: 1e9, ReadFinishedNs: 1e9 + 1e6,
			Value: ptr(124.8391029384712), // -> 124.84%
		},
		{
			Source: core.ResourceSource{Metric: "temperature", Name: "AppleSMC:Tp0C", Unit: "celsius", ClockID: "clock", Collector: "fixture"},
			Utc:    "2026-10-20T09:21:05.571234Z", Seq: 4, ReadStartedNs: 1e9, ReadFinishedNs: 1e9 + 1e6,
			Value: ptr(54.37890625), // -> 54.38 °C
		},
		{
			Source: core.ResourceSource{Metric: "temperature", Name: "AppleSMC:Tp0C", Unit: "celsius", ClockID: "clock", Collector: "fixture"},
			Utc:    "2026-10-20T09:21:06.120000Z", Seq: 5, ReadStartedNs: 2e9, ReadFinishedNs: 2e9 + 1e6,
			MissingReason: ptr("smc_source_unavailable"),
		},
	}

	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { s.resourceView(c, samples) })
	}, 1120, 1400)
	tt.Frame()

	for _, item := range []struct {
		plotLabel string
		wantTip   string
	}{
		{"可用内存 GiB曲线", "2026-10-20 17:21:05 · 3.03 GiB"},
		{"服务 RSS GiB曲线", "2026-10-20 17:21:05 · 1.80 GiB"},
		{"服务 CPU %曲线", "2026-10-20 17:21:05 · 124.84%"},
		{"传感器温度 °C曲线", "2026-10-20 17:21:05 · 54.38 °C"},
	} {
		r, ok := tt.Find(item.plotLabel)
		if !ok {
			t.Fatalf("plot %q not found in %v", item.plotLabel, tt.Texts())
		}
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
		tt.Move(0, 0)
		tt.Key(0, ui.KeyHome)
		if !tt.HasText(item.wantTip) {
			t.Fatalf("plot %q tooltip mismatch: want %q in %v", item.plotLabel, item.wantTip, tt.Texts())
		}
	}

	// Missing sample tooltip keeps readable timestamp and omits trailing unit.
	tt.Key(0, ui.KeyRight)
	if !tt.HasText("2026-10-20 17:21:06 · 缺测：smc_source_unavailable") {
		t.Fatalf("missing sample tooltip mismatch: %v", tt.Texts())
	}

	click(t, tt, "原始采样数值")
	tt.Frame()
	for _, want := range []string{"时间", "2026-10-20 17:21:05", "3.03", "1.80", "GiB", "124.84", "%", "54.38", "°C", "缺测", "smc_source_unavailable"} {
		if !tt.HasText(want) {
			t.Fatalf("sample table missing %q in %v", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"2026-10-20T09:21:05Z", "54.37890625", "124.8391029384712", "3250585600", "celsius", "bytes", "UTC"} {
		if tt.HasText(unwanted) {
			t.Fatalf("sample table still shows raw unformatted text %q in %v", unwanted, tt.Texts())
		}
	}
	if v, u := formatResourceSample(nil, core.ResourceSample{
		Source: core.ResourceSource{Metric: "temperature", Unit: "celsius"},
		Value:  ptr(-0.001),
	}); v != "0.00" || u != "°C" {
		t.Fatalf("negative near-zero sample formatted as %q %q, want 0.00 °C", v, u)
	}

	restoreEn := l10n.SetTestLocale(l10n.LanguageEn)
	defer restoreEn()
	ttEn := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { s.resourceView(c, samples) })
	}, 1120, 1400)
	ttEn.SetLocale(l10n.LanguageEn)
	ttEn.Frame()
	click(t, ttEn, "Raw Sample Values")
	ttEn.Frame()
	for _, want := range []string{"Time", "Missing", "2026-10-20 17:21:05", "54.38", "°C"} {
		if !ttEn.HasText(want) {
			t.Fatalf("english sample table missing %q in %v", want, ttEn.Texts())
		}
	}
}

func TestResourceViewOmitsTemperatureSourceDropdownAndKeepsAllSourcesInTable(t *testing.T) {
	samples := chartSamples()
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { s.resourceView(c, samples) })
	}, 1120, 1400)
	tt.Frame()

	if _, found := tt.Find("传感器温度 °C来源"); found {
		t.Fatalf("temperature source dropdown should be removed, found in %v", tt.Texts())
	}
	if !tt.HasText("AppleSMC:Tg0C") {
		t.Fatalf("expected default temperature source label AppleSMC:Tg0C in %v", tt.Texts())
	}
	if _, ok := tt.Find("传感器温度 °C曲线"); !ok {
		t.Fatalf("temperature plot missing in %v", tt.Texts())
	}

	click(t, tt, "原始采样数值")
	tt.Frame()
	for _, want := range []string{"AppleSMC:Tg0C", "AppleSMC:Tp0C"} {
		if !tt.HasText(want) {
			t.Fatalf("sample table missing temperature source %q in %v", want, tt.Texts())
		}
	}

	// When the first sensor has only missing samples, default to the next sensor with valid values.
	fallbackSamples := []core.ResourceSample{
		chartSample(1, "temperature", "AppleSMC:Tg0C", 0, nil),
		chartSample(2, "temperature", "AppleSMC:Tp0C", 0, ptr(58.5)),
	}
	sFallback := testState()
	ttFallback := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(1080).Children(func() { sFallback.resourceView(c, fallbackSamples) })
	}, 1120, 1400)
	ttFallback.Frame()

	if !ttFallback.HasText("AppleSMC:Tp0C") {
		t.Fatalf("expected fallback to valid temperature source AppleSMC:Tp0C in %v", ttFallback.Texts())
	}
	if _, ok := ttFallback.Find("传感器温度 °C曲线"); !ok {
		t.Fatalf("fallback temperature plot missing in %v", ttFallback.Texts())
	}
}
