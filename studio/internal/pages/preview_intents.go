package pages

import (
	"encoding/json"

	"inferyard/studio/internal/core"
)

type previewIntent interface{ previewIntent() }

type previewStep uint8

const (
	previewPrepareEnvironment previewStep = iota
	previewCheckDevice
	previewStartEngine
	previewStopEngine
	previewBind
	previewRetryBenchmark
	previewRefreshDevice
	previewRefreshDiscovery
	previewReload
)

func (previewStep) previewIntent() {}

type previewManagedEngine struct{ candidate string }

func (previewManagedEngine) previewIntent() {}

type previewStandaloneStart struct {
	model, engine string
	contextSize   int64
	port          int
	lanAccess     bool
	gpuLayers     *int
	threads       *int
	extraArgs     []string
}

func (previewStandaloneStart) previewIntent() {}

type previewSaveServicePreset struct {
	preset core.ServicePreset
}

func (previewSaveServicePreset) previewIntent() {}

type previewDismissExitNotice struct{ id string }

func (previewDismissExitNotice) previewIntent() {}

type previewStopManagedService struct {
	serviceID string
	after     func()
}

func (previewStopManagedService) previewIntent() {}

type previewTestServiceEndpoint struct {
	baseURL, model string
	seq            uint64
}

func (previewTestServiceEndpoint) previewIntent() {}

type previewStopEngineThen struct{ after func() }

func (previewStopEngineThen) previewIntent() {}

type previewBenchmark struct {
	model, engine string
	bundles       []string
	resumePath    string
	reusedService *core.EngineState
}

func (previewBenchmark) previewIntent() {}

type previewDownload struct {
	kind, source, name string
	family, version    string
	releaseID, buildID string
}

func (previewDownload) previewIntent() {}

type previewPolystrataSetup struct{ req core.PolystrataSetupRequest }

func (previewPolystrataSetup) previewIntent() {}

type previewCancelDownload struct{ id string }

func (previewCancelDownload) previewIntent() {}

type previewRetryDownload struct{ id string }

func (previewRetryDownload) previewIntent() {}

type previewSettings struct{ patch map[string]json.RawMessage }

type previewCleanWorkspaceArtifacts struct{}

func (previewCleanWorkspaceArtifacts) previewIntent() {}

type previewBrowsePath struct{ key string }

func (previewBrowsePath) previewIntent() {}

func (previewSettings) previewIntent() {}

type previewInstallation struct{ action, path string }

func (previewInstallation) previewIntent() {}

type previewLocalBinding struct {
	taskID, endpoint string
	pid              int64
}

func (previewLocalBinding) previewIntent() {}

type previewCreateReport struct{ paths []string }

func (previewCreateReport) previewIntent() {}

type previewStartRun struct{ bundle, resumePath string }

func (previewStartRun) previewIntent() {}

type previewCancelRun struct{ id string }

func (previewCancelRun) previewIntent() {}

type previewSpeedStart struct {
	model, engine string
	context       int
}

func (previewSpeedStart) previewIntent() {}

type previewSpeedCancel struct{}

func (previewSpeedCancel) previewIntent() {}

type previewSpeedRead struct{ id string }

func (previewSpeedRead) previewIntent() {}

type previewUpdateCheck struct{}

func (previewUpdateCheck) previewIntent() {}

type previewUpdateStart struct{}

func (previewUpdateStart) previewIntent() {}

type previewUpdateCancel struct{}

func (previewUpdateCancel) previewIntent() {}

type previewUpdateApply struct{ confirmStopWorkloads bool }

func (previewUpdateApply) previewIntent() {}

type previewUpdateRetry struct{}

func (previewUpdateRetry) previewIntent() {}
