package pages

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"inferyard/studio/internal/icons/reicon"
)

func isBrandTokenRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func hasBrandToken(lower, token string) bool {
	if token == "" {
		return false
	}
	offset := 0
	for {
		idx := strings.Index(lower[offset:], token)
		if idx < 0 {
			return false
		}
		start := offset + idx
		end := start + len(token)
		leftOK := start == 0 || !isBrandTokenRune(rune(lower[start-1]))
		rightOK := end == len(lower) || !isBrandTokenRune(rune(lower[end]))
		if leftOK && rightOK {
			return true
		}
		offset = start + 1
	}
}

func hasAnySubstring(lower string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

func resolveBrandModelIcon(modelName string) (svg *ui.SVG, monochrome bool) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case hasAnySubstring(lower, "deepseek", "janus", "dsv3", "dsv4"):
		return svgBrandDeepSeek, false
	case hasAnySubstring(lower, "nemotron", "nvidia"):
		return svgBrandNvidia, false
	case hasAnySubstring(lower, "minicpm", "openbmb"):
		return svgBrandOpenBMB, false
	case hasAnySubstring(lower, "lfm", "liquid"):
		return svgBrandLiquid, true
	case hasAnySubstring(lower, "glm", "chatglm", "zhipu", "codegeex", "cogvlm") || hasBrandToken(lower, "zai"):
		return svgBrandZai, true
	case hasAnySubstring(lower, "hunyuan", "hy4-flash") || hasBrandToken(lower, "hy4"):
		return svgBrandHunyuan, false
	case hasAnySubstring(lower, "kimi", "moonshot", "moonlight"):
		return svgBrandKimi, false
	case strings.Contains(lower, "minimax"):
		return svgBrandMiniMax, false
	case hasAnySubstring(lower, "mistral", "mixtral", "ministral", "codestral", "devstral", "pixtral", "mathstral", "magistral"):
		return svgBrandMistral, false
	case strings.Contains(lower, "gemma"):
		return svgBrandGemma, false
	case hasAnySubstring(lower, "gpt-oss", "openai", "gpt-4", "gpt-3", "gpt4") || hasBrandToken(lower, "gpt"):
		return svgBrandOpenAI, true
	case hasAnySubstring(lower, "phi-", "phi2", "phi3", "phi4", "bitnet", "wizardlm", "orca-2", "microsoft") || hasBrandToken(lower, "phi"):
		return svgBrandMicrosoft, false
	case hasAnySubstring(lower, "internlm", "internvl"):
		return svgBrandInternLM, false
	case hasAnySubstring(lower, "command-r", "command_r", "c4ai", "aya-", "aya_", "cohere") || hasBrandToken(lower, "aya"):
		return svgBrandCohere, false
	case hasAnySubstring(lower, "doubao", "seed-oss", "seed-coder", "bytedance", "ui-tars"):
		return svgBrandDoubao, false
	case hasAnySubstring(lower, "ernie", "paddleocr", "baidu"):
		return svgBrandBaidu, false
	case hasAnySubstring(lower, "stepfun", "step-1", "step-2", "step-3", "step1", "step2", "step3", "step-audio"):
		return svgBrandStepFun, false
	case strings.Contains(lower, "baichuan"):
		return svgBrandBaichuan, false
	case strings.Contains(lower, "granite") || hasBrandToken(lower, "ibm"):
		return svgBrandIBM, true
	case hasAnySubstring(lower, "falcon", "tiiuae"):
		return svgBrandTII, false
	case hasAnySubstring(lower, "mimo", "xiaomi"):
		return svgBrandXiaomiMiMo, true
	case hasAnySubstring(lower, "longcat", "meituan"):
		return svgBrandLongCat, false
	case hasAnySubstring(lower, "bailing", "inclusionai", "ling-", "ring-") || hasBrandToken(lower, "ling"):
		return svgBrandLing, false
	case strings.Contains(lower, "01-ai") || hasBrandToken(lower, "yi"):
		return svgBrandYi, false
	case hasAnySubstring(lower, "qwen", "qwq", "tongyi", "marco-o1"):
		return svgBrandQwen, false
	case hasAnySubstring(lower, "smollm", "smolvlm", "starcoder", "zephyr", "huggingface"):
		return svgBrandHuggingFace, false
	case hasAnySubstring(strings.ReplaceAll(lower, "ollama", ""), "llama", "codellama", "vicuna"):
		return svgBrandMeta, false
	default:
		return svgBrandHuggingFace, false
	}
}

func renderBrandModelIcon(c *ui.Context, modelName string, size float32) {
	t := tokens(c)
	svg, monochrome := resolveBrandModelIcon(modelName)
	if monochrome {
		ui.Icon(c, svg).Size(size, size).TextColor(t.Color.FgPrimary)
		return
	}
	ui.Image(c, svg).Size(size, size).TextColor(t.Color.FgPrimary)
}

func resolveBrandEngineIcon(engineName string) (svg *ui.SVG, colorImage bool) {
	lower := strings.ToLower(strings.TrimSpace(engineName))
	switch {
	case hasAnySubstring(lower, "polystrata", "strata"):
		return svgBrandPolyStrata, true
	case strings.Contains(lower, "vllm"):
		return svgBrandVLLM, true
	case hasAnySubstring(lower, "lm studio", "lmstudio") || hasBrandToken(lower, "lms"):
		return svgBrandLMStudio, false
	case strings.Contains(lower, "ollama"):
		return svgBrandOllama, false
	case strings.Contains(lower, "mlx"):
		return svgBrandMLX, false
	case strings.Contains(lower, "llama"):
		return svgBrandLlamaCpp, false
	default:
		return nil, false
	}
}

func renderBrandEngineIcon(c *ui.Context, engineName string, size float32, tint ui.Color) {
	svg, colorImage := resolveBrandEngineIcon(engineName)
	switch {
	case svg == nil:
		components.Icon(c, reicon.Server).Size(size, size).TextColor(tint)
	case colorImage:
		ui.Image(c, svg).Size(size, size).TextColor(tint)
	default:
		ui.Icon(c, svg).Size(size, size).TextColor(tint)
	}
}

func brandIconTileBg(c *ui.Context) ui.Color {
	t := tokens(c)
	if c.Theme().Dark {
		return t.Color.Elevated
	}
	return t.Color.Sunken
}

func modelIconBox(c *ui.Context, modelName string, size, radius float32) {
	t := tokens(c)
	if radius <= 0 {
		radius = t.Radius.SM
	}
	iconSize := size * 0.64
	ui.Box(c).Size(size, size).Radius(radius).Shrink(0).AlignSelf(ui.Center).
		Background(brandIconTileBg(c)).Border(1, t.Color.BorderSubtle).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		renderBrandModelIcon(c, modelName, iconSize)
	})
}

func engineIconKind(name string) stateKind {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case hasAnySubstring(lower, "polystrata", "strata"):
		return stateSuccess
	case strings.Contains(lower, "ollama"):
		return stateNeutral
	case strings.Contains(lower, "mlx"):
		return stateActive
	case strings.Contains(lower, "llama"):
		return stateInfo
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
		Background(brandIconTileBg(c)).Border(1, t.Color.BorderSubtle).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		renderBrandEngineIcon(c, name, iconSize, t.Color.FgPrimary)
	})
}

func enginePill(c *ui.Context, name string) {
	enginePillWithHint(c, name, name)
}

func enginePillWithHint(c *ui.Context, label, hint string) {
	t := tokens(c)
	if strings.TrimSpace(hint) == "" {
		hint = label
	}
	scale := stateScale(c, engineIconKind(hint))
	ui.Row(c).AlignSelf(ui.Center).AlignItems(ui.Center).MinHeight(22).Gap(t.Density.Tight).
		Padding(t.Density.Tight, t.Density.Row).Radius(t.Radius.Full).
		Background(scale.SubtleBg).Children(func() {
		renderBrandEngineIcon(c, hint, 12, scale.Fg)
		ui.Text(c, label).FontSize(max(11, t.Type.ShellLabel)).TextColor(scale.Fg).SingleLine()
	})
}

func (s *viewState) modelIconHint(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	base := filepath.Base(trimmed)
	for _, m := range s.registry.Models {
		if m.Name == trimmed || modelLabel(m) == trimmed || (base != "" && m.Name == base) {
			return trimmed + " " + m.Name + " " + filepath.Base(m.Path) + " " + m.Architecture
		}
	}
	return trimmed
}

func (s *viewState) engineIconHint(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	for _, e := range s.registry.Engines {
		if e.Name == trimmed {
			return engineFamilyTitle(e.Family, e.Name) + " " + e.Family + " " + trimmed
		}
	}
	return trimmed
}

func (s *viewState) enginePill(c *ui.Context, name string) {
	enginePillWithHint(c, name, s.engineIconHint(name))
}

func inlineBadge(c *ui.Context, label string, kind stateKind, opts badgeOptions) ui.Element {
	return badge(c, label, kind, opts).AlignSelf(ui.Center)
}

func inlineButton(c *ui.Context, variant components.ButtonVariant, label string) ui.Element {
	return sizedButton(c, variant, sizeSM, label).AlignSelf(ui.Center)
}

func inlineDangerButton(c *ui.Context, label string) ui.Element {
	return styleButtonSize(c, dangerButton(c, label), sizeSM).AlignSelf(ui.Center)
}

func chevronBox(c *ui.Context, expanded bool) {
	t := tokens(c)
	chev := reicon.ChevronRight
	if expanded {
		chev = reicon.ChevronDown
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
