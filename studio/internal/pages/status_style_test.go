package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func assertTextColor(t *testing.T, tt *ui.Tester, label string, want ui.Color) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q: %v", label, tt.Texts())
	}
	img := tt.Image()
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := img.RGBAAt(x, y)
			if absDiff(p.R, want.R) < 8 && absDiff(p.G, want.G) < 8 && absDiff(p.B, want.B) < 8 {
				return
			}
		}
	}
	t.Fatalf("%q does not render theme color %v", label, want)
}

func absDiff(a, b uint8) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func TestDashboardStepStyles(t *testing.T) {
	s := testState()
	var theme *ui.Theme
	tt := ui.NewTester(func(c *ui.Context) { theme = c.Theme(); ui.Column(c).Children(func() { s.conclusionView(c) }) }, 1120, 1600)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		assertTextColor(t, tt, "✓  准备", theme.Success)
		assertTextColor(t, tt, "启动与绑定", theme.Accent)
		assertTextColor(t, tt, "报告核验", theme.TextMuted)
		if tt.HasText("○") || tt.HasText("●") {
			t.Fatal(tt.Texts())
		}
	}
	s.progress.DefaultScopeComplete = true
	tt.Frame()
	assertTextColor(t, tt, "✓  报告核验", theme.Success)
	s.progress = nil
	tt.Frame()
	assertTextColor(t, tt, "准备", theme.Accent)
	assertTextColor(t, tt, "设备检查", theme.TextMuted)
}

func TestTaskTableChineseBadgeText(t *testing.T) {
	for _, tc := range []struct {
		state  string
		queued bool
		exit   *core.RunExit
		want   string
	}{
		{"done", false, &core.RunExit{ExitCode: 0}, "成功"},
		{"failed", false, &core.RunExit{ExitCode: 2}, "输入 / 预检阻断"},
		{"done", false, &core.RunExit{ExitCode: 3}, "运行不完整"},
		{"failed", false, &core.RunExit{ExitCode: 4}, "工具 / 证据错误"},
		{"cancelled", false, &core.RunExit{ExitCode: 130}, "已取消"},
		{"starting", true, nil, "排队中"},
		{"starting", false, nil, "启动中"},
		{"running", false, nil, "运行中"},
		{"cancelling", false, nil, "取消中"},
		{"done", false, nil, "已结束"},
		{"failed", false, nil, "失败"},
		{"cancelled", false, nil, "已取消"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			s := testState()
			s.tasks = []core.RunTask{{TaskId: "task", State: tc.state, Queued: tc.queued, Exit: tc.exit}}
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.tasksView(c) }) }, 1000, 600)
			if _, ok := tt.Find(tc.want); !ok {
				t.Fatal(tt.Texts())
			}
			if tt.HasText(tc.state) {
				t.Fatalf("raw state in task table: %v", tt.Texts())
			}
		})
	}
}

func TestResourceLegendThemeColors(t *testing.T) {
	s := testState()
	var theme *ui.Theme
	samples := []core.ResourceSample{{Utc: "2026-10-13T06:00:00Z", MemAvailableBytes: ptr(int64(1073741824)), ServiceRssBytes: ptr(int64(536870912)), ServiceCpuPercent: ptr(12.5), TemperatureCelsius: ptr(42.0)}}
	tt := ui.NewTester(func(c *ui.Context) { theme = c.Theme(); ui.Column(c).Children(func() { s.resourceView(c, samples) }) }, 1120, 1000)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		colors := []ui.Color{theme.Accent, theme.Success, theme.Warning, theme.Danger}
		for i, label := range []string{"可用内存 GiB（内存图）", "服务 RSS GiB（内存图）", "服务 CPU %（CPU 图）", "温度 °C（独立刻度）"} {
			assertTextColor(t, tt, label, colors[i])
			for j := 0; j < i; j++ {
				if colors[i] == colors[j] {
					t.Fatal("resource colors overlap")
				}
			}
		}
	}
}
