//go:build ui_debug && !release

package pages

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"github.com/oneo-me/onevra/source/shells/sidebar"
)

type dsShowcaseState struct {
	segment   string
	selected  string
	switchOn  bool
	checked   bool
	radio     string
	inputText string
}

func (p *previewSession) designSystemView(c *ui.Context) {
	t := tokens(c)
	ui.Column(c).Grow(1).MinWidth(0).Fill().Padding(sidebar.HeaderHeight(c), 0, 0, 0).Children(func() {
		ui.Scroll(c.Key("design-system-scroll")).Grow(1).Padding(t.Density.Page).Children(func() {
			root := ui.Column(c).Gap(t.Density.Section)
			st := ui.Local(root, "ds-showcase", func() dsShowcaseState {
				return dsShowcaseState{
					segment:   "64K",
					selected:  "Qwen2.5-14B-Instruct-Q4_K_M.gguf",
					switchOn:  true,
					checked:   true,
					radio:     "local",
					inputText: "INFERYARD_API_KEY",
				}
			})
			root.Children(func() {
				ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
					heading(c, "InferYard Studio 设计系统", 1)
					modeLabel := "浅色模式"
					if c.Theme().Dark {
						modeLabel = "柔和深灰模式"
					}
					statusBadge(c, modeLabel, stateColor(c, stateInfo))
					statusBadge(c, "4px 栅格 · 圆润连续圆角", stateColor(c, stateNeutral))
				})
				dsColorSection(c)
				dsMaterialAndRadiusSection(c)
				dsTypographySection(c)
				dsComponentsSection(c, st)
			})
		})
	})
}

func dsColorSection(c *ui.Context) {
	t := tokens(c)
	card(c, "一、色彩体系（纯净中性灰 + 极简石墨单色 + 方案 A 功能色）", func() {
		section(c, "中性灰表面与描边", func() {
			swatches := []struct {
				name  string
				color ui.Color
			}{
				{"Canvas", t.Color.Canvas},
				{"Sunken", t.Color.Sunken},
				{"Surface", t.Color.Surface},
				{"Hover", t.Color.SurfaceHover},
				{"Pressed", t.Color.SurfacePressed},
				{"Elevated", t.Color.Elevated},
				{"Border Subtle", t.Color.BorderSubtle},
				{"Border Default", t.Color.BorderDefault},
				{"Border Strong", t.Color.BorderStrong},
			}
			dsSwatchRow(c, swatches)
		})
		section(c, "单色交互与功能状态", func() {
			swatches := []struct {
				name  string
				color ui.Color
			}{
				{"Accent Solid", t.Color.Accent},
				{"Accent Hover", t.Color.AccentHover},
				{"Info / Active", t.Color.Info.Solid},
				{"Success", t.Color.Success.Solid},
				{"Warning", t.Color.Warning.Solid},
				{"Danger", t.Color.Danger.Solid},
			}
			dsSwatchRow(c, swatches)
		})
		section(c, "数据可视化色板（Chart[6]）", func() {
			swatches := make([]struct {
				name  string
				color ui.Color
			}, len(t.Color.Chart))
			for i, col := range t.Color.Chart {
				swatches[i] = struct {
					name  string
					color ui.Color
				}{fmt.Sprintf("Chart %d", i+1), col}
			}
			dsSwatchRow(c, swatches)
		})
	})
}

func dsSwatchRow(c *ui.Context, items []struct {
	name  string
	color ui.Color
}) {
	t := tokens(c)
	ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
		for _, item := range items {
			ui.Column(c).Width(108).Gap(t.Density.Tight).Children(func() {
				ui.Box(c).FillWidth().Height(36).Radius(t.Radius.MD).Background(item.color).Border(1, t.Color.BorderDefault)
				ui.Text(c, item.name).FontSize(t.Type.Caption).SingleLine()
				ui.Text(c, fmt.Sprintf("#%02X%02X%02X", item.color.R, item.color.G, item.color.B)).
					FontSize(t.Type.ShellDetail).TextColor(t.Color.FgSecondary).FontFeatures("tnum").Selectable()
			})
		}
	})
}

func dsMaterialAndRadiusSection(c *ui.Context) {
	t := tokens(c)
	card(c, "二、样式与材质体系（4px 间距 · 圆润柔和连续圆角 · 轻投影 + 细边框）", func() {
		section(c, "7 种 Surface 材质对比", func() {
			cols := 3
			if compactLayout(c) {
				cols = 1
			}
			ui.Grid(c).Columns(cols).Gap(t.Density.Stack).Children(func() {
				materials := []struct {
					name, spec string
					kind       materialKind
				}{
					{"Sunken", "R=12 · 无投影 · 弱描边", materialSunken},
					{"Card", "R=16 · 轻投影 XS · 默认描边", materialCard},
					{"Inset", "R=12 · 无投影 · 弱描边", materialInset},
					{"Control", "R=8 · 轻投影 XS · 默认描边", materialControl},
					{"Floating", "R=12 · 中投影 MD · 浮层面", materialFloating},
					{"Modal", "R=20 · 强投影 LG · 浮层面", materialModal},
				}
				for _, m := range materials {
					applyMaterial(c, ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Stack), m.kind).Children(func() {
						ui.Text(c, m.name).Bold().FontSize(t.Type.Section)
						ui.Text(c, m.spec).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					})
				}
			})
		})
		section(c, "等距内缩连续圆角演示（外层 Card R=16 → 嵌套 Inset R=12 → 控件 R=8）", func() {
			applyMaterial(c, ui.Column(c).Gap(t.Density.Row).Padding(t.Density.Stack), materialInset).Children(func() {
				ui.Text(c, "嵌套容器（Inset · R=12, P=12）").Bold().FontSize(t.Type.Body)
				ui.Row(c).Wrap().Gap(t.Density.Row).Children(func() {
					applyMaterial(c, ui.Box(c).Padding(t.Density.Row, t.Density.Stack), materialControl).Children(func() {
						ui.Text(c, "内嵌控件（Control · R=8）").FontSize(t.Type.Caption)
					})
					ui.Box(c).Padding(t.Density.Tight, t.Density.Row).Radius(concentricRadius(t.Radius.LG, t.Density.Tight)).
						Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
						ui.Text(c, "concentric(12, 4) = 8px").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					})
				})
			})
		})
	})
}

func dsTypographySection(c *ui.Context) {
	t := tokens(c)
	card(c, "三、排版字阶与等宽数字（tnum）", func() {
		cols := 3
		if compactLayout(c) {
			cols = 1
		}
		ui.Grid(c).Columns(cols).Gap(t.Density.Stack).Children(func() {
			metricCard(c, "吞吐速度（tnum）", "84.26 tok/s", "中位数 · 64K 上下文", "样本数 12")
			metricCard(c, "首字延迟 TTFT", "142.8 ms", "P95 168.4 ms", "")
			metricCard(c, "缺测指标示例", "缺测", "缺测不补 0", "")
		})
	})
}

func dsComponentsSection(c *ui.Context, st *dsShowcaseState) {
	t := tokens(c)
	card(c, "四、基础组件与状态语义映射", func() {
		section(c, "按钮层级（石墨单色 Primary + 克制 Secondary + 绯红 Danger）", func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				primaryButton(c, "开始测评")
				button(c, "刷新状态")
				dangerButton(c, "停止服务")
				primaryActionButton(c, "核验中", true)
			})
		})
		section(c, "状态徽章与内联通知（方案 A：钢蓝运行态 + 3 态结果色）", func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				statusBadge(c, "排队中", stateColor(c, stateNeutral))
				statusBadge(c, "运行中", stateColor(c, stateActive))
				statusBadge(c, "提示", stateColor(c, stateInfo))
				statusBadge(c, "完整收尾", stateColor(c, stateSuccess))
				statusBadge(c, "运行不完整", stateColor(c, stateWarning))
				statusBadge(c, "工具错误", stateColor(c, stateDanger))
			})
			stateBanner(c, stateInfo, lucide.Info, func(fg ui.Color) {
				ui.Text(c, "信息提示（Info）：当前使用本地发现的服务端点，仅保存环境变量名。").TextColor(fg)
			})
			stateBanner(c, stateSuccess, lucide.CircleCheck, func(fg ui.Color) {
				ui.Text(c, "成功（Success）：所有题目已完成并落盘原始证据。").TextColor(fg)
			})
			stateBanner(c, stateWarning, lucide.TriangleAlert, func(fg ui.Color) {
				ui.Text(c, "告警（Warning）：存在 2 项缺测采样，报告将保持缺测标记。").TextColor(fg)
			})
			stateBanner(c, stateDanger, lucide.CircleAlert, func(fg ui.Color) {
				ui.Text(c, "错误（Danger）：服务连接已断开 · engine_unreachable").TextColor(fg)
			})
		})
		section(c, "表单与选择控件", func() {
			segmentedField(c, "上下文档位（Segmented · R=12 → 8）", "", &st.segment, []components.NativeSelectOption[string]{
				{Label: "16K", Value: "16K"},
				{Label: "32K", Value: "32K"},
				{Label: "64K", Value: "64K"},
				{Label: "128K", Value: "128K"},
			})
			searchableSelectField(c, "模型选择（SearchableCombobox）", "", "搜索本地模型", "未找到 %q", &st.selected, []string{
				"Qwen2.5-14B-Instruct-Q4_K_M.gguf",
				"Qwen2.5-32B-Instruct-Q4_K_M.gguf",
				"DeepSeek-R1-Distill-Qwen-14B-Q4_K_M.gguf",
				"Llama-3.3-70B-Instruct-Q4_K_M.gguf",
			})
			field(c, "凭据环境变量（api_key_env）", &st.inputText)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Section).Children(func() {
				components.Switch(c, &st.switchOn, "自动采样资源")
				components.Checkbox(c, &st.checked, "保留原始输出目录")
				components.Radio(c, &st.radio, "local", "本地引擎")
				components.Radio(c, &st.radio, "external", "外部服务")
			})
			componentFoldKeyed(c, "ds-fold-demo", "口径说明与观测依据（Collapsible 折叠区）", func() {
				ui.Text(c, "次要说明与原始路径统一收进折叠区，避免主界面堆叠防御性文案。").TextColor(t.Color.FgSecondary)
			})
		})
	})
}
