package pages

import "encoding/json"

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
	previewVerifyReport
)

func (previewStep) previewIntent() {}

type previewManagedEngine struct{ candidate string }

func (previewManagedEngine) previewIntent() {}

type previewBenchmark struct {
	model, engine string
	bundles       []string
	resumePath    string
}

func (previewBenchmark) previewIntent() {}

type previewDownload struct{ kind, source, name string }

func (previewDownload) previewIntent() {}

type previewCancelDownload struct{ id string }

func (previewCancelDownload) previewIntent() {}

type previewRetryDownload struct{ id string }

func (previewRetryDownload) previewIntent() {}

type previewSettings struct{ patch map[string]json.RawMessage }

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
