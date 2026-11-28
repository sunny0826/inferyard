package pages

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"inferyard/studio/internal/core"
)

// settingsViewState groups the settings, appearance (theme/language/tray)
// persistence workers, workspace cleanup and software update state. It is
// embedded anonymously in viewState so the whole package keeps using s.<field>.
type settingsViewState struct {
	cleanArtifactsConfirm                 bool
	cleanArtifactsSeq                     atomic.Uint64
	cleanWorkspaceArtifactsFn             func() (core.WorkspaceCleanupResult, *core.ApiError)
	settingsLeaveConfirm                  bool
	settingsTab                           int
	buildInfo                             func() core.BuildInfo
	tokenEnv, downloadProxy               string
	downloadSource                        string
	downloadSourceSaving                  bool
	downloadSourceMu                      sync.Mutex
	downloadSourceWanted                  string
	downloadSourceWantedSeq               uint64
	downloadSourceWorkerRunning           bool
	persistDownloadSource                 func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	settingsEpochMu                       sync.Mutex
	settingsEpoch                         uint64
	themeMode, appliedThemeMode           string
	themeSaving                           bool
	themeMu                               sync.Mutex
	themeWanted                           string
	themeWantedSeq                        uint64
	themeWorkerRunning                    bool
	applyNativeTheme                      func(string)
	persistTheme                          func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	accentMode                            string
	accentSaving                          bool
	accentMu                              sync.Mutex
	accentWanted                          string
	accentWantedSeq                       uint64
	accentWorkerRunning                   bool
	persistAccent                         func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	fontSizeMode                          string
	fontSizeSaving                        bool
	fontSizeMu                            sync.Mutex
	fontSizeWanted                        string
	fontSizeWantedSeq                     uint64
	fontSizeWorkerRunning                 bool
	persistFontSize                       func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	languageMode, appliedLanguageMode     string
	languageSaving                        bool
	languageMu                            sync.Mutex
	languageWanted                        string
	languageWantedSeq                     uint64
	languageWorkerRunning                 bool
	applyNativeLocale                     func(string)
	persistLanguage                       func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	reasoningEffort                       string
	reasoningSaving                       bool
	reasoningMu                           sync.Mutex
	reasoningWanted                       string
	reasoningWantedSeq                    uint64
	reasoningWorkerRunning                bool
	minimizeToTray, appliedMinimizeToTray bool
	trayUnavailable                       bool
	minimizeToTraySaving                  bool
	minimizeToTrayMu                      sync.Mutex
	minimizeToTrayWanted                  bool
	minimizeToTrayWantedSeq               uint64
	minimizeToTrayWorkerRunning           bool
	minimizeToTrayWorkerDone              chan struct{}
	applyMinimizeToTray                   func(bool) error
	minimizeToTrayNow                     func() error
	persistMinimizeToTray                 func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	keepServicesSaving                    bool
	keepServicesMu                        sync.Mutex
	keepServicesWanted                    bool
	keepServicesWantedSeq                 uint64
	keepServicesWorkerRunning             bool
	keepServicesWorkerDone                chan struct{}
	persistKeepServices                   func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	updateState                           core.UpdateState
	cliUpgrade                            core.CLIUpgradeState
	cliUpgradeDismissedSeq                uint64
	updateDialogOpen                      bool
	updateReadyPromptDismissedVersion     string
	updateCheckFn                         func(context.Context) (core.UpdateState, error)
	updateStartFn                         func()
	updateCancelFn                        func() core.UpdateState
	updateApplyFn                         func(context.Context, bool) (core.UpdateState, error)
}

func (s *viewState) settingsSequence() uint64 {
	s.settingsEpochMu.Lock()
	defer s.settingsEpochMu.Unlock()
	return s.settingsEpoch
}
