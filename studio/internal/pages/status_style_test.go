package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

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
			tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.dashboardView(c) }) }, 1000, 600)
			if _, ok := tt.Find(tc.want); !ok {
				t.Fatal(tt.Texts())
			}
			if tt.HasText(tc.state) {
				t.Fatalf("raw state in task table: %v", tt.Texts())
			}
		})
	}
}
