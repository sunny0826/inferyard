package pages

import (
	"sync"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

type studioAccentSpec struct {
	id       string
	labelKey string
	descKey  string
	light    paletteColors
	dark     paletteColors
}

func makeButtonPalette(
	secBg, secHov, secPrs, secBdr, secBdrHov,
	dngBg, dngHov, dngPrs, dngBdr, dngFg,
	dngSol, dngSolHov, dngSolPrs,
	engRun, engRunPrs,
	selBg, focus ui.Color,
) buttonPalette {
	return buttonPalette{
		secondaryBg: secBg, secondaryHover: secHov, secondaryPressed: secPrs,
		secondaryBorder: secBdr, secondaryBorderHover: secBdrHov,
		dangerBg: dngBg, dangerHover: dngHov, dangerPressed: dngPrs, dangerBorder: dngBdr, dangerFg: dngFg,
		dangerSolid: dngSol, dangerSolidHover: dngSolHov, dangerSolidPressed: dngSolPrs,
		engineRunning: engRun, engineRunningPressed: engRunPrs,
		selectionBg: selBg, focusRing: focus,
	}
}

var (
	cobaltAccentSpec = studioAccentSpec{
		id:       core.AccentCobalt,
		labelKey: "settings.appearance.accent.cobalt",
		descKey:  "settings.appearance.accent.cobalt_desc",
		light: paletteColors{
			background: ui.Hex("#f5f5f7"), sunken: ui.Hex("#efeff1"), surface: ui.Hex("#ffffff"),
			surfaceHover: ui.Hex("#f5f5f7"), surfacePressed: ui.Hex("#ebebee"), elevated: ui.Hex("#ffffff"),
			borderSubtle: ui.Hex("#ececee"), border: ui.Hex("#e0e0e4"), borderStrong: ui.Hex("#c7c7cc"),
			text: ui.Hex("#181818"), muted: ui.Hex("#6a6a6a"), tertiary: ui.Hex("#6a6a6a"),
			accent: ui.Hex("#1d5bbf"), accentHover: ui.Hex("#174da6"), accentPressed: ui.Hex("#123f8a"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#1d5bbf"), success: ui.Hex("#157a4c"), warning: ui.Hex("#9a5b08"), danger: ui.Hex("#c42336"),
			chart: [6]ui.Color{ui.Hex("#1d5bbf"), ui.Hex("#157a4c"), ui.Hex("#c26d0a"), ui.Hex("#6d4cd1"), ui.Hex("#c42336"), ui.Hex("#6a6a6a")},
			buttons: makeButtonPalette(
				ui.Hex("#ffffff"), ui.Hex("#f5f5f7"), ui.Hex("#ebebee"), ui.Hex("#dcdce0"), ui.Hex("#c7c7cc"),
				ui.Hex("#fef2f3"), ui.Hex("#fde2e5"), ui.Hex("#fbd0d6"), ui.Hex("#c42336").Alpha(.42), ui.Hex("#b41f31"),
				ui.Hex("#c42336"), ui.Hex("#a81c2d"), ui.Hex("#8e1624"),
				ui.Hex("#157a4c"), ui.Hex("#0f5c39"),
				ui.Hex("#1d5bbf").Alpha(.10), ui.Hex("#1d5bbf").Alpha(.70),
			),
		},
		dark: paletteColors{
			background: ui.Hex("#15181e"), sunken: ui.Hex("#0f1217"), surface: ui.Hex("#1d2129"),
			surfaceHover: ui.Hex("#262b36"), surfacePressed: ui.Hex("#2f3643"), elevated: ui.Hex("#242934"),
			borderSubtle: ui.Hex("#272c37"), border: ui.Hex("#343b49"), borderStrong: ui.Hex("#4b5568"),
			text: ui.Hex("#f1f5f9"), muted: ui.Hex("#9ca7b8"), tertiary: ui.Hex("#6c7789"),
			accent: ui.Hex("#2563eb"), accentHover: ui.Hex("#2f6ef0"), accentPressed: ui.Hex("#1d4ed8"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#60a5fa"), success: ui.Hex("#4ade80"), warning: ui.Hex("#fbbf24"), danger: ui.Hex("#f87171"),
			chart: [6]ui.Color{ui.Hex("#60a5fa"), ui.Hex("#4ade80"), ui.Hex("#fbbf24"), ui.Hex("#a78bfa"), ui.Hex("#f87171"), ui.Hex("#94a3b8")},
			buttons: makeButtonPalette(
				ui.Hex("#262b35"), ui.Hex("#303643"), ui.Hex("#3a4252"), ui.Hex("#3e4656"), ui.Hex("#535e73"),
				ui.Hex("#371f24"), ui.Hex("#47252b"), ui.Hex("#582c34"), ui.Hex("#f87171").Alpha(.48), ui.Hex("#fda4af"),
				ui.Hex("#c22b3d"), ui.Hex("#d13648"), ui.Hex("#a62131"),
				ui.Hex("#1c8050"), ui.Hex("#15663f"),
				ui.Hex("#3b82f6").Alpha(.18), ui.Hex("#60a5fa").Alpha(.82),
			),
		},
	}

	indigoAccentSpec = studioAccentSpec{
		id:       core.AccentIndigo,
		labelKey: "settings.appearance.accent.indigo",
		descKey:  "settings.appearance.accent.indigo_desc",
		light: paletteColors{
			background: ui.Hex("#f5f5f7"), sunken: ui.Hex("#efeff1"), surface: ui.Hex("#ffffff"),
			surfaceHover: ui.Hex("#f5f5f7"), surfacePressed: ui.Hex("#ebebee"), elevated: ui.Hex("#ffffff"),
			borderSubtle: ui.Hex("#ececee"), border: ui.Hex("#e0e0e4"), borderStrong: ui.Hex("#c7c7cc"),
			text: ui.Hex("#181818"), muted: ui.Hex("#6a6a6a"), tertiary: ui.Hex("#6a6a6a"),
			accent: ui.Hex("#4338ca"), accentHover: ui.Hex("#3730a3"), accentPressed: ui.Hex("#2e278c"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#2563a8"), success: ui.Hex("#147a52"), warning: ui.Hex("#9a5b08"), danger: ui.Hex("#c4233a"),
			chart: [6]ui.Color{ui.Hex("#4338ca"), ui.Hex("#147a52"), ui.Hex("#2563a8"), ui.Hex("#c26d0a"), ui.Hex("#c4233a"), ui.Hex("#6a6a6a")},
			buttons: makeButtonPalette(
				ui.Hex("#ffffff"), ui.Hex("#f5f5f7"), ui.Hex("#ebebee"), ui.Hex("#dcdce0"), ui.Hex("#c7c7cc"),
				ui.Hex("#fef2f3"), ui.Hex("#fde2e6"), ui.Hex("#fbd0d7"), ui.Hex("#c4233a").Alpha(.42), ui.Hex("#b41f35"),
				ui.Hex("#c4233a"), ui.Hex("#a81c30"), ui.Hex("#8d1627"),
				ui.Hex("#147a52"), ui.Hex("#0e5c3d"),
				ui.Hex("#4338ca").Alpha(.10), ui.Hex("#4338ca").Alpha(.70),
			),
		},
		dark: paletteColors{
			background: ui.Hex("#12131a"), sunken: ui.Hex("#0c0d12"), surface: ui.Hex("#1b1c26"),
			surfaceHover: ui.Hex("#242634"), surfacePressed: ui.Hex("#2d3042"), elevated: ui.Hex("#222432"),
			borderSubtle: ui.Hex("#262838"), border: ui.Hex("#33364a"), borderStrong: ui.Hex("#4a4f6b"),
			text: ui.Hex("#f2f3fa"), muted: ui.Hex("#a0a4bd"), tertiary: ui.Hex("#6f738f"),
			accent: ui.Hex("#4f46e5"), accentHover: ui.Hex("#5b54ee"), accentPressed: ui.Hex("#4338ca"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#60a5fa"), success: ui.Hex("#34d399"), warning: ui.Hex("#fbbf24"), danger: ui.Hex("#fb7185"),
			chart: [6]ui.Color{ui.Hex("#818cf8"), ui.Hex("#34d399"), ui.Hex("#60a5fa"), ui.Hex("#fbbf24"), ui.Hex("#fb7185"), ui.Hex("#9498b3")},
			buttons: makeButtonPalette(
				ui.Hex("#242634"), ui.Hex("#2e3142"), ui.Hex("#393d52"), ui.Hex("#3e4259"), ui.Hex("#555a78"),
				ui.Hex("#371e26"), ui.Hex("#47242f"), ui.Hex("#582c39"), ui.Hex("#fb7185").Alpha(.48), ui.Hex("#fda4af"),
				ui.Hex("#c22943"), ui.Hex("#d1344e"), ui.Hex("#a61f36"),
				ui.Hex("#198057"), ui.Hex("#136645"),
				ui.Hex("#6366f1").Alpha(.20), ui.Hex("#818cf8").Alpha(.82),
			),
		},
	}

	tealAccentSpec = studioAccentSpec{
		id:       core.AccentTeal,
		labelKey: "settings.appearance.accent.teal",
		descKey:  "settings.appearance.accent.teal_desc",
		light: paletteColors{
			background: ui.Hex("#f5f5f7"), sunken: ui.Hex("#efeff1"), surface: ui.Hex("#ffffff"),
			surfaceHover: ui.Hex("#f5f5f7"), surfacePressed: ui.Hex("#ebebee"), elevated: ui.Hex("#ffffff"),
			borderSubtle: ui.Hex("#ececee"), border: ui.Hex("#e0e0e4"), borderStrong: ui.Hex("#c7c7cc"),
			text: ui.Hex("#181818"), muted: ui.Hex("#6a6a6a"), tertiary: ui.Hex("#6a6a6a"),
			accent: ui.Hex("#0f766e"), accentHover: ui.Hex("#0c625b"), accentPressed: ui.Hex("#094f4a"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#1f639c"), success: ui.Hex("#16794b"), warning: ui.Hex("#9a5b08"), danger: ui.Hex("#c02636"),
			chart: [6]ui.Color{ui.Hex("#0f766e"), ui.Hex("#1f639c"), ui.Hex("#c26d0a"), ui.Hex("#704cc7"), ui.Hex("#c02636"), ui.Hex("#6a6a6a")},
			buttons: makeButtonPalette(
				ui.Hex("#ffffff"), ui.Hex("#f5f5f7"), ui.Hex("#ebebee"), ui.Hex("#dcdce0"), ui.Hex("#c7c7cc"),
				ui.Hex("#fef2f3"), ui.Hex("#fde2e5"), ui.Hex("#fbd0d5"), ui.Hex("#c02636").Alpha(.42), ui.Hex("#ad2030"),
				ui.Hex("#c02636"), ui.Hex("#a51e2c"), ui.Hex("#8b1724"),
				ui.Hex("#15784d"), ui.Hex("#0e5b3a"),
				ui.Hex("#0f766e").Alpha(.11), ui.Hex("#0f766e").Alpha(.70),
			),
		},
		dark: paletteColors{
			background: ui.Hex("#141819"), sunken: ui.Hex("#0e1112"), surface: ui.Hex("#1d2224"),
			surfaceHover: ui.Hex("#262c2f"), surfacePressed: ui.Hex("#30383b"), elevated: ui.Hex("#242a2c"),
			borderSubtle: ui.Hex("#262d30"), border: ui.Hex("#343e42"), borderStrong: ui.Hex("#4b595e"),
			text: ui.Hex("#eef4f4"), muted: ui.Hex("#99abab"), tertiary: ui.Hex("#697c7c"),
			accent: ui.Hex("#14b8a6"), accentHover: ui.Hex("#2dd4bf"), accentPressed: ui.Hex("#0d9488"), accentText: ui.Hex("#071a18"),
			info: ui.Hex("#60a5fa"), success: ui.Hex("#4ade80"), warning: ui.Hex("#fbbf24"), danger: ui.Hex("#f87171"),
			chart: [6]ui.Color{ui.Hex("#2dd4bf"), ui.Hex("#60a5fa"), ui.Hex("#fbbf24"), ui.Hex("#a78bfa"), ui.Hex("#f87171"), ui.Hex("#8fa3a3")},
			buttons: makeButtonPalette(
				ui.Hex("#252c2e"), ui.Hex("#2f383b"), ui.Hex("#3a4448"), ui.Hex("#3e4a4e"), ui.Hex("#536368"),
				ui.Hex("#372024"), ui.Hex("#47262b"), ui.Hex("#582d34"), ui.Hex("#f87171").Alpha(.48), ui.Hex("#fda4af"),
				ui.Hex("#c22b3d"), ui.Hex("#d13648"), ui.Hex("#a62131"),
				ui.Hex("#1b8052"), ui.Hex("#146641"),
				ui.Hex("#14b8a6").Alpha(.17), ui.Hex("#2dd4bf").Alpha(.82),
			),
		},
	}

	studioAccentSpecs = []studioAccentSpec{
		cobaltAccentSpec,
		indigoAccentSpec,
		tealAccentSpec,
		terracottaAccentSpec,
		monoAccentSpec,
	}

	activeAccentMu sync.RWMutex
	activeAccentID = core.AccentCobalt
)

func setActiveAccent(accent string) {
	normalized := core.NormalizeAccent(accent)
	activeAccentMu.Lock()
	activeAccentID = normalized
	activeAccentMu.Unlock()
}

func activeAccent() string {
	activeAccentMu.RLock()
	id := activeAccentID
	activeAccentMu.RUnlock()
	return core.NormalizeAccent(id)
}

func studioAccentSpecFor(id string) studioAccentSpec {
	normalized := core.NormalizeAccent(id)
	for _, spec := range studioAccentSpecs {
		if spec.id == normalized {
			return spec
		}
	}
	return studioAccentSpecs[0]
}

func paletteForAccent(accent string, dark bool) paletteColors {
	spec := studioAccentSpecFor(accent)
	if dark {
		return spec.dark
	}
	return spec.light
}
