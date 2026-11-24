package l10n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"testing/fstest"

	mygoi18n "github.com/egoist/mygo/i18n"
	_ "github.com/egoist/mygo/locales/zh-Hans"
)

const (
	LanguageSystem = "system"
	LanguageEn     = "en"
	LanguageZhHans = "zh-Hans"
)

//go:embed locales/*/*.json
var localeFiles embed.FS

var (
	catalog     *mygoi18n.Catalog
	mergedData  map[string]map[string]string
	overrideMu  sync.RWMutex
	overrideTag string
)

func init() {
	cat, data, err := loadCatalog(localeFiles)
	if err != nil {
		panic(fmt.Sprintf("l10n: failed to load embedded catalogs: %v", err))
	}
	catalog = cat
	mergedData = data
	mygoi18n.Register(catalog)
}

func loadCatalog(fsys fs.FS) (*mygoi18n.Catalog, map[string]map[string]string, error) {
	entries, err := fs.Glob(fsys, "locales/*/*.json")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(entries)

	byLang := map[string]map[string]string{}
	seenSource := map[string]map[string]string{}

	for _, file := range entries {
		base := path.Base(file)
		lang := strings.TrimSuffix(base, ".json")
		raw, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", file, err)
		}
		var doc map[string]string
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", file, err)
		}
		if byLang[lang] == nil {
			byLang[lang] = map[string]string{}
			seenSource[lang] = map[string]string{}
		}
		for k, v := range doc {
			if strings.HasPrefix(k, "@") {
				continue
			}
			if prev, exists := seenSource[lang][k]; exists {
				return nil, nil, fmt.Errorf("duplicate key %q for %s in %s and %s", k, lang, prev, file)
			}
			seenSource[lang][k] = file
			byLang[lang][k] = v
		}
	}

	memFS := fstest.MapFS{}
	for lang, msgs := range byLang {
		payload := make(map[string]string, len(msgs)+1)
		payload["@@locale"] = lang
		for k, v := range msgs {
			payload[k] = v
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal merged %s: %w", lang, err)
		}
		memFS[lang+".json"] = &fstest.MapFile{Data: encoded}
	}

	cat, err := mygoi18n.NewCatalog(memFS, LanguageEn)
	if err != nil {
		return nil, nil, err
	}
	return cat, byLang, nil
}

// SupportedLanguages returns the BCP 47 tags shipped by Studio (source first).
func SupportedLanguages() []string {
	return []string{LanguageEn, LanguageZhHans}
}

// Catalog returns the loaded MyGo i18n catalog.
func Catalog() *mygoi18n.Catalog {
	return catalog
}

// NormalizeLanguage normalizes a persisted language preference into
// "system", "en", or "zh-Hans". Unknown values normalize to "system".
func NormalizeLanguage(value string) string {
	switch strings.TrimSpace(value) {
	case "", LanguageSystem, "auto":
		return LanguageSystem
	}
	norm := mygoi18n.Normalize(value)
	if norm == "" {
		return LanguageSystem
	}
	switch {
	case norm == LanguageEn || strings.HasPrefix(norm, "en-"):
		return LanguageEn
	case norm == LanguageZhHans || norm == "zh" || norm == "zh-CN" || norm == "zh-SG" || strings.HasPrefix(norm, "zh-Hans-"):
		return LanguageZhHans
	default:
		return LanguageSystem
	}
}

// ParseLanguageSetting validates a user-supplied language setting patch value.
// Empty string or "system" maps to LanguageSystem.
func ParseLanguageSetting(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == LanguageSystem {
		return LanguageSystem, true
	}
	norm := mygoi18n.Normalize(trimmed)
	switch {
	case norm == LanguageEn || strings.HasPrefix(norm, "en-"):
		return LanguageEn, true
	case norm == LanguageZhHans || norm == "zh" || norm == "zh-CN" || norm == "zh-SG" || strings.HasPrefix(norm, "zh-Hans-"):
		return LanguageZhHans, true
	default:
		return "", false
	}
}

// LocaleForSetting converts a persisted language setting into the tag passed
// to mygo.App.SetLocale ("" means follow system preferred languages).
func LocaleForSetting(setting string) string {
	switch NormalizeLanguage(setting) {
	case LanguageEn:
		return LanguageEn
	case LanguageZhHans:
		return LanguageZhHans
	default:
		return ""
	}
}

// ResolvedLanguageName returns the display name of the resolved UI language in
// its own or active locale (e.g. "English" or "简体中文").
func ResolvedLanguageName(locale string) string {
	resolved := resolveDisplayLocale(locale)
	switch resolved {
	case LanguageZhHans:
		return "简体中文"
	default:
		return "English"
	}
}

func resolveDisplayLocale(locale string) string {
	if strings.TrimSpace(locale) == "" {
		locale = CurrentLocale()
	}
	norm := mygoi18n.Normalize(locale)
	if norm == "" {
		return LanguageEn
	}
	if mygoi18n.IsPseudo(norm) {
		return norm
	}
	if matched := mygoi18n.Match([]string{norm}, SupportedLanguages()); matched != "" {
		return matched
	}
	return LanguageEn
}

// CurrentLocale returns the active UI locale tag for non-frame callers
// (core, desktop tray, notifications, async callbacks).
func CurrentLocale() string {
	overrideMu.RLock()
	tag := overrideTag
	overrideMu.RUnlock()
	if tag != "" {
		return tag
	}
	if loc := strings.TrimSpace(mygoi18n.Locale()); loc != "" {
		return loc
	}
	return LanguageEn
}

// SetTestLocale overrides CurrentLocale for deterministic unit tests and
// returns a restore function.
func SetTestLocale(locale string) func() {
	overrideMu.Lock()
	prev := overrideTag
	overrideTag = strings.TrimSpace(locale)
	overrideMu.Unlock()
	return func() {
		overrideMu.Lock()
		overrideTag = prev
		overrideMu.Unlock()
	}
}

// T formats a message without arguments for the given locale.
// If locale is empty, CurrentLocale() is used.
func T(locale, key string) string {
	if locale == "" {
		locale = CurrentLocale()
	}
	return catalog.Format(locale, key, nil)
}

// Tf formats a message with alternating key, value placeholder pairs for the
// given locale. If locale is empty, CurrentLocale() is used.
func Tf(locale, key string, kv ...any) string {
	if locale == "" {
		locale = CurrentLocale()
	}
	if len(kv) == 0 {
		return catalog.Format(locale, key, nil)
	}
	args := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if name, ok := kv[i].(string); ok {
			args[name] = kv[i+1]
		}
	}
	return catalog.Format(locale, key, args)
}

// TR formats a message without arguments using CurrentLocale().
func TR(key string) string {
	return T("", key)
}

// TRf formats a message with alternating key, value placeholder pairs using CurrentLocale().
func TRf(key string, kv ...any) string {
	return Tf("", key, kv...)
}
