package core

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestLanguageSettingPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	settingsFile := filepath.Join(dir, "settings.json")
	svc, err := New(context.Background(), Options{SettingsPath: settingsFile})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := svc.Language(); got != LanguageSystem {
		t.Fatalf("default Language() = %q, want %q", got, LanguageSystem)
	}
	if got := svc.Settings().Effective.Language; got != LanguageSystem {
		t.Fatalf("default Effective.Language = %q, want %q", got, LanguageSystem)
	}

	rawEn, _ := json.Marshal(LanguageEn)
	resp, apiErr := svc.UpdateSettings(map[string]json.RawMessage{"language": rawEn})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(en): %+v", apiErr)
	}
	if resp.Settings.Language != LanguageEn || resp.Effective.Language != LanguageEn {
		t.Fatalf("unexpected en response: %+v", resp)
	}
	if got := svc.Language(); got != LanguageEn {
		t.Fatalf("svc.Language() = %q, want %q", got, LanguageEn)
	}

	// Reload from disk to verify persistence.
	reloaded, err := New(context.Background(), Options{SettingsPath: settingsFile})
	if err != nil {
		t.Fatalf("reload New: %v", err)
	}
	if got := reloaded.Language(); got != LanguageEn {
		t.Fatalf("reloaded Language() = %q, want %q", got, LanguageEn)
	}

	// Accept zh-CN alias and normalize to zh-Hans.
	rawZh, _ := json.Marshal("zh-CN")
	resp, apiErr = reloaded.UpdateSettings(map[string]json.RawMessage{"language": rawZh})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(zh-CN): %+v", apiErr)
	}
	if resp.Settings.Language != LanguageZhHans || resp.Effective.Language != LanguageZhHans {
		t.Fatalf("unexpected zh-Hans response: %+v", resp)
	}

	// Reject unsupported language tag.
	rawBad, _ := json.Marshal("fr-FR")
	if _, apiErr := reloaded.UpdateSettings(map[string]json.RawMessage{"language": rawBad}); apiErr == nil || apiErr.Code != "invalid_request" {
		t.Fatalf("expected invalid_request for unsupported language, got %+v", apiErr)
	}

	// Reset to system via null.
	resp, apiErr = reloaded.UpdateSettings(map[string]json.RawMessage{"language": json.RawMessage("null")})
	if apiErr != nil {
		t.Fatalf("UpdateSettings(null): %+v", apiErr)
	}
	if resp.Settings.Language != LanguageSystem || resp.Effective.Language != LanguageSystem {
		t.Fatalf("expected system reset, got %+v", resp)
	}
}
