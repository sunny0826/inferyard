package pages

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"inferyard/studio/internal/core"
	"inferyard/studio/internal/l10n"
)

func TestDownloadSourceSettingsDefaultsAndDownloadsSync(t *testing.T) {
	s := settingsState()
	st := s.ensureDownloadsUI()
	if st.modelSource != core.DownloadSourceModelScope {
		t.Fatalf("initial downloads modelSource = %q, want %q", st.modelSource, core.DownloadSourceModelScope)
	}
	if st.polystrataModelSource != core.DownloadSourceModelScope {
		t.Fatalf("initial downloads polystrataModelSource = %q, want %q", st.polystrataModelSource, core.DownloadSourceModelScope)
	}

	saved := make(chan string, 2)
	s.persistDownloadSource = func(patch map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		var src string
		_ = json.Unmarshal(patch["download_source"], &src)
		saved <- src
		next := s.settings
		next.Settings.DownloadSource = src
		next.Effective.DownloadSource = src
		return next, nil
	}

	tt := ui.NewTester(s.downloadSourceSettingsView, 800, 300)
	tt.SetLocale(l10n.LanguageZhHans)
	tt.Frame()
	if !tt.HasText("默认下载源") || !tt.HasText("ModelScope") || !tt.HasText("HuggingFace") {
		t.Fatalf("expected Chinese download source settings labels, got %v", tt.Texts())
	}

	click(t, tt, "HuggingFace")
	select {
	case got := <-saved:
		if got != core.DownloadSourceHuggingFace {
			t.Fatalf("persisted download source = %q, want %q", got, core.DownloadSourceHuggingFace)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for download source persist")
	}

	if s.effectiveDownloadSource() != core.DownloadSourceHuggingFace {
		t.Fatalf("effectiveDownloadSource = %q, want %q", s.effectiveDownloadSource(), core.DownloadSourceHuggingFace)
	}
	if st.modelSource != core.DownloadSourceHuggingFace || st.polystrataModelSource != core.DownloadSourceHuggingFace {
		t.Fatalf("downloads UI did not follow updated default source: direct=%q polystrata=%q", st.modelSource, st.polystrataModelSource)
	}

	// Manual override on downloads page must not be clobbered by background settings refresh.
	st.setPolystrataModelSource("auto")
	s.loadDownloadSourceSettings(core.SettingsResponse{
		Settings:  core.UserSettings{DownloadSource: core.DownloadSourceHuggingFace},
		Effective: core.EffectiveSettings{DownloadSource: core.DownloadSourceHuggingFace},
	})
	if st.polystrataModelSource != "auto" {
		t.Fatalf("background settings refresh overwrote manual polystrata source override: %q", st.polystrataModelSource)
	}

	// Explicit change in settings page updates downloads UI again.
	click(t, tt, "ModelScope")
	select {
	case got := <-saved:
		if got != core.DownloadSourceModelScope {
			t.Fatalf("persisted download source = %q, want %q", got, core.DownloadSourceModelScope)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second download source persist")
	}
	if st.modelSource != core.DownloadSourceModelScope || st.polystrataModelSource != core.DownloadSourceModelScope {
		t.Fatalf("explicit settings change did not update downloads UI: direct=%q polystrata=%q", st.modelSource, st.polystrataModelSource)
	}

	tt.SetLocale(l10n.LanguageEn)
	tt.Frame()
	if !tt.HasText("Default Download Source") || !tt.HasText("ModelScope") || !tt.HasText("HuggingFace") {
		t.Fatalf("expected English download source settings labels, got %v", tt.Texts())
	}
}

func TestDownloadSourceSettingsRevertsOnPersistFailure(t *testing.T) {
	s := settingsState()
	s.selected = "settings"
	s.settings.Settings.DownloadSource = core.DownloadSourceModelScope
	s.settings.Effective.DownloadSource = core.DownloadSourceModelScope
	st := s.ensureDownloadsUI()

	done := make(chan struct{})
	s.updateWindow = func(fn func()) {
		fn()
		close(done)
	}
	s.persistDownloadSource = func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError) {
		return core.SettingsResponse{}, core.Err("internal_error")
	}

	s.changeDownloadSource(core.DownloadSourceHuggingFace)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for failure rollback")
	}

	if s.effectiveDownloadSource() != core.DownloadSourceModelScope {
		t.Fatalf("expected rollback to modelscope, got %q", s.effectiveDownloadSource())
	}
	if st.modelSource != core.DownloadSourceModelScope || st.polystrataModelSource != core.DownloadSourceModelScope {
		t.Fatalf("expected downloads UI rollback to modelscope, got direct=%q polystrata=%q", st.modelSource, st.polystrataModelSource)
	}
	if s.failure == nil || s.failure.Code != "internal_error" {
		t.Fatalf("expected failure error on rollback, got %+v", s.failure)
	}
}
