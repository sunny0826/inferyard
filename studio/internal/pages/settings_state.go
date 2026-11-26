package pages

import (
	"context"
	"encoding/json"
	"sync"

	"inferyard/studio/internal/core"
)

// settingsViewState groups the settings, appearance (theme/language/tray)
// persistence workers, workspace cleanup and software update state. It is
// embedded anonymously in viewState so the whole package keeps using s.<field>.
type settingsViewState struct {
	cleanArtifactsConfirm                 bool
	cleanWorkspaceArtifactsFn             func() (core.WorkspaceCleanupResult, *core.ApiError)
	settingsLeaveConfirm                  bool
	settingsTab                           int
	buildInfo                             func() core.BuildInfo
	tokenEnv, downloadProxy               string
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
	languageMode, appliedLanguageMode     string
	languageSaving                        bool
	languageMu                            sync.Mutex
	languageWanted                        string
	languageWantedSeq                     uint64
	languageWorkerRunning                 bool
	applyNativeLocale                     func(string)
	persistLanguage                       func(map[string]json.RawMessage) (core.SettingsResponse, *core.ApiError)
	minimizeToTray, appliedMinimizeToTray bool
	trayUnavailable                       bool
	minimizeToTraySaving                  bool
	minimizeToTrayMu                      sync.Mutex
	minimizeToTrayWanted                  bool
	minimizeToTrayWantedSeq               uint64
	minimizeToTrayWorkerRunning           bool
	minimizeToTrayWorkerDone              chan struct{}
	applyMinimizeToTray                   func(bool) error
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
