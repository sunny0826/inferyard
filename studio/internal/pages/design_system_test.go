package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"math"
	"testing"
)

func TestDesignProgressRanges(t *testing.T) {
	for _, mode := range []string{"service", "run", "detail"} {
		for _, total := range []*int64{nil, ptr(int64(0)), ptr(int64(4))} {
			t.Run(mode+"/"+number(total), func(t *testing.T) {
				s := testState()
				s.engineState.State = "starting"
				s.tasks = []core.RunTask{{TaskId: "active", State: "running"}}
				s.runProgress = &core.RunProgress{Done: 1, Total: total}
				s.detail = &core.RunTaskDetail{Task: s.tasks[0], Progress: s.runProgress}
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Children(func() {
						switch mode {
						case "service":
							s.serviceView(c)
						case "run":
							s.runView(c)
						case "detail":
							s.detailView(c)
						}
					})
				}, 1120, 1600)
				nodes := modelUINodes(tt, "widget", "")
				for i := len(nodes) - 1; i >= 0; i-- {
					if nodes[i].FieldByName("role").Uint() != uint64(ui.RoleProgress) {
						nodes = append(nodes[:i], nodes[i+1:]...)
					}
				}
				if len(nodes) != 1 {
					t.Fatalf("progress count %d", len(nodes))
				}
				value := nodes[0].FieldByName("accRange").Index(2).Float()
				want := -1.0
				if mode != "service" && total != nil && *total > 0 {
					want = .25
				}
				if math.IsNaN(value) || value != want {
					t.Fatalf("progress = %v, want %v", value, want)
				}
				if nodes[0].FieldByName("label").String() == "" {
					t.Fatal("unnamed progress")
				}
			})
		}
	}
}

func TestDesignDetailFailureIsNotLoading(t *testing.T) {
	s := testState()
	s.selected, s.detailID = "dashboard", "missing"
	s.detailError = &core.ApiError{Code: "not_found", Message: "任务详情读取失败"}
	tt := ui.NewTester(s.view, 1120, 800)
	if tt.HasText("正在读取任务详情") {
		t.Fatal("failed detail still claims loading")
	}
	if _, ok := tt.Find("重试读取详情"); !ok {
		t.Fatal("missing retry")
	}
	click(t, tt, "返回 Dashboard")
	if s.detailID != "" {
		t.Fatal("return failed")
	}
}
