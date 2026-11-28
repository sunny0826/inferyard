package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func TestStudioDialogRendersContentAndCloses(t *testing.T) {
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		studioDialog(c, &open, components.DialogOptions{Label: "测试弹窗"}, func(panel ui.Element) {
			ui.Text(c, "弹窗正文")
			if outlineButton(c, "完成").Clicked() {
				open = false
			}
		})
	}, 640, 480)
	if !tt.HasText("弹窗正文") {
		t.Fatalf("dialog content missing from %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, "测试弹窗"); !ok {
		t.Fatalf("dialog role/label missing from %v", tt.Texts())
	}
	click(t, tt, "完成")
	tt.Frame()
	if open {
		t.Fatal("dialog stayed open after its action was clicked")
	}
}

func TestStudioDialogDismissesViaEscapeAndBackdrop(t *testing.T) {
	tests := []struct {
		name     string
		close    func(tt *ui.Tester)
		wantOpen bool
	}{
		{name: "escape", close: func(tt *ui.Tester) { tt.Key(0, ui.KeyEscape) }, wantOpen: false},
		{name: "backdrop", close: func(tt *ui.Tester) { tt.ClickAt(2, 2) }, wantOpen: false},
		{name: "escape disabled", close: func(tt *ui.Tester) { tt.Key(0, ui.KeyEscape) }, wantOpen: true},
		{name: "backdrop disabled", close: func(tt *ui.Tester) { tt.ClickAt(2, 2) }, wantOpen: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			open := true
			options := components.DialogOptions{Label: "测试弹窗"}
			if tc.wantOpen {
				options.DisableBackdropClose = true
				options.DisableEscapeClose = true
			}
			tt := ui.NewTester(func(c *ui.Context) {
				studioDialog(c, &open, options, func(panel ui.Element) {
					ui.Text(c, "弹窗正文")
				})
			}, 640, 480)
			tc.close(tt)
			tt.Frame()
			if open != tc.wantOpen {
				t.Fatalf("dialog open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

func TestStudioDialogPanelIsOpaque(t *testing.T) {
	sample := func(behind ui.Color) ([4]uint32, bool) {
		open := true
		var pixel [4]uint32
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).FillWidth().FillHeight().Background(behind)
			studioDialog(c, &open, components.DialogOptions{Label: "不透明弹窗"}, func(panel ui.Element) {
				ui.Text(c, "弹窗正文")
			})
		}, 640, 480)
		info, ok := tt.AccessByRole(ui.RoleDialog, "不透明弹窗")
		if !ok || info.Bounds.W < 16 || info.Bounds.H < 16 {
			return pixel, false
		}
		img := tt.Image()
		x := int(info.Bounds.X + info.Bounds.W/2)
		y := int(info.Bounds.Y + 8)
		r, g, b, a := img.At(x, y).RGBA()
		return [4]uint32{r, g, b, a}, true
	}
	overRed, ok := sample(ui.Hex("#ff0000"))
	if !ok {
		t.Fatal("dialog panel not found")
	}
	overBlue, ok := sample(ui.Hex("#0000ff"))
	if !ok {
		t.Fatal("dialog panel not found")
	}
	if overRed != overBlue {
		t.Fatalf("dialog panel samples its backdrop (over red %v, over blue %v); it must be opaque", overRed, overBlue)
	}
}

func TestStudioDialogPreservesHostTitlebarDragStrip(t *testing.T) {
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		components.WithDialogWindowDrag(c, 48, func() {
			studioDialog(c, &open, components.DialogOptions{Label: "标题栏拖动区弹窗"}, func(panel ui.Element) {
				ui.Text(c, "弹窗正文")
			})
		})
	}, 640, 480)
	// Clicking inside the 48 DIP host titlebar drag strip must keep the dialog open.
	tt.ClickAt(20, 20)
	tt.Frame()
	if !open {
		t.Fatal("clicking inside host titlebar drag strip dismissed the dialog")
	}
	// Clicking the backdrop below the titlebar must still dismiss the dialog.
	tt.ClickAt(20, 64)
	tt.Frame()
	if open {
		t.Fatal("clicking backdrop below the titlebar did not dismiss the dialog")
	}
}

func TestStudioDialogDestructiveConfirmAndCancel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  components.DialogResult
	}{
		{name: "confirm", label: "删除", want: components.DialogConfirmed},
		{name: "cancel", label: "取消", want: components.DialogCancelled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			open := true
			var result components.DialogResult
			tt := ui.NewTester(func(c *ui.Context) {
				if r := studioDestructiveAlertDialog(c, &open, components.AlertDialogOptions{
					Title:        "删除模型",
					Message:      "此操作不可撤销",
					ConfirmLabel: "删除",
					CancelLabel:  "取消",
				}); r != components.DialogNone {
					result = r
				}
			}, 640, 480)
			if !tt.HasText("此操作不可撤销") {
				t.Fatalf("alert dialog message missing from %v", tt.Texts())
			}
			click(t, tt, tc.label)
			tt.Frame()
			if result != tc.want {
				t.Fatalf("dialog result = %v, want %v", result, tc.want)
			}
			if open {
				t.Fatal("dialog stayed open after it was dismissed")
			}
		})
	}
}

func TestStudioDialogNestedPanelContent(t *testing.T) {
	open := true
	tt := ui.NewTester(func(c *ui.Context) {
		studioDialog(c, &open, components.DialogOptions{Label: "嵌套弹窗"}, func(panel ui.Element) {
			panel.Width(320).MaxWidthPercent(90).Children(func() {
				ui.Text(c, "嵌套内容")
			})
		})
	}, 640, 480)
	if !tt.HasText("嵌套内容") {
		t.Fatalf("nested panel content missing from %v", tt.Texts())
	}
}

func TestResumeDialogUsesStudioDialog(t *testing.T) {
	s := testState()
	bundle := "zh-core"
	s.resumeSource = &core.RunSummary{RunId: "run-1", Path: "/runs/run-1", Bundle: &bundle}
	tt := ui.NewTester(func(c *ui.Context) { s.resumeDialog(c) }, 900, 700)
	if !tt.HasText("续测 run-1") {
		t.Fatalf("resume dialog heading missing from %v", tt.Texts())
	}
	if !tt.HasText("确认续测") {
		t.Fatalf("resume dialog confirm action missing from %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, "续测设置"); !ok {
		t.Fatalf("resume dialog role/label missing from %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.resumeSource != nil {
		t.Fatalf("resume dialog survived Escape, source = %+v", s.resumeSource)
	}
}

func TestExtCheckDialogUsesStudioDialog(t *testing.T) {
	s := testState()
	s.extCheckDialog = extConnCheckDialogState{
		open:  true,
		probe: core.ExternalConnProbe{Status: core.ExclusivityExternal, Port: 8080, Clients: []string{"Cherry Studio (PID 4182)"}},
	}
	tt := ui.NewTester(func(c *ui.Context) { s.extConnCheckDialog(c) }, 900, 700)
	if !tt.HasText("检测到外部连接") {
		t.Fatalf("ext check dialog title missing from %v", tt.Texts())
	}
	if !tt.HasText("Cherry Studio (PID 4182)") {
		t.Fatalf("ext check dialog probe clients missing from %v", tt.Texts())
	}
	if _, ok := tt.AccessByRole(ui.RoleAlertDialog, "检测到外部连接"); !ok {
		t.Fatalf("ext check dialog role/label missing from %v", tt.Texts())
	}
	click(t, tt, "取消")
	tt.Frame()
	if s.extCheckDialog.open {
		t.Fatal("ext check dialog stayed open after cancel")
	}
}

func TestSubpageHeaderRendersTitleAndActionsWithoutRightBack(t *testing.T) {
	s := &viewState{}
	exported := 0
	tt := ui.NewTester(func(c *ui.Context) {
		s.subpageHeader(c, "任务详情", func() {
			if outlineButton(c, "导出").Clicked() {
				exported++
			}
		})
	}, 900, 200)
	for _, label := range []string{"任务详情", "导出"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("%q missing from %v", label, tt.Texts())
		}
	}
	if _, ok := tt.Find("返回"); ok {
		t.Fatalf("subpageHeader must not render a right-aligned back button: %v", tt.Texts())
	}
	click(t, tt, "导出")
	tt.Frame()
	if exported != 1 {
		t.Fatalf("extra action calls = %d, want 1", exported)
	}
}

func TestShellLeftHeaderBackAndHistoryReturn(t *testing.T) {
	s := readyState("benchmarks")
	tt := ui.NewTester(s.view, 1120, 900)
	tt.Frame()

	// 1. Entering Run History from Benchmarks task list exposes left-sidebar back button -> returns to Tasks.
	click(t, tt, "运行历史")
	tt.Frame()
	if s.benchmarksTab != benchmarksTabHistory || s.historyFromFlow {
		t.Fatalf("expected history tab from tasks list, got tab=%d fromFlow=%v", s.benchmarksTab, s.historyFromFlow)
	}
	backList, ok := tt.AccessByRole(ui.RoleButton, "返回测评列表")
	if !ok || backList.Bounds.X+backList.Bounds.W > 205 {
		t.Fatalf("expected left-sidebar back button (<=205px), got ok=%v bounds=%+v", ok, backList.Bounds)
	}
	click(t, tt, "返回测评列表")
	tt.Frame()
	if s.benchmarksTab != benchmarksTabTasks {
		t.Fatalf("expected back from history to restore tasks tab, got %d", s.benchmarksTab)
	}

	// 2. Entering Run History from Benchmark Flow header returns back to Benchmark Flow.
	s.benchFlowOpen = true
	tt.Frame()
	click(t, tt, "运行历史")
	tt.Frame()
	if s.benchmarksTab != benchmarksTabHistory || s.benchFlowOpen || !s.historyFromFlow {
		t.Fatalf("expected history opened from flow: tab=%d flow=%v fromFlow=%v", s.benchmarksTab, s.benchFlowOpen, s.historyFromFlow)
	}
	click(t, tt, "返回任务")
	tt.Frame()
	if !s.benchFlowOpen || s.benchmarksTab != benchmarksTabTasks || s.historyFromFlow {
		t.Fatalf("expected back from flow-opened history to restore flow: flow=%v tab=%d fromFlow=%v", s.benchFlowOpen, s.benchmarksTab, s.historyFromFlow)
	}
}

func TestDiscoveredServicesOmitsLimitationBanners(t *testing.T) {
	s, _ := servicesP2State(t)
	s.discoveryLoaded = true
	s.discovery.Limitations = []string{"process_ownership_unreadable", "process_name_unreadable"}
	tt := newServicesTester(s)
	tt.Frame()
	for _, unwanted := range []string{"部分监听进程不可读", "进程名称不可读"} {
		if tt.HasText(unwanted) {
			t.Fatalf("discovered services should omit limitation banner %q: %v", unwanted, tt.Texts())
		}
	}
}

func TestAddNodeDialogExitTransitionDoesNotPanic(t *testing.T) {
	s := testState()
	s.nodes.addOpen = true
	tt := ui.NewTester(func(c *ui.Context) { s.nodesDialogs(c) }, 900, 700)
	_ = tt.Image()
	tt.Key(0, ui.KeyEscape)
	for range 4 {
		tt.Frame()
		_ = tt.Image()
	}
	if s.nodes.addOpen {
		t.Fatal("add node dialog stayed open after Escape")
	}
}
