package pages

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDownloadsCatalogPreservesInitialCustomURL(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	const custom = "https://example.invalid/custom.gguf"
	s.modelURL = custom
	tt := ui.NewTester(s.view, 1120, 3000)
	tt.Frame()
	if s.modelURL != custom || !tt.HasText("自定义地址") {
		t.Fatalf("opening Downloads replaced the user's URL: %s", s.modelURL)
	}
}

func TestDownloadsCatalogDefaultsAndProgressiveDetails(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	for _, want := range []string{"推荐资源 · " + core.PlatformLabel(core.CurrentPlatform()), "模型", "引擎", "100.57 MiB", "609.82 MiB", "其他准备方式"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	if s.engineURL != core.DefaultEngineURL() || s.modelURL != core.DefaultModelURL() {
		t.Fatal("wrong platform defaults", s.engineURL, s.modelURL)
	}
	model, _ := core.CatalogEntryByURL(s.modelURL)
	if tt.HasText(model.SHA256) || tt.HasText(model.URL) {
		t.Fatal("technical details dominate the default view")
	}
	click(t, tt, "版本与校验 · "+model.Title)
	if !tt.HasText(model.SHA256) || !tt.HasText(model.Revision) {
		t.Fatal(tt.Texts())
	}
	click(t, tt, "自定义模型地址")
	if !tt.HasText("模型 URL（固定 40 位 revision 的 GGUF）") {
		t.Fatal(tt.Texts())
	}
}

func TestDownloadsCatalogPlatformChoices(t *testing.T) {
	cases := []struct {
		platform, engine string
		mlx              bool
	}{
		{"darwin/arm64", "engine-macos-arm64", true},
		{"darwin/amd64", "engine-macos-x64", false},
		{"windows/amd64", "engine-windows-x64", false},
		{"linux/amd64", "engine-linux-x64", false},
		{"linux/arm64", "engine-linux-arm64", false},
		{"windows/arm64", "", false},
		{"unknown/unknown", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			s := testState()
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Children(func() {
					s.catalogChoices(c, core.CatalogEngine, tc.platform)
					s.catalogChoices(c, core.CatalogModel, tc.platform)
					catalogReferences(c, tc.platform)
				})
			}, 1120, 3000)
			for _, entry := range core.Catalog() {
				if !entry.Downloadable() {
					continue
				}
				_, shown := tt.Find("选择 · " + entry.DisplayName())
				want := entry.Platform == "any" || entry.Platform == tc.platform
				if shown != want {
					t.Fatalf("%s: shown=%v want=%v", entry.ID, shown, want)
				}
			}
			if tc.engine != "" {
				entry, _ := core.CatalogEntryByID(tc.engine)
				click(t, tt, "选择 · "+entry.DisplayName())
				if s.engineURL != entry.URL {
					t.Fatal("platform engine selection failed")
				}
			} else if !tt.HasText("当前平台没有可直接下载的推荐资源") {
				t.Fatal("unsupported platform has no explanation")
			}
			mlx, _ := core.CatalogEntryByID("model-qwen3-0.6b-mlx")
			if tt.HasText(mlx.Title) {
				t.Fatal("reference section should start collapsed")
			}
			click(t, tt, "其他准备方式")
			if tt.HasText(mlx.Title) != tc.mlx {
				t.Fatal("MLX reference leaked across platforms", tt.Texts())
			}
			for _, entry := range core.CatalogForPlatform(tc.platform) {
				if !entry.Downloadable() && (!tt.HasText(entry.Reason) || !tt.HasText("准备说明："+entry.Doc)) {
					t.Fatal("reference lost preparation instructions", entry.ID)
				}
			}
			if tt.HasText("填入下载地址") {
				t.Fatal("reference must not submit an unsupported download")
			}
		})
	}
}

func TestDownloadsCatalogSelectionAndManualEdit(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	entry, _ := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	click(t, tt, "选择 · "+entry.DisplayName())
	if s.modelURL != entry.URL {
		t.Fatal("choice did not fill the download form")
	}
	if _, ok := tt.Find("已选择 · " + entry.DisplayName()); !ok {
		t.Fatal("selection is not visible")
	}
	s.modelURL = "https://example.invalid/other.gguf"
	tt.Frame()
	if s.modelURL != "https://example.invalid/other.gguf" {
		t.Fatal("render overwrote custom URL")
	}
	if _, ok := tt.Find("已选择 · " + entry.DisplayName()); ok {
		t.Fatal("stale selected marker")
	}
	click(t, tt, "选择 · "+entry.DisplayName())
	if s.modelURL != entry.URL {
		t.Fatal("cannot reselect after manual edit")
	}
}

func TestDownloadsCatalogWarnsForCustomForeignAsset(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	for _, entry := range core.Catalog() {
		if entry.Downloadable() && !entry.RunsOnCurrentPlatform() {
			s.engineURL = entry.URL
			tt := ui.NewTester(s.view, 1120, 3000)
			if s.engineURL != entry.URL || !tt.HasText("不能在本机运行") {
				t.Fatal(tt.Texts())
			}
			return
		}
	}
	t.Fatal("missing foreign asset fixture")
}

func TestCatalogSizeRendering(t *testing.T) {
	cases := map[int64]string{
		0:         "大小未知",
		512:       "512 bytes",
		2048:      "2.0 KiB（2048 bytes）",
		11517506:  "10.98 MiB（11517506 bytes）",
		639446688: "609.82 MiB（639446688 bytes）",
	}
	for bytes, want := range cases {
		if got := catalogSize(bytes); got != want {
			t.Fatalf("catalogSize(%d) = %q, want %q", bytes, got, want)
		}
	}
}

func TestCatalogDisplayNameSeparatesEntriesThatShareATitle(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range core.Catalog() {
		name := entry.DisplayName()
		if seen[name] {
			t.Fatal("two entries share a display name", entry.ID, name)
		}
		seen[name] = true
		if entry.SharesTitle() && !strings.Contains(name, core.PlatformLabel(entry.Platform)) {
			t.Fatal("an ambiguous title does not name its platform", entry.ID, name)
		}
	}
}
