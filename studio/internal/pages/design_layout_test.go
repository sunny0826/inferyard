package pages

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"image/png"
	"inferyard/studio/internal/core"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func designNodes(tt *ui.Tester, visit func(reflect.Value, bool)) {
	root := reflect.ValueOf(tt).Elem().FieldByName("rt").Elem().FieldByName("c").FieldByName("root")
	var walk func(reflect.Value, bool)
	walk = func(p reflect.Value, table bool) {
		if p.IsNil() {
			return
		}
		n := p.Elem()
		visit(n, table)
		table = table || n.FieldByName("widget").String() == "Table"
		for child := n.FieldByName("first"); !child.IsNil(); child = child.Elem().FieldByName("next") {
			walk(child, table)
		}
	}
	walk(root, false)
}

func designFixture(page string) *viewState {
	s := deviceTestState()
	s.selected = page
	long := "/fixture/" + strings.Repeat("long-path-segment/", 12)
	s.settings.Effective.ModelsRoot = ptr(long + "models")
	s.downloads = []core.DownloadTask{{TaskId: "one", Kind: "model", OutName: strings.Repeat("长模型名称", 12), OutDir: long, State: "downloading"}, {TaskId: "two", Kind: "engine", OutName: "engine", State: "failed", Error: &core.ApiError{Code: "download_failed"}}}
	s.tasks = []core.RunTask{{TaskId: "active", State: "cancelling", Bundle: ptr("zh-core"), RunPath: ptr(long)}}
	s.runProgress = &core.RunProgress{Done: 1, Total: ptr(int64(4))}
	for i := range 100 {
		s.history.StandaloneRuns = append(s.history.StandaloneRuns, core.RunSummary{RunId: fmt.Sprintf("run-%03d", i), Path: fmt.Sprintf("%s/%d", long, i), Status: "partial"})
	}
	s.reports = []core.ReportSummary{{ReportId: "report-fixture", Path: long + "report", RunIds: []string{"run-001"}}}
	if page == "detail" {
		s.selected, s.detailID = "dashboard", "active"
		s.detail = &core.RunTaskDetail{Task: s.tasks[0], Progress: &core.RunProgress{Done: 1, Total: ptr(int64(4)), Cases: []core.CaseProgress{{CaseId: "case-fixture", State: "completed"}}}, Resources: []core.ResourceSample{{Utc: "2026-10-14T01:00:00Z", ServiceCpuPercent: ptr(10.0)}}}
	}
	return s
}

func TestDesignLayoutMatrix(t *testing.T) {
	for _, page := range []string{"dashboard", "device", "models", "reports", "settings", "detail"} {
		for _, width := range []int{1120, 800, 1440} {
			for _, scale := range []float32{1, 1.5, 2} {
				for _, dark := range []bool{false, true} {
					for _, contrast := range []bool{false, true} {
						name := fmt.Sprintf("%s/%d/%.1f/dark-%t/contrast-%t", page, width, scale, dark, contrast)
						t.Run(name, func(t *testing.T) {
							s := designFixture(page)
							height := 800
							if width == 800 {
								height = 600
							}
							if width == 1440 {
								height = 900
							}
							tt := ui.NewTester(s.view, width, height)
							tt.SetDark(dark)
							tt.SetPreferences(ui.Preferences{TextScale: scale, HighContrast: contrast, ReduceMotion: true})
							tt.Frame()
							headings := 0
							designNodes(tt, func(n reflect.Value, table bool) {
								if n.FieldByName("role").Uint() == uint64(ui.RoleHeading) && n.FieldByName("level").Int() == 1 {
									headings++
								}
								if table {
									return
								}
								x, w := n.FieldByName("x").Float(), n.FieldByName("w").Float()
								if w > 0 && (x < -.1 || x+w > float64(width)+.1) {
									t.Errorf("horizontal overflow %q (%s): x=%.1f w=%.1f", n.FieldByName("text").String(), n.FieldByName("widget").String(), x, w)
								}
							})
							if headings != 1 {
								t.Errorf("page heading count %d", headings)
							}
							if _, ok := tt.Find("刷新"); ok {
								t.Fatal("global refresh button remains visible")
							}
							s.refreshing = true
							tt.Frame()
							before, ok := tt.Find("正在刷新…")
							if !ok {
								t.Fatal("missing refresh status")
							}
							tt.Scroll(float32(width-80), 400, 0, 600)
							after, ok := tt.Find("正在刷新…")
							if !ok || before != after {
								t.Fatal("toolbar moved during content scroll")
							}
							s.refreshing = false
							tt.Frame()
							if dir := os.Getenv("STUDIO_UI_PREVIEW_DIR"); dir != "" && contrast && ((width == 800 && scale == 2) || (width == 1120 && scale == 1)) {
								tt.Scroll(float32(width-80), 400, 0, -100000)
								path := filepath.Join(dir, strings.ReplaceAll(name, "/", "-")+".png")
								f, err := os.Create(path)
								if err != nil {
									t.Fatal(err)
								}
								err = png.Encode(f, tt.Image())
								f.Close()
								if err != nil {
									t.Fatal(err)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestDesignModelsStackAndLongPath(t *testing.T) {
	s := designFixture("models")
	tt := ui.NewTester(s.view, 1120, 800)
	left, _ := tt.Find("模型")
	right, _ := tt.Find("引擎服务")
	if left.Y != right.Y || right.X <= left.X {
		t.Fatal("wide columns", left, right)
	}
	for _, scale := range []float32{1, 1.5, 2} {
		tt.SetSize(800, 600)
		tt.SetPreferences(ui.Preferences{TextScale: scale, ReduceMotion: true})
		var modelY, downloadY, engineY, bindingY float64
		for _, n := range modelUINodes(tt, "text", "模型") {
			if n.FieldByName("role").Uint() == uint64(ui.RoleHeading) {
				modelY = n.FieldByName("y").Float()
				break
			}
		}
		downloadY = modelUINodes(tt, "text", "下载")[0].FieldByName("y").Float()
		engineY = modelUINodes(tt, "text", "引擎服务")[0].FieldByName("y").Float()
		bindingY = modelUINodes(tt, "text", "绑定存活提示")[0].FieldByName("y").Float()
		if !(modelY < downloadY && downloadY < engineY && engineY < bindingY) {
			t.Fatal("single-column section order", modelY, downloadY, engineY, bindingY)
		}
	}
}
