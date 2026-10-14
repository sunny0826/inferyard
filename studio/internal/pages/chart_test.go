package pages

import (
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func chartSamples() []core.ResourceSample {
	return []core.ResourceSample{
		{Utc: "2026-10-14T01:00:00Z", MemAvailableBytes: ptr(int64(1073741824)), ServiceCpuPercent: ptr(10.0), TemperatureCelsius: ptr(40.0)},
		{Utc: "2026-10-14T01:00:01Z"},
		{Utc: "2026-10-14T01:00:09Z", MemAvailableBytes: ptr(int64(2147483648)), ServiceCpuPercent: ptr(20.0), TemperatureCelsius: ptr(50.0)},
	}
}

func TestResourceLineDataPreservesGapsAndUnits(t *testing.T) {
	points := resourcePoints(chartSamples())
	colors := []ui.Color{ui.Hex("#123456"), ui.Hex("#234567"), ui.Hex("#345678"), ui.Hex("#456789")}
	data := resourceChartData(points, colors, 0, 1, 2, 3)
	for i, series := range data.Series {
		if len(series.Values) != 3 || !math.IsNaN(series.Values[1]) || series.Color != colors[i] {
			t.Fatalf("lost gap/color: %+v", series)
		}
	}
	if data.Series[0].Values[0] != 1 || data.Series[0].Values[2] != 2 || data.Series[2].Values[2] != 20 || data.Series[3].Values[2] != 50 {
		t.Fatal("unit conversion changed", data.Series)
	}
	if data.Labels[2] != "2026-10-14T01:00:09Z" || !math.IsNaN(data.Series[1].Values[0]) {
		t.Fatal("lost UTC or missing RSS")
	}
}

func TestResourceLinePointerAndKeyboardSelection(t *testing.T) {
	s := testState()
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.resourceView(c, chartSamples()) }) }, 1120, 1800)
	bounds, ok := tt.Find("内存 GiB曲线")
	if !ok {
		t.Fatal("missing memory chart")
	}
	tt.Move(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	if s.chartIndex != 1 {
		t.Fatal("hover missed gap sample", s.chartIndex)
	}
	found := false
	for _, text := range tt.Texts() {
		if strings.Contains(text, "01:00:01Z · 可用内存 缺测") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing sample description", tt.Texts())
	}
	tt.Move(1200, 1900)
	if s.chartIndex != -1 {
		t.Fatal("hover exit did not clear selection")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("内存 GiB曲线") {
		t.Fatal("chart is not keyboard focusable")
	}
	tt.Key(0, ui.KeyEnd)
	if s.chartIndex != 2 {
		t.Fatal("End did not select last sample")
	}
	tt.Key(0, ui.KeyLeft)
	if s.chartIndex != 1 {
		t.Fatal("keyboard skipped missing sample")
	}
	tt.Key(0, ui.KeyEscape)
	if s.chartIndex != -1 {
		t.Fatal("Escape did not clear selection")
	}
	for _, want := range []string{"采样点：3", "1073741824", "2147483648", "CPU %", "温度 °C"} {
		if !tt.HasText(want) {
			t.Fatal("missing raw measurement", want)
		}
	}
}
