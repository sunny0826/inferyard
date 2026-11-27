package core

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"inferyard/studio/internal/l10n"
)

func (s *Service) allModelsRoots(u UserSettings) []string {
	return activeModelRoots(u, s.paths)
}

func modelsRootCLIArgs(roots []string) []string {
	args := make([]string, 0, len(roots)*2)
	for _, root := range roots {
		args = append(args, "--models-root", root)
	}
	return args
}

func normalizeExtraModelsRoots(primary *string, raw []string) ([]string, *ApiError) {
	if raw == nil {
		return nil, Err("invalid_request")
	}
	var primaryKey string
	if primary != nil && strings.TrimSpace(*primary) != "" {
		primaryKey = filepath.Clean(strings.TrimSpace(*primary))
		if c, err := canonical(primaryKey); err == nil {
			primaryKey = c
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || !filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, 0) {
			return nil, Err("invalid_request")
		}
		clean := filepath.Clean(trimmed)
		key := clean
		if c, err := canonical(clean); err == nil {
			key = c
		}
		if key == primaryKey || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, clean)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func slicesEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func NormalizeTheme(mode string) string {
	switch strings.TrimSpace(mode) {
	case ThemeLight:
		return ThemeLight
	case ThemeDark:
		return ThemeDark
	default:
		return ThemeSystem
	}
}

// Theme returns the normalized persisted UI appearance mode without probing the CLI.
func (s *Service) Theme() string {
	return NormalizeTheme(s.userSettings().Theme)
}

func NormalizeAccent(accent string) string {
	switch strings.TrimSpace(accent) {
	case AccentIndigo:
		return AccentIndigo
	case AccentTeal:
		return AccentTeal
	case AccentTerracotta:
		return AccentTerracotta
	case AccentMono:
		return AccentMono
	default:
		return AccentCobalt
	}
}

// Accent returns the normalized persisted UI accent palette without probing the CLI.
func (s *Service) Accent() string {
	return NormalizeAccent(s.userSettings().Accent)
}

func NormalizeLanguage(mode string) string {
	return l10n.NormalizeLanguage(mode)
}

// Language returns the normalized persisted UI language preference without probing the CLI.
func (s *Service) Language() string {
	return NormalizeLanguage(s.userSettings().Language)
}

func ParseReasoningEffort(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", ReasoningEffortNone:
		return ReasoningEffortNone, true
	case ReasoningEffortLow:
		return ReasoningEffortLow, true
	case ReasoningEffortMedium:
		return ReasoningEffortMedium, true
	case ReasoningEffortHigh:
		return ReasoningEffortHigh, true
	default:
		return "", false
	}
}

func NormalizeReasoningEffort(raw string) string {
	if normalized, ok := ParseReasoningEffort(raw); ok {
		return normalized
	}
	return ReasoningEffortNone
}

// ReasoningEffort returns the normalized persisted benchmark reasoning effort without probing the CLI.
func (s *Service) ReasoningEffort() string {
	return NormalizeReasoningEffort(s.userSettings().ReasoningEffort)
}

func ParseDownloadSource(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", DownloadSourceModelScope:
		return DownloadSourceModelScope, true
	case DownloadSourceHuggingFace:
		return DownloadSourceHuggingFace, true
	default:
		return "", false
	}
}

func NormalizeDownloadSource(raw string) string {
	if normalized, ok := ParseDownloadSource(raw); ok {
		return normalized
	}
	return DownloadSourceModelScope
}

// DownloadSource returns the normalized persisted default model download source without probing the CLI.
func (s *Service) DownloadSource() string {
	return NormalizeDownloadSource(s.userSettings().DownloadSource)
}

// MinimizeToTray returns whether closing the window minimizes to the system tray.
func (s *Service) MinimizeToTray() bool {
	return s.userSettings().MinimizeToTray
}

// EffectiveKeepServicesOnQuitValue resolves the effective keep-services-on-quit setting (defaults to true).
func EffectiveKeepServicesOnQuitValue(u UserSettings) bool {
	if u.KeepServicesOnQuit == nil {
		return true
	}
	return *u.KeepServicesOnQuit
}

// EffectiveKeepServicesOnQuit returns whether standalone services stay alive when Studio quits.
func (s *Service) EffectiveKeepServicesOnQuit() bool {
	return EffectiveKeepServicesOnQuitValue(s.userSettings())
}

func (s *Service) applySettingsPatchExtra(u *UserSettings, key string, raw json.RawMessage) (bool, *ApiError) {
	switch key {
	case "download_source":
		var source *string
		if json.Unmarshal(raw, &source) != nil {
			return true, Err("invalid_request")
		}
		if source == nil {
			u.DownloadSource = DownloadSourceModelScope
			return true, nil
		}
		normalized, ok := ParseDownloadSource(*source)
		if !ok {
			return true, Err("invalid_request")
		}
		u.DownloadSource = normalized
		return true, nil
	case "download_proxy":
		var proxy *string
		if json.Unmarshal(raw, &proxy) != nil {
			return true, Err("invalid_request")
		}
		if proxy == nil || strings.TrimSpace(*proxy) == "" {
			u.DownloadProxy = nil
			return true, nil
		}
		trimmed := strings.TrimRight(strings.TrimSpace(*proxy), "/")
		if !ValidateDownloadProxy(trimmed) {
			return true, Err("invalid_request")
		}
		u.DownloadProxy = &trimmed
		return true, nil
	case "theme":
		var mode *string
		if json.Unmarshal(raw, &mode) != nil {
			return true, Err("invalid_request")
		}
		if mode == nil {
			u.Theme = ThemeSystem
			return true, nil
		}
		trimmed := strings.TrimSpace(*mode)
		switch trimmed {
		case "", ThemeSystem:
			u.Theme = ThemeSystem
		case ThemeLight, ThemeDark:
			u.Theme = trimmed
		default:
			return true, Err("invalid_request")
		}
		return true, nil
	case "accent":
		var accent *string
		if json.Unmarshal(raw, &accent) != nil {
			return true, Err("invalid_request")
		}
		if accent == nil {
			u.Accent = AccentCobalt
			return true, nil
		}
		trimmed := strings.TrimSpace(*accent)
		switch trimmed {
		case "", AccentCobalt:
			u.Accent = AccentCobalt
		case AccentIndigo, AccentTeal, AccentTerracotta, AccentMono:
			u.Accent = trimmed
		default:
			return true, Err("invalid_request")
		}
		return true, nil
	case "language":
		var lang *string
		if json.Unmarshal(raw, &lang) != nil {
			return true, Err("invalid_request")
		}
		if lang == nil {
			u.Language = LanguageSystem
			return true, nil
		}
		normalized, ok := l10n.ParseLanguageSetting(*lang)
		if !ok {
			return true, Err("invalid_request")
		}
		u.Language = normalized
		return true, nil
	case "reasoning_effort":
		var effort *string
		if json.Unmarshal(raw, &effort) != nil {
			return true, Err("invalid_request")
		}
		if effort == nil {
			u.ReasoningEffort = ReasoningEffortNone
			return true, nil
		}
		normalized, ok := ParseReasoningEffort(*effort)
		if !ok {
			return true, Err("invalid_request")
		}
		u.ReasoningEffort = normalized
		return true, nil
	case "minimize_to_tray":
		var enabled *bool
		if json.Unmarshal(raw, &enabled) != nil {
			return true, Err("invalid_request")
		}
		u.MinimizeToTray = enabled != nil && *enabled
		return true, nil
	case "keep_services_on_quit":
		var enabled *bool
		if json.Unmarshal(raw, &enabled) != nil {
			return true, Err("invalid_request")
		}
		u.KeepServicesOnQuit = enabled
		return true, nil
	case "extra_models_roots":
		var roots []string
		if json.Unmarshal(raw, &roots) != nil || roots == nil {
			return true, Err("invalid_request")
		}
		normalized, err := normalizeExtraModelsRoots(s.modelsRoot(*u), roots)
		if err != nil {
			return true, err
		}
		u.ExtraModelsRoots = normalized
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) AddExtraModelsRoot(dir string) (SettingsResponse, *ApiError) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || !filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.paths != nil {
		return SettingsResponse{}, Conflict("paths_bound")
	}
	u := s.userSettings()
	combined := append([]string(nil), u.ExtraModelsRoots...)
	combined = append(combined, trimmed)
	normalized, err := normalizeExtraModelsRoots(s.modelsRoot(u), combined)
	if err != nil {
		return SettingsResponse{}, err
	}
	u.ExtraModelsRoots = normalized
	return s.saveSettings(u)
}

func (s *Service) RemoveExtraModelsRoot(dir string) (SettingsResponse, *ApiError) {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" || strings.ContainsRune(trimmed, 0) {
		return SettingsResponse{}, Err("invalid_request")
	}
	targetClean := filepath.Clean(trimmed)
	targetKey := targetClean
	if c, err := canonical(targetClean); err == nil {
		targetKey = c
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.paths != nil {
		return SettingsResponse{}, Conflict("paths_bound")
	}
	u := s.userSettings()
	kept := make([]string, 0, len(u.ExtraModelsRoots))
	for _, root := range u.ExtraModelsRoots {
		key := filepath.Clean(root)
		if c, err := canonical(key); err == nil {
			key = c
		}
		if key != targetKey {
			kept = append(kept, root)
		}
	}
	if len(kept) == 0 {
		u.ExtraModelsRoots = nil
	} else {
		u.ExtraModelsRoots = kept
	}
	return s.saveSettings(u)
}
