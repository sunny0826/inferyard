package core

import "encoding/json"

type ApiError struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	Limitations []string       `json:"limitations"`
	Details     map[string]any `json:"details"`
}

type InferyardProbe struct {
	Path    *string `json:"path"`
	Version *string `json:"version"`
	Source  string  `json:"source"`
	Status  string  `json:"status"`
}

type StatusResponse struct {
	Status          string         `json:"status"`
	Version         string         `json:"version"`
	Inferyard       InferyardProbe `json:"inferyard"`
	ActiveRunTasks  int64          `json:"active_run_tasks"`
	ActiveDownloads int64          `json:"active_downloads"`
}

type UserSettings struct {
	ModelDownloadName  string  `json:"model_download_name"`
	EngineDownloadName string  `json:"engine_download_name"`
	WorkspaceRoot      *string `json:"workspace_root"`
	InferyardPath      *string `json:"inferyard_path"`
	ResultsRoot        *string `json:"results_root"`
	ModelsRoot         *string `json:"models_root"`
}

type EffectiveSettings struct {
	WorkspaceRoot    *string `json:"workspace_root"`
	InferyardPath    *string `json:"inferyard_path"`
	InferyardVersion *string `json:"inferyard_version"`
	InferyardStatus  string  `json:"inferyard_status"`
	ResultsRoot      *string `json:"results_root"`
	ModelsRoot       *string `json:"models_root"`
	ReportsRoot      *string `json:"reports_root"`
}

type SettingsResponse struct {
	Settings  UserSettings      `json:"settings"`
	Effective EffectiveSettings `json:"effective"`
}

type DeviceResponse struct {
	Snapshot   json.RawMessage `json:"snapshot"`
	CapturedAt *string         `json:"captured_at"`
	Refreshing bool            `json:"refreshing"`
	LastError  *ApiError       `json:"last_error"`
}

type RunScore struct {
	Passed int64    `json:"passed"`
	Valid  int64    `json:"valid"`
	Rate   *float64 `json:"rate"`
}

type RunSummary struct {
	RunId        string    `json:"run_id"`
	Path         string    `json:"path"`
	BatchId      *string   `json:"batch_id"`
	Status       string    `json:"status"`
	Sealed       bool      `json:"sealed"`
	Completeness *string   `json:"completeness"`
	StopReason   *string   `json:"stop_reason"`
	ModelName    *string   `json:"model_name"`
	ModelPath    *string   `json:"model_path"`
	Engine       *string   `json:"engine"`
	Bundle       *string   `json:"bundle"`
	Score        *RunScore `json:"score"`
	Relation     *string   `json:"relation"`
	ParentRunId  *string   `json:"parent_run_id"`
	StartedAt    *string   `json:"started_at"`
	FinishedAt   *string   `json:"finished_at"`
	Resumable    bool      `json:"resumable"`
}

type BatchSummary struct {
	BatchId string       `json:"batch_id"`
	Path    string       `json:"path"`
	Runs    []RunSummary `json:"runs"`
}

type RunHistoryResponse struct {
	ResultsRoot    *string        `json:"results_root"`
	ScannedAt      string         `json:"scanned_at"`
	Batches        []BatchSummary `json:"batches"`
	StandaloneRuns []RunSummary   `json:"standalone_runs"`
}

type RunExit struct {
	ExitCode     int64    `json:"exit_code"`
	CliStatus    *string  `json:"cli_status"`
	Completeness *string  `json:"completeness"`
	StopReason   *string  `json:"stop_reason"`
	Limitations  []string `json:"limitations"`
}

type RunTask struct {
	TaskId     string    `json:"task_id"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"`
	Queued     bool      `json:"queued"`
	ConfigPath *string   `json:"config_path"`
	ResumeFrom *string   `json:"resume_from"`
	Bundle     *string   `json:"bundle"`
	RunId      *string   `json:"run_id"`
	RunPath    *string   `json:"run_path"`
	CreatedAt  string    `json:"created_at"`
	StartedAt  *string   `json:"started_at"`
	FinishedAt *string   `json:"finished_at"`
	Exit       *RunExit  `json:"exit"`
	Error      *ApiError `json:"error"`
}

type CaseProgress struct {
	CaseId        string   `json:"case_id"`
	PlanIndex     *int64   `json:"plan_index"`
	State         string   `json:"state"`
	Category      *string  `json:"category"`
	QualityState  *string  `json:"quality_state"`
	StartedAt     *string  `json:"started_at"`
	FinishedAt    *string  `json:"finished_at"`
	DurationMs    *float64 `json:"duration_ms"`
	ErrorCategory *string  `json:"error_category"`
}

type RunProgress struct {
	Phase         *string        `json:"phase"`
	Done          int64          `json:"done"`
	Total         *int64         `json:"total"`
	CurrentCaseId *string        `json:"current_case_id"`
	LastSeq       int64          `json:"last_seq"`
	Cases         []CaseProgress `json:"cases"`
}

type ResourceSample struct {
	Utc                string   `json:"utc"`
	MemAvailableBytes  *int64   `json:"mem_available_bytes"`
	ServiceRssBytes    *int64   `json:"service_rss_bytes"`
	ServiceCpuPercent  *float64 `json:"service_cpu_percent"`
	TemperatureCelsius *float64 `json:"temperature_celsius"`
}

type StartRunsRequest struct {
	ConfigPaths       []string `json:"config_paths"`
	AllowSingleBundle *bool    `json:"allow_single_bundle"`
}

type ResumeRunRequest struct {
	FromRun     string  `json:"from_run"`
	EndpointUrl *string `json:"endpoint_url"`
	ServerPid   *int64  `json:"server_pid"`
}

type RunTaskDetail struct {
	Task      RunTask          `json:"task"`
	Progress  *RunProgress     `json:"progress"`
	Resources []ResourceSample `json:"resources"`
}

type ReportSummary struct {
	ReportId  string    `json:"report_id"`
	Path      string    `json:"path"`
	CreatedAt *string   `json:"created_at"`
	RunIds    []string  `json:"run_ids"`
	RunPaths  []*string `json:"run_paths"`
	HtmlUrl   string    `json:"html_url"`
}

type CreateReportRequest struct {
	RunPaths []string `json:"run_paths"`
}

type DownloadResult struct {
	Bytes  int64  `json:"bytes"`
	Sha256 string `json:"sha256"`
}

type DownloadTask struct {
	Kind            string          `json:"kind"`
	ProgressReason  *string         `json:"progress_reason"`
	EnginePath      *string         `json:"engine_path"`
	TaskId          string          `json:"task_id"`
	State           string          `json:"state"`
	SourceUrl       string          `json:"source_url"`
	OutName         string          `json:"out_name"`
	OutDir          string          `json:"out_dir"`
	FileName        *string         `json:"file_name"`
	DownloadedBytes int64           `json:"downloaded_bytes"`
	BytesPerSecond  *float64        `json:"bytes_per_second"`
	StartedAt       string          `json:"started_at"`
	FinishedAt      *string         `json:"finished_at"`
	Result          *DownloadResult `json:"result"`
	Error           *ApiError       `json:"error"`
}

type CreateDownloadRequest struct {
	SourceUrl string `json:"source_url"`
	OutName   string `json:"out_name"`
}

// MarshalJSON preserves the native model counters while representing unobserved
// engine transfer measurements as null on the event and JSON boundary.
func (t DownloadTask) MarshalJSON() ([]byte, error) {
	type task DownloadTask
	var downloaded *int64
	var rate *float64
	if t.Kind != "engine" {
		downloaded = &t.DownloadedBytes
		rate = t.BytesPerSecond
	}
	return json.Marshal(struct {
		task
		DownloadedBytes *int64   `json:"downloaded_bytes"`
		BytesPerSecond  *float64 `json:"bytes_per_second"`
	}{task(t), downloaded, rate})
}
