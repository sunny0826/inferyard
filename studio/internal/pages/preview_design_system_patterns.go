//go:build ui_debug && !release

package pages

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/core"
)

func dsPatternsSection(c *ui.Context, st *dsShowcaseState) {
	t := tokens(c)
	card(c, "五、设计范式（Design Patterns）", func() {
		kpiOverviewSection(c, kpiOverviewOptions{
			Title: "范式：KPI 总览与证据折叠",
			Items: []kpiItem{
				{Label: "综合吞吐", Value: "84.26 tok/s", Detail: "中位数 · 64K"},
				{Label: "首字延迟", Value: "142.8 ms", Detail: "P95 168.4 ms"},
				{Label: "显存峰值", Value: "缺测", Detail: "当前平台未上报"},
			},
			EvidenceKey: "ds-pattern-kpi-evidence",
			Evidence: func() {
				keyValueRow(c, "证据目录", "/Users/demo/.inferyard/runs/20261121-001", false)
				keyValueRow(c, "采样点数", "600", true)
			},
		})
		section(c, "范式：多条件筛选与可下钻数据表格", func() {
			allRows := []struct {
				id, model, engine, status, tps string
				kind                           stateKind
			}{
				{"run-001", "Qwen2.5-14B-Q4", "llama-server", "complete", "84.26", stateSuccess},
				{"run-002", "DeepSeek-R1-14B", "LM Studio", "partial", "62.10", stateWarning},
				{"run-003", "Llama-3.3-70B", "mlx-lm", "failed", "缺测", stateDanger},
			}
			q := strings.ToLower(strings.TrimSpace(st.tableQuery))
			filtered := allRows[:0:0]
			for _, r := range allRows {
				if st.tableStatus != "all" && r.status != st.tableStatus {
					continue
				}
				if q != "" && !strings.Contains(strings.ToLower(r.model+" "+r.engine), q) {
					continue
				}
				filtered = append(filtered, r)
			}
			filterableTable(c, filterableTableOptions{
				SearchPlaceholder: "搜索模型或引擎",
				Query:             &st.tableQuery,
				HasActiveFilters:  st.tableStatus != "all",
				OnClearFilters: func() {
					st.tableQuery = ""
					st.tableStatus = "all"
				},
				Filters: func() {
					segmentedField(c, "", "", &st.tableStatus, []components.NativeSelectOption[string]{
						{Label: "全部", Value: "all"},
						{Label: "完整", Value: "complete"},
						{Label: "不完整", Value: "partial"},
						{Label: "失败", Value: "failed"},
					})
				},
				TotalCount:    len(allRows),
				FilteredCount: len(filtered),
				Table: func() {
					cols := []ui.TableColumn{
						{Title: "模型", Width: 200},
						{Title: "引擎", Width: 140},
						{Title: "状态", Width: 120},
						{Title: "吞吐 (tok/s)", Width: 120},
					}
					st.tableState.Key = func(i int) any { return filtered[i].id }
					pageTable(c, &st.tableState, "范式示例表格", cols, len(filtered), func(row, col int) {
						item := filtered[row]
						switch col {
						case 0:
							ui.Text(c, item.model).SingleLine()
						case 1:
							ui.Text(c, item.engine).TextColor(t.Color.FgSecondary).SingleLine()
						case 2:
							badge(c, stateLabel(item.status), item.kind, badgeOptions{Style: badgeDot, Size: sizeSM})
						case 3:
							ui.Text(c, item.tps).FontFeatures("tnum").SingleLine()
						}
					}).Height(140)
				},
			})
		})
		section(c, "范式：结构化错误诊断与恢复", func() {
			errorRecoveryBanner(c, &core.ApiError{
				Code:    "conflict",
				Message: "conflict",
				Details: map[string]any{"reason": "engine_active", "pid": 4001},
			}, func() {
				button(c, "刷新状态")
				ghostButton(c, "前往任务页")
			})
		})
		settingsFormSection(c, settingsFormOptions{
			Title:       "范式：分区校验配置表单",
			Description: "展示脏态提示、字段校验与唯一主操作按钮约束。",
			Dirty:       st.formDirty,
			Fields: func() {
				monoField(c, "外部服务端点", "仅接受 http 或 https 地址。", "http://127.0.0.1:1234", urlError(st.formEndpoint), &st.formEndpoint)
			},
			Actions: func() {
				if primaryButton(c, "保存配置").Disabled(urlError(st.formEndpoint) != "").Clicked() {
					st.formDirty = false
				}
				if button(c, "还原").Clicked() {
					st.formEndpoint = "http://127.0.0.1:1234"
					st.formDirty = false
				}
			},
		})
	})
}
