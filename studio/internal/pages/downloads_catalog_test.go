package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDownloadsCatalogOffersEveryBuiltInAddress(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	for _, want := range []string{"内置地址", "引擎地址", "模型地址", "其他内置地址（下载通道取不到，需自行准备）"} {
		if !tt.HasText(want) {
			t.Fatal(want, tt.Texts())
		}
	}
	engineID := core.CatalogEngineForCurrentPlatform()
	if engineID == "" {
		t.Skip("no engine build for this platform")
	}
	entry, _ := core.CatalogEntryByID(engineID)
	if !tt.HasText(entry.Title) {
		t.Fatal("default engine entry missing", tt.Texts())
	}
	if s.engineURL != entry.URL {
		t.Fatal("engine url is not the catalog entry", s.engineURL)
	}
	if s.modelURL != core.DefaultModelURL() {
		t.Fatal("model url is not the catalog entry", s.modelURL)
	}
}

func TestDownloadsCatalogReferenceEntriesAreNeverPrefilled(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	for _, entry := range core.Catalog() {
		if entry.Downloadable() {
			continue
		}
		if !tt.HasText(entry.Title) {
			t.Fatal("reference entry not shown", entry.ID, tt.Texts())
		}
		if !tt.HasText(entry.Reason) {
			t.Fatal("reference entry without a reason", entry.ID)
		}
		if entry.Doc != "" && !tt.HasText("准备说明："+entry.Doc) {
			t.Fatal("reference entry without a preparation document", entry.ID)
		}
		if s.engineURL == entry.URL || s.modelURL == entry.URL {
			t.Fatal("reference entry was prefilled", entry.ID)
		}
	}
}

func TestDownloadsCatalogReferenceEntryCanBeChosen(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	entry, ok := core.CatalogEntryByID("engine-mlx-lm")
	if !ok {
		t.Fatal("reference engine entry missing")
	}
	if entry.Downloadable() {
		t.Fatal("the entry under test is no longer a reference entry")
	}
	click(t, tt, "填入下载地址 · "+entry.DisplayName())
	tt.Frame()
	if s.engineURL != entry.URL {
		t.Fatal("the reference address did not reach the download form", s.engineURL)
	}
	// The picker keeps its own choice: a reference entry is not one of its
	// options, so the trigger must not show a raw id.
	if s.catalog.engineID == entry.ID {
		t.Fatal("the picker was pointed at an entry it does not offer", entry.ID)
	}
}

func TestDownloadsCatalogMarksEntriesForAnotherPlatform(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	other := ""
	for _, entry := range core.Catalog() {
		if entry.Downloadable() && !entry.RunsOnCurrentPlatform() {
			other = entry.ID
			break
		}
	}
	if other == "" {
		t.Skip("no entry for another platform")
	}
	entry, _ := core.CatalogEntryByID(other)
	click(t, tt, "内置引擎")
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	click(t, tt, entry.DisplayName())
	tt.Frame()
	if !tt.HasText("该地址对应的产物不能在当前机器上运行") {
		t.Fatal("the foreign-platform entry is not marked", tt.Texts())
	}
}

func TestDownloadsCatalogPickerFillsTheDownloadForm(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	entry, ok := core.CatalogEntryByID("model-qwen3-0.6b-q8")
	if !ok {
		t.Fatal("model entry missing")
	}
	current, _ := core.CatalogEntryByID("model-smollm2-135m")
	click(t, tt, current.DisplayName())
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	click(t, tt, entry.DisplayName())
	tt.Frame()
	if s.catalog.modelID != entry.ID {
		t.Fatal("picker did not record the choice", s.catalog.modelID)
	}
	if s.modelURL != entry.URL {
		t.Fatal("picker did not fill the download form", s.modelURL)
	}
}

func TestDownloadsCatalogKeepsAHandEditedURL(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	s.modelURL = "https://huggingface.co/owner/repo/blob/0123456789abcdef0123456789abcdef01234567/other.gguf"
	tt.Frame()
	if s.modelURL != "https://huggingface.co/owner/repo/blob/0123456789abcdef0123456789abcdef01234567/other.gguf" {
		t.Fatal("the built-in address overwrote a hand-edited url", s.modelURL)
	}
}

func TestDownloadsCatalogKeepsTheSearchBox(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	engine, _ := core.CatalogEntryByID(core.CatalogEngineForCurrentPlatform())
	if engine.ID == "" {
		t.Skip("no engine build for this platform")
	}
	click(t, tt, engine.DisplayName())
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	click(t, tt, "搜索内置引擎")
	tt.Type("zzz")
	tt.Frame()
	if !tt.HasText("没有匹配的内置地址。") {
		t.Fatal(tt.Texts())
	}
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

func TestCatalogEntrySummaryLabelsEveryAsset(t *testing.T) {
	for _, entry := range core.Catalog() {
		summary := catalogEntrySummary(entry)
		if !strings.Contains(summary, entry.Title) {
			t.Fatal("summary lost the title", entry.ID, summary)
		}
		if entry.Reason != "" && !strings.Contains(summary, "仅参考") {
			t.Fatal("summary lost the reference marker", entry.ID, summary)
		}
		// The platform is part of the display name already; repeating it
		// would read "macOS arm64 · macOS arm64".
		if strings.Count(summary, core.PlatformLabel(entry.Platform)) > 1 {
			t.Fatal("summary repeats the platform", entry.ID, summary)
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
