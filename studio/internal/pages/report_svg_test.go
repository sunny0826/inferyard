package pages

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type fakeSVGStage struct {
	loads     []string
	destroyed int
}

func (f *fakeSVGStage) loadHTML(html string)     { f.loads = append(f.loads, html) }
func (f *fakeSVGStage) native() ui.NativeWebView { return nil }
func (f *fakeSVGStage) destroy()                 { f.destroyed++ }

func decodeEmbeddedSVG(t *testing.T, html string) string {
	t.Helper()
	const marker = `src="data:image/svg+xml;base64,`
	start := strings.Index(html, marker)
	if start < 0 {
		t.Fatalf("missing svg data URI in %s", html)
	}
	rest := html[start+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("unterminated data URI in %s", html)
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		t.Fatalf("decode embedded base64: %v", err)
	}
	return string(raw)
}

func TestBuildSVGPreviewHTMLSandboxNamespaceAndSizeLimit(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><circle r="6"><animate attributeName="r" values="6;9;6" dur="1s" repeatCount="indefinite"/></circle></svg>`
	html, ok := buildSVGPreviewHTML(svg)
	if !ok {
		t.Fatal("expected valid SVG preview HTML")
	}
	if !strings.Contains(html, "default-src 'none'; img-src data:; style-src 'unsafe-inline';") {
		t.Fatalf("missing strict CSP header: %s", html)
	}
	if got := decodeEmbeddedSVG(t, html); got != svg {
		t.Fatalf("expected existing xmlns SVG preserved verbatim, got %q", got)
	}
	if strings.Contains(html, "<svg") || strings.Contains(html, "<script>") {
		t.Fatalf("raw SVG markup must never be embedded directly in HTML: %s", html)
	}

	// Table-driven namespace normalization covering all four root namespace cases plus BOM/prolog/charset fallback.
	cases := []struct {
		name   string
		input  string
		wantOK bool
		want   string
	}{
		{
			name:   "existing default SVG namespace preserved",
			input:  `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`,
			wantOK: true,
			want:   `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`,
		},
		{
			name:   "prefixed SVG namespace preserved",
			input:  `<svg:svg xmlns:svg="http://www.w3.org/2000/svg"><svg:circle r="4"/></svg:svg>`,
			wantOK: true,
			want:   `<svg:svg xmlns:svg="http://www.w3.org/2000/svg"><svg:circle r="4"/></svg:svg>`,
		},
		{
			name:   "bare svg injects default namespace",
			input:  `<svg viewBox="0 0 64 64"><circle r="6"><animate attributeName="r" values="6;9;6" dur="1s"/></circle></svg>`,
			wantOK: true,
			want:   `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle r="6"><animate attributeName="r" values="6;9;6" dur="1s"/></circle></svg>`,
		},
		{
			name:   "empty double-quoted xmlns replaced in place",
			input:  `<svg xmlns="" viewBox="0 0 64 64"><circle r="4"/></svg>`,
			wantOK: true,
			want:   `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle r="4"/></svg>`,
		},
		{
			name:   "empty single-quoted xmlns replaced while preserving xmlns:xlink",
			input:  `<svg xmlns:xlink="http://www.w3.org/1999/xlink" xmlns=''><circle r="4"/></svg>`,
			wantOK: true,
			want:   `<svg xmlns:xlink="http://www.w3.org/1999/xlink" xmlns='http://www.w3.org/2000/svg'><circle r="4"/></svg>`,
		},
		{
			name:   "xmlns inside another attribute value is never corrupted",
			input:  `<svg aria-label="sample xmlns=''"><circle r="6"/></svg>`,
			wantOK: true,
			want:   `<svg xmlns="http://www.w3.org/2000/svg" aria-label="sample xmlns=''"><circle r="6"/></svg>`,
		},
		{
			name:   "utf8 BOM bare svg injects namespace after BOM",
			input:  "\ufeff<svg><circle r=\"4\"/></svg>",
			wantOK: true,
			want:   "\ufeff<svg xmlns=\"http://www.w3.org/2000/svg\"><circle r=\"4\"/></svg>",
		},
		{
			name:   "xml decl and comment prolog injects namespace on root svg",
			input:  "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- pelican -->\n<svg xmlns:xlink=\"http://www.w3.org/1999/xlink\"><circle r=\"4\"/></svg>",
			wantOK: true,
			want:   "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!-- pelican -->\n<svg xmlns=\"http://www.w3.org/2000/svg\" xmlns:xlink=\"http://www.w3.org/1999/xlink\"><circle r=\"4\"/></svg>",
		},
		{
			name:   "non-SVG namespace rejected without rewriting",
			input:  `<svg xmlns="http://example.com"><circle r="4"/></svg>`,
			wantOK: false,
		},
		{
			name:   "ISO-8859-1 bare svg still gets SVG namespace",
			input:  `<?xml version="1.0" encoding="ISO-8859-1"?><svg><circle r="4"/></svg>`,
			wantOK: true,
			want:   `<?xml version="1.0" encoding="ISO-8859-1"?><svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`,
		},
		{
			name:   "US-ASCII bare svg still gets SVG namespace",
			input:  `<?xml version="1.0" encoding="US-ASCII"?><svg><circle r="4"/></svg>`,
			wantOK: true,
			want:   `<?xml version="1.0" encoding="US-ASCII"?><svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, ok := buildSVGPreviewHTML(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("buildSVGPreviewHTML ok=%v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got := decodeEmbeddedSVG(t, doc); got != tc.want {
				t.Fatalf("embedded svg = %q, want %q", got, tc.want)
			}
		})
	}

	if _, ok := buildSVGPreviewHTML(""); ok {
		t.Fatal("empty SVG must be rejected")
	}
	oversized := strings.Repeat("a", inferyard.StudioMaxSVGBytes+1)
	if _, ok := buildSVGPreviewHTML(oversized); ok {
		t.Fatal("oversized SVG must be rejected")
	}
}

func TestReportSVGStageLifecycleReuseDuplicateCasesAndNewBenchmark(t *testing.T) {
	svg := `<svg viewBox="0 0 64 64"><circle r="8"><animate attributeName="r" values="8;12;8" dur="1s" repeatCount="indefinite"/></circle></svg>`
	lat1, lat2 := 1000.0, 2500.0
	run := core.ReportRun{
		Source: core.ReportSource{RunID: "pelican-run-1", Path: "/results/pelican-run-1"},
		Model:  "Test Model",
		Facets: core.ReportFacets{BundleLabel: inferyard.DefaultBundlePelican + " · 1.0.0"},
		Requests: []core.ReportRequest{
			{CaseID: "svg-pelican-01", CategoryLabel: "svg", State: "completed", LatencyMS: &lat1},
			{CaseID: "svg-pelican-01", CategoryLabel: "svg", State: "completed", LatencyMS: &lat2},
		},
		SVGOutputs: []core.ReportSVGOutput{
			{CaseID: "svg-pelican-01", Prompt: "pelican prompt", Status: "ok", SVG: &svg, Bytes: int64(len(svg))},
			{CaseID: "svg-pelican-01", Prompt: "pelican prompt", Status: "ok", SVG: &svg, Bytes: int64(len(svg))},
		},
	}

	s := testState()
	s.selected = "reports"
	s.openedReport = &reportViewState{
		reference: "pelican-report",
		index:     &core.ReportIndex{Runs: []core.ReportRun{run}},
	}

	var stages []*fakeSVGStage
	s.newSVGStage = func() (svgStageView, error) {
		st := &fakeSVGStage{}
		stages = append(stages, st)
		return st, nil
	}

	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1400)
	tt.Frame()
	if len(stages) != 1 || len(stages[0].loads) != 1 {
		t.Fatalf("expected 1 created stage with 1 HTML load, got stages=%d loads=%v", len(stages), stages)
	}
	if !tt.HasText("已提取") || !tt.HasText("耗时 1.00 s") || !tt.HasText("查看 SVG 源码") {
		t.Fatalf("missing first SVG showcase metadata: %v", tt.Texts())
	}

	// Switching to the second occurrence with identical SVG shows second latency and avoids redundant LoadHTML.
	s.openedReport.svgIndex = 1
	tt.Frame()
	if !tt.HasText("耗时 2.50 s") {
		t.Fatalf("expected second occurrence latency 2.50 s, got %v", tt.Texts())
	}
	if len(stages) != 1 || len(stages[0].loads) != 1 || stages[0].destroyed != 0 {
		t.Fatalf("identical SVG switch should reuse WebView without reload: stages=%d loads=%d destroyed=%d", len(stages), len(stages[0].loads), stages[0].destroyed)
	}

	// Starting a new benchmark from reports page destroys the WebView.
	s.newBenchmark()
	if stages[0].destroyed != 1 {
		t.Fatalf("newBenchmark must release WebView, destroyed=%d", stages[0].destroyed)
	}

	// Returning to reports lazily recreates the WebView; Back button closes and destroys it.
	s.navigateToNow("reports", "")
	tt.Frame()
	if len(stages) != 2 || len(stages[1].loads) != 1 {
		t.Fatalf("returning to reports should lazily recreate WebView, got stages=%d", len(stages))
	}
	click(t, tt, "返回报告列表")
	if stages[1].destroyed != 1 || s.openedReport != nil {
		t.Fatalf("back button must close report and destroy WebView: destroyed=%d opened=%v", stages[1].destroyed, s.openedReport)
	}
}

func TestReportSVGDegradedStatesAndLegacyHintPrecision(t *testing.T) {
	t.Run("webview unavailable shows Extracted badge and fallback notice", func(t *testing.T) {
		svg := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>`
		s := testState()
		s.selected = "reports"
		s.newSVGStage = func() (svgStageView, error) { return nil, errors.New("no webview2") }
		s.openedReport = &reportViewState{
			reference: "degraded",
			index: &core.ReportIndex{Runs: []core.ReportRun{{
				Source: core.ReportSource{RunID: "r1", Path: "/r1"},
				Model:  "M",
				Facets: core.ReportFacets{BundleLabel: inferyard.DefaultBundlePelican + " · 1.0.0"},
				SVGOutputs: []core.ReportSVGOutput{{
					CaseID: "svg-pelican-01", Prompt: "pelican prompt", Status: "ok", SVG: &svg, Bytes: int64(len(svg)),
				}},
			}}},
		}
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
		tt.Frame()
		if !tt.HasText("已提取") || !tt.HasText("当前环境无法启动内嵌动图预览，可在下方查看原始 SVG 源码。") || !tt.HasText("查看 SVG 源码") {
			t.Fatalf("expected Extracted badge, degraded notice, and source fold: %v", tt.Texts())
		}
	})

	t.Run("malformed status skips webview creation and shows reason", func(t *testing.T) {
		called := false
		s := testState()
		s.selected = "reports"
		s.newSVGStage = func() (svgStageView, error) { called = true; return &fakeSVGStage{}, nil }
		s.openedReport = &reportViewState{
			reference: "malformed",
			index: &core.ReportIndex{Runs: []core.ReportRun{{
				Source: core.ReportSource{RunID: "r2", Path: "/r2"},
				Model:  "M",
				Facets: core.ReportFacets{BundleLabel: inferyard.DefaultBundlePelican + " · 1.0.0"},
				SVGOutputs: []core.ReportSVGOutput{{
					CaseID: "svg-pelican-01", Prompt: "pelican prompt", Status: "malformed", SVG: nil, Bytes: 32,
				}},
			}}},
		}
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
		tt.Frame()
		if called {
			t.Fatal("malformed SVG output must not create a WebView")
		}
		if !tt.HasText("解析失败") || !tt.HasText("模型输出中的 SVG 结构无效，无法渲染。") {
			t.Fatalf("missing malformed status message: %v", tt.Texts())
		}
	})

	t.Run("legacy pelican report shows regenerate hint while custom prefix bundle does not", func(t *testing.T) {
		legacyRun := core.ReportRun{
			Source:   core.ReportSource{RunID: "r3", Path: "/r3"},
			Model:    "M",
			Facets:   core.ReportFacets{BundleLabel: inferyard.DefaultBundlePelican + " · 1.0.0"},
			Requests: []core.ReportRequest{{CaseID: "svg-pelican-01", CategoryLabel: "svg", State: "completed"}},
		}
		customPrefixRun := core.ReportRun{
			Source:   core.ReportSource{RunID: "r4", Path: "/r4"},
			Model:    "M",
			Facets:   core.ReportFacets{BundleLabel: "zh-svg-pelican-text · 1.0.0"},
			Requests: []core.ReportRequest{{CaseID: "txt-01", CategoryLabel: "知识问答", State: "completed"}},
		}
		if !runHasSVGCases(legacyRun) {
			t.Fatal("expected legacy pelican run to be detected")
		}
		if runHasSVGCases(customPrefixRun) {
			t.Fatal("custom bundle sharing prefix with no svg cases must not trigger SVG regenerate hint")
		}
		s := testState()
		s.selected = "reports"
		s.openedReport = &reportViewState{reference: "legacy-pelican", index: &core.ReportIndex{Runs: []core.ReportRun{legacyRun}}}
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
		tt.Frame()
		if !tt.HasText("当前报告未包含 SVG 展示数据，请从历史运行重新生成报告。") {
			t.Fatalf("missing legacy regenerate hint: %v", tt.Texts())
		}
	})

	t.Run("foreign namespace item does not poison subsequent valid SVG item", func(t *testing.T) {
		foreign := `<svg xmlns="http://example.com"><circle r="4"/></svg>`
		valid := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="8"/></svg>`
		var stages []*fakeSVGStage
		s := testState()
		s.selected = "reports"
		s.newSVGStage = func() (svgStageView, error) {
			st := &fakeSVGStage{}
			stages = append(stages, st)
			return st, nil
		}
		s.openedReport = &reportViewState{
			reference: "mixed-svg",
			index: &core.ReportIndex{Runs: []core.ReportRun{{
				Source: core.ReportSource{RunID: "r5", Path: "/r5"},
				Model:  "M",
				Facets: core.ReportFacets{BundleLabel: inferyard.DefaultBundlePelican + " · 1.0.0"},
				SVGOutputs: []core.ReportSVGOutput{
					{CaseID: "svg-foreign", Prompt: "p1", Status: "ok", SVG: &foreign, Bytes: int64(len(foreign))},
					{CaseID: "svg-valid", Prompt: "p2", Status: "ok", SVG: &valid, Bytes: int64(len(valid))},
				},
			}}},
		}
		tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Children(func() { s.reportsView(c) }) }, 1120, 1200)
		tt.Frame()
		if len(stages) != 0 {
			t.Fatalf("foreign namespace item must not create WebView, got %d", len(stages))
		}
		s.openedReport.svgIndex = 1
		tt.Frame()
		if len(stages) != 1 || len(stages[0].loads) != 1 {
			t.Fatalf("valid second item must still create and load WebView, got stages=%d", len(stages))
		}
	})
}
