package pages

import (
	_ "embed"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

//go:embed brand_icons/qwen-color.svg
var brandQwenSVG []byte

//go:embed brand_icons/gemma-color.svg
var brandGemmaSVG []byte

//go:embed brand_icons/meta-color.svg
var brandMetaSVG []byte

//go:embed brand_icons/deepseek-color.svg
var brandDeepSeekSVG []byte

//go:embed brand_icons/huggingface-color.svg
var brandHuggingFaceSVG []byte

//go:embed brand_icons/ollama.svg
var brandOllamaSVG []byte

//go:embed brand_icons/vllm-color.svg
var brandVLLMSVG []byte

//go:embed brand_icons/lmstudio.svg
var brandLMStudioSVG []byte

//go:embed brand_icons/apple.svg
var brandAppleSVG []byte

//go:embed brand_icons/llamaindex.svg
var brandLlamaIndexSVG []byte

var (
	svgBrandQwen        = ui.MustParseSVG(brandQwenSVG)
	svgBrandGemma       = ui.MustParseSVG(brandGemmaSVG)
	svgBrandMeta        = ui.MustParseSVG(brandMetaSVG)
	svgBrandDeepSeek    = ui.MustParseSVG(brandDeepSeekSVG)
	svgBrandHuggingFace = ui.MustParseSVG(brandHuggingFaceSVG)
	svgBrandOllama      = ui.MustParseSVG(brandOllamaSVG)
	svgBrandVLLM        = ui.MustParseSVG(brandVLLMSVG)
	svgBrandLMStudio    = ui.MustParseSVG(brandLMStudioSVG)
	svgBrandApple       = ui.MustParseSVG(brandAppleSVG)
	svgBrandLlamaIndex  = ui.MustParseSVG(brandLlamaIndexSVG)
)

func renderBrandModelIcon(c *ui.Context, modelName string, size float32) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	var svg *ui.SVG
	switch {
	case strings.Contains(lower, "qwen"):
		svg = svgBrandQwen
	case strings.Contains(lower, "gemma"):
		svg = svgBrandGemma
	case strings.Contains(lower, "llama"):
		svg = svgBrandMeta
	case strings.Contains(lower, "deepseek"):
		svg = svgBrandDeepSeek
	default:
		svg = svgBrandHuggingFace
	}
	ui.Image(c, svg).Size(size, size)
}

func renderBrandEngineIcon(c *ui.Context, engineName string, size float32, tint ui.Color) {
	lower := strings.ToLower(strings.TrimSpace(engineName))
	switch {
	case strings.Contains(lower, "ollama"):
		ui.Icon(c, svgBrandOllama).Size(size, size).TextColor(tint)
	case strings.Contains(lower, "vllm"):
		ui.Image(c, svgBrandVLLM).Size(size, size)
	case strings.Contains(lower, "lm studio") || strings.Contains(lower, "lmstudio"):
		ui.Icon(c, svgBrandLMStudio).Size(size, size).TextColor(tint)
	case strings.Contains(lower, "mlx"):
		ui.Icon(c, svgBrandApple).Size(size, size).TextColor(tint)
	case strings.Contains(lower, "llama"):
		ui.Icon(c, svgBrandLlamaIndex).Size(size, size).TextColor(tint)
	case strings.Contains(lower, "polystrata"):
		components.Icon(c, lucide.Layers).Size(size, size).TextColor(tint)
	default:
		components.Icon(c, lucide.Server).Size(size, size).TextColor(tint)
	}
}

func modelIconBox(c *ui.Context, modelName string, size, radius float32) {
	t := tokens(c)
	if radius <= 0 {
		radius = t.Radius.SM
	}
	iconSize := size * 0.64
	ui.Box(c).Size(size, size).Radius(radius).Shrink(0).AlignSelf(ui.Center).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		renderBrandModelIcon(c, modelName, iconSize)
	})
}

func engineIconKind(name string) stateKind {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "llama"):
		return stateInfo
	case strings.Contains(lower, "mlx"):
		return stateActive
	case strings.Contains(lower, "polystrata"):
		return stateSuccess
	default:
		return stateNeutral
	}
}

func engineIconBox(c *ui.Context, name string, size, radius float32) {
	t := tokens(c)
	if radius <= 0 {
		radius = t.Radius.SM
	}
	iconSize := size * 0.62
	ui.Box(c).Size(size, size).Radius(radius).Shrink(0).AlignSelf(ui.Center).
		Background(t.Color.Sunken).Border(1, t.Color.BorderSubtle).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		renderBrandEngineIcon(c, name, iconSize, t.Color.FgPrimary)
	})
}

func enginePill(c *ui.Context, name string) {
	t := tokens(c)
	scale := stateScale(c, engineIconKind(name))
	ui.Row(c).AlignSelf(ui.Center).AlignItems(ui.Center).MinHeight(22).Gap(t.Density.Tight).
		Padding(t.Density.Tight, t.Density.Row).Radius(t.Radius.Full).
		Background(scale.SubtleBg).Children(func() {
		renderBrandEngineIcon(c, name, 12, scale.Fg)
		ui.Text(c, name).FontSize(max(11, t.Type.ShellLabel)).TextColor(scale.Fg).SingleLine()
	})
}

func inlineBadge(c *ui.Context, label string, kind stateKind, opts badgeOptions) ui.Element {
	return badge(c, label, kind, opts).AlignSelf(ui.Center)
}

func inlineButton(c *ui.Context, variant components.ButtonVariant, label string) ui.Element {
	return sizedButton(c, variant, sizeSM, label).AlignSelf(ui.Center).MinHeight(28)
}

func inlineDangerButton(c *ui.Context, label string) ui.Element {
	t := tokens(c)
	return dangerButton(c, label).
		Padding(t.Density.Tight, t.Density.Row).
		FontSize(t.Type.Caption).
		AlignSelf(ui.Center).MinHeight(28)
}

func chevronBox(c *ui.Context, expanded bool) {
	t := tokens(c)
	chev := lucide.ChevronRight
	if expanded {
		chev = lucide.ChevronDown
	}
	ui.Box(c).Size(20, 20).Shrink(0).AlignSelf(ui.Center).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		components.Icon(c, chev).Size(14, 14).TextColor(t.Color.FgSecondary)
	})
}

func sidebarIconSlot(c *ui.Context, fn func()) {
	ui.Box(c).Size(16, 16).Shrink(0).AlignSelf(ui.Center).
		AlignItems(ui.Center).Justify(ui.Center).Children(fn)
}

func compactCard(c *ui.Context, el ui.Element, pad float32) (ui.Element, float32) {
	t := tokens(c)
	outer := t.Radius.LG
	return applyMaterial(c, el.Padding(pad), materialCard).Radius(outer), concentricRadius(outer, pad)
}
