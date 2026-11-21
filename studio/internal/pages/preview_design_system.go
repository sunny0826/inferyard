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
	segment     string
	selected    string
	switchOn    bool
	checked     bool
	radio       string
	inputText   string
	badEnv      string
	selectedRow int
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
					badEnv:    "sk-plaintext-invalid",
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
					{"Sunken", "R=8 · 无投影 · 弱描边", materialSunken},
					{"Card", "R=20 · 轻投影 XS · 默认描边", materialCard},
					{"Inset", "R=8 · 同心 (20-12) · 弱描边", materialInset},
					{"Control", "R=8 · 同心 (12-4) · 默认描边", materialControl},
					{"Floating", "R=12 · 中投影 MD · 浮层面", materialFloating},
					{"Modal", "R=24 · 强投影 LG · 浮层面", materialModal},
				}
				for _, m := range materials {
					applyMaterial(c, ui.Column(c).Gap(t.Density.Tight).Padding(t.Density.Stack), m.kind).Children(func() {
						ui.Text(c, m.name).Bold().FontSize(t.Type.Section)
						ui.Text(c, m.spec).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
					})
				}
			})
		})
		section(c, "等距内缩同心圆角演示（Card 20 - P12 = Inset 8；Inset 8 - P4 = Item 4）", func() {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Row).Padding(t.Density.Tight), materialInset).Children(func() {
				ui.Box(c).FillWidth().Padding(t.Density.Row, t.Density.Stack).
					Radius(concentricRadius(t.Radius.Inset, t.Density.Tight)).
					Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).Children(func() {
					ui.Text(c, "内嵌层（Inset R=8, P=4 → 子项 R=4，与外层 Card R=20, P=12 严格同心）").
						FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
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
		section(c, "按钮层级与尺寸（Primary / Secondary / Outline / Ghost / Danger / IconButton）", func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				primaryButton(c, "开始测评")
				button(c, "刷新状态")
				outlineButton(c, "次要描边")
				ghostButton(c, "清空筛选")
				dangerButton(c, "停止服务")
				primaryActionButton(c, "核验中", true)
				iconButton(c, lucide.RefreshCw, "刷新列表", components.IconButtonPlain)
				iconButton(c, lucide.Copy, "复制路径", components.IconButtonCompact)
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				sizedButton(c, components.ButtonPrimary, sizeSM, "SM 按钮")
				sizedButton(c, components.ButtonPrimary, sizeMD, "MD 按钮")
				sizedButton(c, components.ButtonPrimary, sizeLG, "LG 按钮")
			})
		})
		section(c, "状态徽章（Soft / Outline / Dot × SM / MD）与内联通知", func() {
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				badge(c, "排队中", stateNeutral, badgeOptions{Style: badgeSoft})
				badge(c, "运行中", stateActive, badgeOptions{Style: badgeSoft})
				badge(c, "完整收尾", stateSuccess, badgeOptions{Style: badgeSoft})
				badge(c, "运行不完整", stateWarning, badgeOptions{Style: badgeSoft})
				badge(c, "工具错误", stateDanger, badgeOptions{Style: badgeSoft})
			})
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
				badge(c, "Outline · 信息", stateInfo, badgeOptions{Style: badgeOutline, Size: sizeSM})
				badge(c, "Outline · 通过", stateSuccess, badgeOptions{Style: badgeOutline, Size: sizeSM})
				badge(c, "Dot · 运行中", stateActive, badgeOptions{Style: badgeDot})
				badge(c, "Dot · 就绪", stateSuccess, badgeOptions{Style: badgeDot})
				badge(c, "Dot · 告警", stateWarning, badgeOptions{Style: badgeDot})
				badge(c, "Dot · 失败", stateDanger, badgeOptions{Style: badgeDot})
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateInfo,
				Size:    bannerCompact,
				Message: "Compact Info：当前使用本地发现的服务端点，仅保存环境变量名。",
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateSuccess,
				Title:   "测评已完成",
				Message: "所有题目已结束并落盘原始证据目录。",
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Message: "告警（Warning）：存在 2 项缺测采样，报告将保持缺测标记。",
			})
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateDanger,
				Title:   "服务连接已断开 · engine_unreachable",
				Message: "请检查本地推理服务是否仍在运行，确认后重新绑定。",
				Actions: func() {
					button(c, "重新探测")
					ghostButton(c, "前往设置")
				},
				Details: func() {
					componentFoldKeyed(c, "ds-banner-details", "错误详情", func() {
						keyValueRow(c, "endpoint", "http://127.0.0.1:1234", true)
						keyValueRow(c, "pid", "4001", true)
					})
				},
			})
			ui.Column(c).FillWidth().Gap(t.Density.Row).Children(func() {
				ui.Text(c, "进度条（进行中 · 钢蓝 62%）").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				progressBar(c, 0.62, "进行中进度")
				ui.Text(c, "进度条（已完成 · 松绿 100%）").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				progressBar(c, 1.0, "已完成进度")
				ui.Text(c, "进度条（总量未知 · 不确定脉冲）").FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary)
				progressBar(c, -1, "不确定进度")
			})
		})
		section(c, "表单与选择控件（含字段校验与开关行）", func() {
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
			monoField(c, "凭据环境变量（正常态）", "仅保存环境变量名，不落盘明文密钥。", "例如 INFERYARD_API_KEY", tokenNameError(st.inputText), &st.inputText)
			monoField(c, "凭据环境变量（校验失败态）", "", "例如 INFERYARD_API_KEY", tokenNameError(st.badEnv), &st.badEnv)
			switchField(c, "自动采样系统资源", "测评期间按固定频率采集内存与温度曲线，即时生效。", &st.switchOn)
			ui.Row(c).Wrap().AlignItems(ui.Center).Gap(t.Density.Section).Children(func() {
				components.Checkbox(c, &st.checked, "保留原始输出目录")
				components.Radio(c, &st.radio, "local", "本地引擎")
				components.Radio(c, &st.radio, "external", "外部服务")
			})
			componentFoldKeyed(c, "ds-fold-demo", "口径说明与观测依据（Collapsible 折叠区）", func() {
				ui.Text(c, "次要说明与原始路径统一收进折叠区，避免主界面堆叠防御性文案。").TextColor(t.Color.FgSecondary)
			})
		})
		section(c, "列表行、键值对与空态组件", func() {
			applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight).Padding(t.Density.Tight), materialInset).Children(func() {
				rows := []struct {
					title, sub, status string
					kind               stateKind
				}{
					{"Qwen2.5-14B-Instruct-Q4_K_M.gguf", "llama-server · 64K 上下文", "运行中", stateActive},
					{"DeepSeek-R1-Distill-Qwen-14B.gguf", "LM Studio · 常规题集", "完整收尾", stateSuccess},
					{"Llama-3.3-70B-Instruct.gguf", "mlx-lm · 显存采样缺测", "运行不完整", stateWarning},
				}
				for i, item := range rows {
					idx := i
					listRow(c, listRowOptions{
						Title:    item.title,
						Subtitle: item.sub,
						Selected: st.selectedRow == idx,
						Trailing: func() { badge(c, item.status, item.kind, badgeOptions{Style: badgeDot, Size: sizeSM}) },
						OnClick:  func() { st.selectedRow = idx },
					})
				}
			})
			applyMaterial(c, ui.Column(c).FillWidth().Padding(t.Density.Stack), materialInset).Children(func() {
				keyValueRow(c, "运行目录", "/Users/demo/.inferyard/runs/20261121-001", false)
				keyValueRow(c, "吞吐中位数", "84.26 tok/s", true)
				keyValueRow(c, "GPU 温度峰值", "", true)
			})
			emptyState(c, emptyStateOptions{
				Icon:        lucide.FileSearch,
				Title:       "暂无匹配的测评报告",
				Description: "当前筛选条件下没有记录，可清空筛选或发起新测评。",
				Surface:     emptySunken,
				Action: func() {
					primaryButton(c, "新建测评任务")
					ghostButton(c, "清空筛选")
				},
			})
		})
	})
}
