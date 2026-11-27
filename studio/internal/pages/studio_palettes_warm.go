package pages

import (
	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
)

var (
	terracottaAccentSpec = studioAccentSpec{
		id:       core.AccentTerracotta,
		labelKey: "settings.appearance.accent.terracotta",
		descKey:  "settings.appearance.accent.terracotta_desc",
		light: paletteColors{
			background: ui.Hex("#f5f5f7"), sunken: ui.Hex("#efeff1"), surface: ui.Hex("#ffffff"),
			surfaceHover: ui.Hex("#f5f5f7"), surfacePressed: ui.Hex("#ebebee"), elevated: ui.Hex("#ffffff"),
			borderSubtle: ui.Hex("#ececee"), border: ui.Hex("#e0e0e4"), borderStrong: ui.Hex("#c7c7cc"),
			text: ui.Hex("#181818"), muted: ui.Hex("#6a6a6a"), tertiary: ui.Hex("#6a6a6a"),
			accent: ui.Hex("#b8441e"), accentHover: ui.Hex("#a13a18"), accentPressed: ui.Hex("#882f12"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#2b5e8f"), success: ui.Hex("#1c7852"), warning: ui.Hex("#9d5b10"), danger: ui.Hex("#b42d38"),
			chart: [6]ui.Color{ui.Hex("#b8441e"), ui.Hex("#1c7852"), ui.Hex("#2b5e8f"), ui.Hex("#b57416"), ui.Hex("#7b5399"), ui.Hex("#6a6a6a")},
			buttons: makeButtonPalette(
				ui.Hex("#ffffff"), ui.Hex("#f5f5f7"), ui.Hex("#ebebee"), ui.Hex("#dcdce0"), ui.Hex("#c7c7cc"),
				ui.Hex("#fdf1f1"), ui.Hex("#fae0e1"), ui.Hex("#f5cbcd"), ui.Hex("#b42d38").Alpha(.42), ui.Hex("#9f232e"),
				ui.Hex("#b42d38"), ui.Hex("#99242e"), ui.Hex("#7f1d25"),
				ui.Hex("#1c7852"), ui.Hex("#145b3e"),
				ui.Hex("#b8441e").Alpha(.10), ui.Hex("#b8441e").Alpha(.70),
			),
		},
		dark: paletteColors{
			background: ui.Hex("#1b1917"), sunken: ui.Hex("#131210"), surface: ui.Hex("#24211e"),
			surfaceHover: ui.Hex("#2e2a26"), surfacePressed: ui.Hex("#38332e"), elevated: ui.Hex("#2c2824"),
			borderSubtle: ui.Hex("#302c27"), border: ui.Hex("#403b34"), borderStrong: ui.Hex("#595248"),
			text: ui.Hex("#f2eee7"), muted: ui.Hex("#b3aba0"), tertiary: ui.Hex("#7d756a"),
			accent: ui.Hex("#e07b4f"), accentHover: ui.Hex("#ea8e66"), accentPressed: ui.Hex("#cd693e"), accentText: ui.Hex("#1c120d"),
			info: ui.Hex("#79addc"), success: ui.Hex("#69c69d"), warning: ui.Hex("#e6b25c"), danger: ui.Hex("#e88088"),
			chart: [6]ui.Color{ui.Hex("#e07b4f"), ui.Hex("#69c69d"), ui.Hex("#79addc"), ui.Hex("#e6b25c"), ui.Hex("#bc9be0"), ui.Hex("#a69e92")},
			buttons: makeButtonPalette(
				ui.Hex("#2d2925"), ui.Hex("#37322d"), ui.Hex("#423c36"), ui.Hex("#49433b"), ui.Hex("#61594e"),
				ui.Hex("#392223"), ui.Hex("#48292a"), ui.Hex("#573132"), ui.Hex("#e88088").Alpha(.48), ui.Hex("#f4a4aa"),
				ui.Hex("#bc303c"), ui.Hex("#ca3a46"), ui.Hex("#a12530"),
				ui.Hex("#217e58"), ui.Hex("#196446"),
				ui.Hex("#e07b4f").Alpha(.17), ui.Hex("#e07b4f").Alpha(.82),
			),
		},
	}

	monoAccentSpec = studioAccentSpec{
		id:       core.AccentMono,
		labelKey: "settings.appearance.accent.mono",
		descKey:  "settings.appearance.accent.mono_desc",
		light: paletteColors{
			background: ui.Hex("#f5f5f7"), sunken: ui.Hex("#efeff1"), surface: ui.Hex("#ffffff"),
			surfaceHover: ui.Hex("#f5f5f7"), surfacePressed: ui.Hex("#ebebee"), elevated: ui.Hex("#ffffff"),
			borderSubtle: ui.Hex("#ececee"), border: ui.Hex("#e0e0e4"), borderStrong: ui.Hex("#c7c7cc"),
			text: ui.Hex("#181818"), muted: ui.Hex("#6a6a6a"), tertiary: ui.Hex("#6a6a6a"),
			accent: ui.Hex("#181818"), accentHover: ui.Hex("#323236"), accentPressed: ui.Hex("#0a0a0c"), accentText: ui.Hex("#ffffff"),
			info: ui.Hex("#235ea6"), success: ui.Hex("#16794b"), warning: ui.Hex("#94590a"), danger: ui.Hex("#be2436"),
			chart: [6]ui.Color{ui.Hex("#235ea6"), ui.Hex("#16794b"), ui.Hex("#c26d0a"), ui.Hex("#6d4aa6"), ui.Hex("#be2436"), ui.Hex("#6a6a6a")},
			buttons: makeButtonPalette(
				ui.Hex("#ffffff"), ui.Hex("#f5f5f7"), ui.Hex("#ebebee"), ui.Hex("#dcdce0"), ui.Hex("#c7c7cc"),
				ui.Hex("#fef2f3"), ui.Hex("#fde2e5"), ui.Hex("#fbd0d5"), ui.Hex("#be2436").Alpha(.45), ui.Hex("#ae1f30"),
				ui.Hex("#be2436"), ui.Hex("#a31c2c"), ui.Hex("#891624"),
				ui.Hex("#16794b"), ui.Hex("#0f5434"),
				ui.Hex("#181818").Alpha(.08), ui.Hex("#2563eb").Alpha(.72),
			),
		},
		dark: paletteColors{
			background: ui.Hex("#17181b"), sunken: ui.Hex("#101114"), surface: ui.Hex("#202227"),
			surfaceHover: ui.Hex("#292c33"), surfacePressed: ui.Hex("#333740"), elevated: ui.Hex("#272a30"),
			borderSubtle: ui.Hex("#2a2d34"), border: ui.Hex("#393d46"), borderStrong: ui.Hex("#525763"),
			text: ui.Hex("#f2f3f5"), muted: ui.Hex("#a3a7b0"), tertiary: ui.Hex("#717682"),
			accent: ui.Hex("#e4e7ec"), accentHover: ui.Hex("#f3f4f7"), accentPressed: ui.Hex("#cfd4dd"), accentText: ui.Hex("#14161a"),
			info: ui.Hex("#70aef2"), success: ui.Hex("#56cf96"), warning: ui.Hex("#edb84d"), danger: ui.Hex("#f78c94"),
			chart: [6]ui.Color{ui.Hex("#70aef2"), ui.Hex("#56cf96"), ui.Hex("#edb84d"), ui.Hex("#b59be8"), ui.Hex("#f78c94"), ui.Hex("#9aa0ab")},
			buttons: makeButtonPalette(
				ui.Hex("#2a2d34"), ui.Hex("#343841"), ui.Hex("#3f444f"), ui.Hex("#434854"), ui.Hex("#596070"),
				ui.Hex("#392125"), ui.Hex("#49272d"), ui.Hex("#592e36"), ui.Hex("#f78c94").Alpha(.48), ui.Hex("#fba4ab"),
				ui.Hex("#c22b3d"), ui.Hex("#d13648"), ui.Hex("#a62131"),
				ui.Hex("#1e8253"), ui.Hex("#176842"),
				ui.Hex("#e4e7ec").Alpha(.13), ui.Hex("#60a5fa").Alpha(.82),
			),
		},
	}
)
