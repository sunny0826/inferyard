package pages

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func TestDSBadgesAndNoticeBanners(t *testing.T) {
	retried := false
	for _, tc := range []struct {
		name         string
		dark         bool
		highContrast bool
	}{
		{"light", false, false},
		{"dark", true, false},
		{"high-contrast", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retried = false
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(studioThemeFor(c, tc.dark))
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					badge(c, "运行中", stateActive, badgeOptions{Style: badgeSoft})
					badge(c, "已通过", stateSuccess, badgeOptions{Style: badgeOutline, Size: sizeSM})
					badge(c, "告警", stateWarning, badgeOptions{Style: badgeDot})
					noticeBanner(c, noticeBannerOptions{
						Kind:    stateDanger,
						Title:   "连接失败",
						Message: "无法访问服务端点",
						Actions: func() {
							button(c, "重试探测").OnClick(func() { retried = true })
						},
					})
					infoBanner(c, "仅保存环境变量名")
				})
			}, 800, 600)
			if tc.highContrast {
				tt.SetPreferences(ui.Preferences{HighContrast: true})
			}
			tt.Frame()
			for _, want := range []string{"运行中", "已通过", "告警", "连接失败", "仅保存环境变量名"} {
				if !tt.HasText(want) {
					t.Fatalf("missing %q in %v", want, tt.Texts())
				}
			}
			click(t, tt, "重试探测")
			if !retried {
				t.Fatal("banner action button did not fire")
			}
		})
	}
}

func TestDSFormFieldsAndSwitch(t *testing.T) {
	enabled := false
	env := "bad-value"
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, false))
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			monoField(c, "凭据变量", "仅接受环境变量名", "INFERYARD_API_KEY", tokenNameError(env), &env)
			switchField(c, "自动采样", "即时生效", &enabled)
			outlineButton(c, "次要描边")
			ghostButton(c, "清空")
			iconButton(c, lucide.RefreshCw, "刷新", components.IconButtonPlain)
			sizedButton(c, components.ButtonPrimary, sizeSM, "小按钮")
			sizedButton(c, components.ButtonPrimary, sizeLG, "大按钮")
		})
	}, 800, 600)
	tt.Frame()
	if !tt.HasText("只接受环境变量名，请勿输入明文 token。") {
		t.Fatalf("validation error not shown: %v", tt.Texts())
	}
	sw, ok := tt.AccessByRole(ui.RoleSwitch, "自动采样")
	if !ok {
		t.Fatal("expected accessible switch 自动采样")
	}
	tt.ClickAt(sw.Bounds.X+sw.Bounds.W/2, sw.Bounds.Y+sw.Bounds.H/2)
	tt.Frame()
	if !enabled {
		t.Fatal("switchField did not toggle state")
	}
}

func TestDSListRowKeyValueAndEmptyState(t *testing.T) {
	selected := 0
	created := false
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(studioThemeFor(c, true))
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			listRow(c, listRowOptions{
				Title:    "模型 A",
				Subtitle: "llama-server",
				Selected: selected == 0,
				OnClick:  func() { selected = 0 },
			})
			listRow(c, listRowOptions{
				Title:    "模型 B",
				Subtitle: "mlx-lm",
				Selected: selected == 1,
				OnClick:  func() { selected = 1 },
			})
			keyValueRow(c, "吞吐速度", "84.26 tok/s", true)
			keyValueRow(c, "温度峰值", "", true)
			emptyState(c, emptyStateOptions{
				Icon:        lucide.FileSearch,
				Title:       "暂无报告",
				Description: "请先完成一次测评",
				Surface:     emptyCard,
				Action: func() {
					primaryButton(c, "新建任务").OnClick(func() { created = true })
				},
			})
		})
	}, 800, 700)
	tt.Frame()
	if !tt.HasText("缺测") || !tt.HasText("84.26 tok/s") || !tt.HasText("暂无报告") {
		t.Fatalf("missing expected texts: %v", tt.Texts())
	}
	click(t, tt, "模型 B")
	if selected != 1 {
		t.Fatalf("listRow selection = %d, want 1", selected)
	}
	click(t, tt, "新建任务")
	if !created {
		t.Fatal("emptyState action button did not fire")
	}
}
