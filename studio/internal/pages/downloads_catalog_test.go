package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

func TestDownloadsCatalogOffersOnlyFetchableAssets(t *testing.T) {
	s := testState()
	s.selected = "downloads"
	tt := ui.NewTester(s.view, 1120, 3000)
	for _, want := range []string{"内置地址", "引擎地址", "模型地址", "参考地址（不可直接下载）"} {
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
		if !tt.HasText("准备说明：" + entry.Doc) {
			t.Fatal("reference entry without a preparation document", entry.ID)
		}
		if s.engineURL == entry.URL || s.modelURL == entry.URL {
			t.Fatal("reference entry was prefilled", entry.ID)
		}
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
	click(t, tt, current.Title)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	click(t, tt, entry.Title)
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
	click(t, tt, engine.Title)
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
	}
}
