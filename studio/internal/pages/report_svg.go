package pages

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

const (
	svgStageHeight      = 420
	svgStageMaxHTML     = 1024 * 1024
	svgNamespaceURI     = "http://www.w3.org/2000/svg"
	svgDefaultNamespace = ` xmlns="http://www.w3.org/2000/svg"`
	// CLI studio_dashboard.CATEGORIES omits "svg", so studio.json emits raw "svg" as category_label.
	svgCategoryLabel    = "svg"
	svgStageFramePrefix = `<!doctype html><html><head><meta charset="utf-8">` +
		`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline';">` +
		`<style>html,body{margin:0;padding:0;width:100%;height:100%;overflow:hidden;background:#ffffff;display:flex;align-items:center;justify-content:center;}` +
		`img{display:block;max-width:100%;max-height:100%;width:auto;height:auto;object-fit:contain;}</style>` +
		`</head><body><img src="data:image/svg+xml;base64,`
	svgStageFrameSuffix = `" alt=""></body></html>`
)

var (
	svgLeadingProlog = regexp.MustCompile(`^(?:\s+|<\?[\s\S]*?\?>|<!--[\s\S]*?-->)*`)
)

type svgStageView interface {
	loadHTML(html string)
	native() ui.NativeWebView
	destroy()
}

type mygoSVGStage struct {
	view *mygo.WebView
}

func (m *mygoSVGStage) loadHTML(html string) {
	if m.view != nil && !m.view.IsDestroyed() {
		m.view.Page().LoadHTML(html, "")
	}
}

func (m *mygoSVGStage) native() ui.NativeWebView {
	if m.view == nil || m.view.IsDestroyed() {
		return nil
	}
	return m.view
}

func (m *mygoSVGStage) destroy() {
	if m.view != nil && !m.view.IsDestroyed() {
		m.view.Destroy()
	}
	m.view = nil
}

func parseRootSVGElement(body string) (xml.StartElement, bool, error) {
	dec := xml.NewDecoder(strings.NewReader(body))
	// Studio JSON strings are already UTF-8; accept ASCII-compatible XML encoding labels so root namespace inspection still runs.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return xml.StartElement{}, false, nil
		}
		if err != nil {
			return xml.StartElement{}, false, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se, true, nil
		}
	}
}

func findRootXMLNSValueSpan(attrs string) (start, end int, found bool) {
	i, n := 0, len(attrs)
	for i < n {
		for i < n && (attrs[i] == ' ' || attrs[i] == '\t' || attrs[i] == '\n' || attrs[i] == '\r' || attrs[i] == '/') {
			i++
		}
		if i >= n {
			break
		}
		nameStart := i
		for i < n && attrs[i] != '=' && attrs[i] != ' ' && attrs[i] != '\t' && attrs[i] != '\n' && attrs[i] != '\r' && attrs[i] != '/' {
			i++
		}
		name := attrs[nameStart:i]
		for i < n && (attrs[i] == ' ' || attrs[i] == '\t' || attrs[i] == '\n' || attrs[i] == '\r') {
			i++
		}
		if i >= n || attrs[i] != '=' {
			continue
		}
		i++
		for i < n && (attrs[i] == ' ' || attrs[i] == '\t' || attrs[i] == '\n' || attrs[i] == '\r') {
			i++
		}
		if i >= n || (attrs[i] != '"' && attrs[i] != '\'') {
			continue
		}
		quote := attrs[i]
		i++
		valStart := i
		for i < n && attrs[i] != quote {
			i++
		}
		if i >= n {
			break
		}
		valEnd := i
		i++
		if name == "xmlns" {
			return valStart, valEnd, true
		}
	}
	return 0, 0, false
}

func ensureSVGNamespace(svg string) (string, bool) {
	bodyOffset := 0
	if strings.HasPrefix(svg, "\ufeff") {
		bodyOffset = len("\ufeff")
	}
	body := svg[bodyOffset:]
	root, found, err := parseRootSVGElement(body)
	if err != nil {
		return svg, true
	}
	if !found || root.Name.Local != "svg" {
		return "", false
	}
	if root.Name.Space == svgNamespaceURI {
		return svg, true
	}
	if root.Name.Space != "" {
		return "", false
	}
	prolog := svgLeadingProlog.FindStringIndex(body)
	pos := bodyOffset
	if prolog != nil {
		pos += prolog[1]
	}
	if !strings.HasPrefix(svg[pos:], "<svg") || pos+4 >= len(svg) {
		return svg, true
	}
	next := svg[pos+4]
	if next != '>' && next != '/' && next != ' ' && next != '\t' && next != '\n' && next != '\r' {
		return svg, true
	}
	end := findXMLTagEnd(svg, pos+4)
	if end < 0 {
		return svg, true
	}
	attrs := svg[pos+4 : end]
	if valStart, valEnd, ok := findRootXMLNSValueSpan(attrs); ok {
		absStart, absEnd := pos+4+valStart, pos+4+valEnd
		return svg[:absStart] + svgNamespaceURI + svg[absEnd:], true
	}
	return svg[:pos+4] + svgDefaultNamespace + svg[pos+4:], true
}

func findXMLTagEnd(s string, pos int) int {
	var quote byte
	for i := pos; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			continue
		}
		if ch == '>' {
			return i
		}
	}
	return -1
}

func buildSVGPreviewHTML(svg string) (string, bool) {
	if len(svg) == 0 || len(svg) > inferyard.StudioMaxSVGBytes {
		return "", false
	}
	renderSVG, ok := ensureSVGNamespace(svg)
	if !ok {
		return "", false
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(renderSVG))
	doc := svgStageFramePrefix + encoded + svgStageFrameSuffix
	if len(doc) > svgStageMaxHTML {
		return "", false
	}
	return doc, true
}

func (r *reportViewState) releaseSVGStage() {
	if r == nil {
		return
	}
	if r.svgStage != nil {
		r.svgStage.destroy()
		r.svgStage = nil
	}
	r.svgLoadedKey = ""
	r.svgStageErr = false
}

func (s *viewState) closeOpenedReport() {
	if s.openedReport != nil {
		s.openedReport.releaseSVGStage()
		s.openedReport = nil
	}
}

func (s *viewState) createSVGStage() (svgStageView, error) {
	if s.newSVGStage != nil {
		return s.newSVGStage()
	}
	if s.win == nil {
		return nil, errors.New("window unavailable")
	}
	v, err := s.win.NewWebView(mygo.WebViewOptions{
		BackgroundColor:   "#ffffff",
		ScrollToContainer: true,
		Page:              mygo.PageOptions{Untrusted: true},
	})
	if err != nil {
		return nil, err
	}
	return &mygoSVGStage{view: v}, nil
}

func runHasSVGCases(run core.ReportRun) bool {
	bundleID, _, _ := strings.Cut(run.Facets.BundleLabel, " · ")
	if strings.TrimSpace(bundleID) == inferyard.DefaultBundlePelican {
		return true
	}
	for _, req := range run.Requests {
		if req.CategoryLabel == svgCategoryLabel {
			return true
		}
	}
	return false
}

func svgCaseLatencyMS(run core.ReportRun, outputs []core.ReportSVGOutput, index int) *float64 {
	if index < 0 || index >= len(outputs) {
		return nil
	}
	target := outputs[index].CaseID
	occ := 0
	for i := 0; i < index; i++ {
		if outputs[i].CaseID == target {
			occ++
		}
	}
	seen := 0
	for _, req := range run.Requests {
		if req.CategoryLabel == svgCategoryLabel && req.CaseID == target {
			if seen == occ {
				return req.LatencyMS
			}
			seen++
		}
	}
	seen = 0
	for _, req := range run.Requests {
		if req.CaseID == target {
			if seen == occ {
				return req.LatencyMS
			}
			seen++
		}
	}
	return nil
}

func (s *viewState) ensureSVGStage(report *reportViewState, item core.ReportSVGOutput) bool {
	if item.Status != "ok" || item.SVG == nil {
		report.releaseSVGStage()
		return false
	}
	key := *item.SVG
	if report.svgStage != nil && report.svgLoadedKey == key {
		return true
	}
	doc, ok := buildSVGPreviewHTML(key)
	if !ok {
		report.releaseSVGStage()
		return false
	}
	if report.svgStage == nil && !report.svgStageErr {
		stage, err := s.createSVGStage()
		if err != nil {
			report.svgStageErr = true
			return false
		}
		report.svgStage = stage
		report.svgLoadedKey = ""
	}
	if report.svgStage == nil {
		return false
	}
	if report.svgLoadedKey != key {
		report.svgStage.loadHTML(doc)
		report.svgLoadedKey = key
	}
	return true
}

func (s *viewState) reportSVGSection(c *ui.Context, report *reportViewState, run core.ReportRun) {
	t := tokens(c)
	outputs := run.SVGOutputs
	if len(outputs) == 0 {
		report.releaseSVGStage()
		if !runHasSVGCases(run) {
			return
		}
		reportSectionCard(c, lucide.Layers, tr(c, "reports.svg.title"), nil, func() {
			infoBanner(c, tr(c, "reports.svg.regenerate_hint"))
		})
		return
	}
	if report.svgIndex < 0 || report.svgIndex >= len(outputs) {
		report.svgIndex = 0
	}
	var trailing func()
	if len(outputs) > 1 {
		trailing = func() {
			pickerLabel := tr(c, "reports.svg.select_case")
			ui.Row(c).AlignItems(ui.Center).Gap(t.Density.Row).Shrink(0).Children(func() {
				ui.Text(c, pickerLabel).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).SingleLine()
				picker := studioSelect(c, &report.svgIndex)
				current := outputs[report.svgIndex].CaseID
				picker.Trigger.Label(pickerLabel).Value(current).Children(func() {
					ui.Text(c, current).FontSize(t.Type.Caption).SingleLine()
				})
				picker.Popup(func(ui.Element) {
					for i, item := range outputs {
						picker.Item(i).Label(item.CaseID).Children(func() {
							ui.Text(c, item.CaseID).SingleLine()
						})
					}
				})
			})
		}
	}
	reportSectionCard(c, lucide.Layers, tr(c, "reports.svg.title"), trailing, func() {
		item := outputs[report.svgIndex]
		applyMaterial(c, ui.Column(c).FillWidth().Gap(t.Density.Tight+1).Padding(t.Density.Row, t.Density.Stack), materialSunken).Children(func() {
			svgItemHeader(c, run, outputs, report.svgIndex)
			if item.Prompt != "" {
				ui.Text(c, item.Prompt).FontSize(t.Type.Caption).TextColor(t.Color.FgSecondary).Selectable()
			}
		})
		if item.Status != "ok" || item.SVG == nil {
			report.releaseSVGStage()
			noticeBanner(c, noticeBannerOptions{
				Kind:    stateWarning,
				Size:    bannerCompact,
				Message: svgStatusMessage(c, item.Status),
			})
			return
		}
		if s.ensureSVGStage(report, item) {
			ui.Box(c).FillWidth().Background(ui.Hex("#ffffff")).Border(1, t.Color.BorderDefault).Radius(t.Radius.Inset).Clip().Children(func() {
				ui.WebView(c.Key("report-svg-webview"), report.svgStage.native()).
					FillWidth().
					Height(svgStageHeight).
					Radius(t.Radius.Inset).
					Label(trf(c, "reports.svg.stage_aria", "case_id", item.CaseID))
			})
		} else {
			infoBanner(c, tr(c, "reports.svg.webview_unavailable"))
		}
		componentFoldKeyed(c, "svg-source-"+run.Source.RunID+"-"+item.CaseID, tr(c, "reports.svg.source_fold"), func() {
			applyMaterial(c, components.ScrollView(c, components.ScrollViewOptions{}).FillWidth().MaxHeight(260), materialSunken).Children(func() {
				ui.Column(c).FillWidth().Padding(t.Density.Stack).Children(func() {
					ui.Text(c, *item.SVG).FontSize(t.Type.Caption).TextColor(t.Color.FgPrimary).Selectable()
				})
			})
		})
	})
}

func svgStatusKind(status string) stateKind {
	if status == "ok" {
		return stateSuccess
	}
	return stateWarning
}

func svgItemHeader(c *ui.Context, run core.ReportRun, outputs []core.ReportSVGOutput, index int) {
	t := tokens(c)
	item := outputs[index]
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(t.Density.Row).Children(func() {
		ui.Text(c, item.CaseID).Bold().FontFeatures("tnum").SingleLine()
		inlineBadge(c, svgStatusLabel(c, item.Status), svgStatusKind(item.Status), badgeOptions{Size: sizeSM, Style: badgeSoft})
		ui.Box(c).Grow(1)
		meta := trf(c, "reports.svg.meta_bytes", "bytes", groupedInt(item.Bytes))
		if latency := svgCaseLatencyMS(run, outputs, index); latency != nil {
			meta += " · " + trf(c, "reports.svg.meta_latency", "latency", reportScaled(latency, 1000, "s"))
		}
		ui.Text(c, meta).FontSize(t.Type.Caption).FontFeatures("tnum").TextColor(t.Color.FgSecondary).SingleLine()
	})
}

func svgStatusLabel(c *ui.Context, status string) string {
	switch status {
	case "ok":
		return tr(c, "reports.svg.status.ok")
	case "not_found":
		return tr(c, "reports.svg.status.not_found")
	case "malformed":
		return tr(c, "reports.svg.status.malformed")
	case "too_large":
		return tr(c, "reports.svg.status.too_large")
	default:
		return reportText(status)
	}
}

func svgStatusColor(c *ui.Context, status string) ui.Color {
	if status == "ok" {
		return stateColor(c, stateSuccess)
	}
	return stateColor(c, stateWarning)
}

func svgStatusMessage(c *ui.Context, status string) string {
	switch status {
	case "not_found":
		return tr(c, "reports.svg.message.not_found")
	case "malformed":
		return tr(c, "reports.svg.message.malformed")
	case "too_large":
		return tr(c, "reports.svg.message.too_large")
	default:
		return tr(c, "reports.svg.message.not_found")
	}
}
