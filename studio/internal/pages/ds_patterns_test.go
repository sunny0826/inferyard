package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func TestDSFilterableTableStatesAndClear(t *testing.T) {
	query := "not-found"
	status := "failed"
	cleared := false
	total := 3
	filtered := 0
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, false))
		filterableTable(c, filterableTableOptions{
			SearchPlaceholder: "搜索模型",
			Query:             &query,
			HasActiveFilters:  status != "all",
			OnClearFilters: func() {
				query = ""
				status = "all"
				filtered = 3
				cleared = true
			},
			TotalCount:    total,
			FilteredCount: filtered,
			Table: func() {
				ui.Text(c, "表格内容行")
			},
		})
	}, 900, 600)
	tt.Frame()
	if !tt.HasText("无匹配记录") || tt.HasText("表格内容行") {
		t.Fatalf("expected empty filtered state, got: %v", tt.Texts())
	}
	click(t, tt, "清空筛选")
	if !cleared || query != "" || status != "all" {
		t.Fatalf("clear filters did not reset state: cleared=%v query=%q status=%q", cleared, query, status)
	}
	tt.Frame()
	if !tt.HasText("表格内容行") || !tt.HasText("共 3 条记录") {
		t.Fatalf("table did not recover after clearing filters: %v", tt.Texts())
	}
}

func TestDSKPIOverviewAndErrorRecovery(t *testing.T) {
	retried := false
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, true))
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			kpiOverviewSection(c, kpiOverviewOptions{
				Title: "性能总览",
				Items: []kpiItem{
					{Label: "吞吐", Value: "84.26 tok/s", Detail: "中位数"},
					{Label: "温度", Value: "缺测", Detail: "未上报"},
				},
				EvidenceKey: "test-kpi-ev",
				Evidence: func() {
					keyValueRow(c, "采样点", "600", true)
				},
			})
			errorRecoveryBanner(c, &core.ApiError{
				Code:    "conflict",
				Message: "conflict",
				Details: map[string]any{"reason": "engine_active"},
			}, func() {
				button(c, "刷新状态").OnClick(func() { retried = true })
			})
		})
	}, 1000, 800)
	tt.Frame()
	for _, want := range []string{"性能总览", "84.26 tok/s", "缺测", "口径说明与观测依据", "已有任务或服务在运行"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	click(t, tt, "刷新状态")
	if !retried {
		t.Fatal("errorRecoveryBanner action did not fire")
	}
}

func TestDSSettingsFormSection(t *testing.T) {
	dirty := true
	saved := false
	endpoint := "http://127.0.0.1:1234"
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, false))
		settingsFormSection(c, settingsFormOptions{
			Title:       "服务设置",
			Description: "配置本地推理服务连接参数。",
			Dirty:       dirty,
			Fields: func() {
				monoField(c, "端点地址", "", "http://127.0.0.1:1234", urlError(endpoint), &endpoint)
			},
			Actions: func() {
				primaryButton(c, "保存设置").OnClick(func() {
					saved = true
					dirty = false
				})
			},
		})
	}, 900, 600)
	tt.Frame()
	if !tt.HasText("有未保存修改") {
		t.Fatalf("dirty badge not rendered: %v", tt.Texts())
	}
	click(t, tt, "保存设置")
	if !saved || dirty {
		t.Fatalf("settings form save failed: saved=%v dirty=%v", saved, dirty)
	}
	tt.Frame()
	if tt.HasText("有未保存修改") {
		t.Fatalf("dirty badge remained after save: %v", tt.Texts())
	}
}

func TestDSFilterToolbarVerticalAlignment(t *testing.T) {
	query := ""
	status := "all"
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, false))
		filterableTable(c, filterableTableOptions{
			SearchPlaceholder: "搜索模型或引擎",
			Query:             &query,
			Filters: func() {
				segmentedControl(c, "状态筛选", &status, []components.NativeSelectOption[string]{
					{Label: "全部", Value: "all"},
					{Label: "完整", Value: "complete"},
				})
			},
			TotalCount:    1,
			FilteredCount: 1,
			Table:         func() { ui.Text(c, "行 1") },
		})
	}, 900, 400)
	tt.Frame()
	searchRect, ok := tt.Find("搜索模型或引擎")
	if !ok {
		t.Fatal("search input not found")
	}
	segRect, ok := tt.Find("状态筛选")
	if !ok {
		t.Fatal("segmented control not found")
	}
	searchMid := searchRect.Y + searchRect.H/2
	segMid := segRect.Y + segRect.H/2
	if diff := searchMid - segMid; diff < -1.5 || diff > 1.5 {
		t.Fatalf("toolbar controls misaligned: search mid=%.1f (rect=%v), segmented mid=%.1f (rect=%v)", searchMid, searchRect, segMid, segRect)
	}
}
