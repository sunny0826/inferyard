package core

import (
	"encoding/json"
	"inferyard/studio/internal/l10n"
)

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

const (
	ThemeSystem = "system"
	ThemeLight  = "light"
	ThemeDark   = "dark"

	AccentCobalt     = "cobalt"
	AccentIndigo     = "indigo"
	AccentTeal       = "teal"
	AccentTerracotta = "terracotta"
	AccentMono       = "mono"

	FontSizeSmaller = "smaller"
	FontSizeSmall   = "small"
	FontSizeDefault = "default"
	FontSizeLarge   = "large"
	FontSizeLarger  = "larger"

	LanguageSystem = "system"
	LanguageEn     = "en"
	LanguageZhHans = "zh-Hans"

	ReasoningEffortNone   = "none"
	ReasoningEffortLow    = "low"
	ReasoningEffortMedium = "medium"
	ReasoningEffortHigh   = "high"

	DownloadSourceModelScope  = "modelscope"
	DownloadSourceHuggingFace = "huggingface"
)

type UserSettings struct {
	EngineInstallations []string `json:"engine_installations,omitempty"`
	DownloadSource      string   `json:"download_source,omitempty"`
	DownloadProxy       *string  `json:"download_proxy,omitempty"`
	Theme               string   `json:"theme,omitempty"`
	Accent              string   `json:"accent,omitempty"`
	FontSize            string   `json:"font_size,omitempty"`
	Language            string   `json:"language,omitempty"`
	ReasoningEffort     string   `json:"reasoning_effort,omitempty"`
	MinimizeToTray      bool     `json:"minimize_to_tray,omitempty"`
	KeepServicesOnQuit  *bool    `json:"keep_services_on_quit,omitempty"`
	WorkspaceRoot       *string  `json:"workspace_root"`
	InferyardPath       *string  `json:"inferyard_path"`
	ExtraModelsRoots    []string `json:"extra_models_roots,omitempty"`
}

type EffectiveSettings struct {
	WorkspaceRoot      *string  `json:"workspace_root"`
	InferyardPath      *string  `json:"inferyard_path"`
	InferyardVersion   *string  `json:"inferyard_version"`
	InferyardStatus    string   `json:"inferyard_status"`
	ResultsRoot        *string  `json:"results_root"`
	ModelsRoot         *string  `json:"models_root"`
	ExtraModelsRoots   []string `json:"extra_models_roots,omitempty"`
	DownloadSource     string   `json:"download_source"`
	DownloadProxy      *string  `json:"download_proxy,omitempty"`
	Theme              string   `json:"theme"`
	Accent             string   `json:"accent"`
	FontSize           string   `json:"font_size"`
	Language           string   `json:"language"`
	ReasoningEffort    string   `json:"reasoning_effort"`
	MinimizeToTray     bool     `json:"minimize_to_tray"`
	KeepServicesOnQuit bool     `json:"keep_services_on_quit"`
	ReportsRoot        *string  `json:"reports_root"`
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
	Passed     int64    `json:"passed"`
	Valid      int64    `json:"valid"`
	Unscorable int64    `json:"unscorable"`
	Failed     int64    `json:"failed"`
	Missing    int64    `json:"missing_score"`
	Rate       *float64 `json:"rate"`
}

func (s RunScore) RateGap() string {
	switch {
	case s.Rate != nil:
		return ""
	case s.Unscorable > 0:
		return l10n.TR("core.score.gap.unscorable")
	case s.Missing > 0:
		return l10n.TR("core.score.gap.missing")
	}
	return l10n.TR("core.score.gap.incomplete")
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
	CandidatePath    *string           `json:"candidate_path,omitempty"`
	EngineFamily     string            `json:"engine_family,omitempty"`
	PreparationStage string            `json:"preparation_stage,omitempty"`
	AwaitingService  bool              `json:"awaiting_service,omitempty"`
	ContextSpeed     *ContextSpeedTask `json:"context_speed,omitempty"`
	Exclusivity      string            `json:"exclusivity,omitempty"`
	TaskId           string            `json:"task_id"`
	Kind             string            `json:"kind"`
	State            string            `json:"state"`
	Queued           bool              `json:"queued"`
	ConfigPath       *string           `json:"config_path"`
	ResumeFrom       *string           `json:"resume_from"`
	Bundle           *string           `json:"bundle"`
	RunId            *string           `json:"run_id"`
	RunPath          *string           `json:"run_path"`
	CreatedAt        string            `json:"created_at"`
	StartedAt        *string           `json:"started_at"`
	FinishedAt       *string           `json:"finished_at"`
	Exit             *RunExit          `json:"exit"`
	Error            *ApiError         `json:"error"`
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
	Prompt        *string  `json:"prompt,omitempty"`
	Response      *string  `json:"response,omitempty"`
}

type RunProgress struct {
	Phase         *string        `json:"phase"`
	Done          int64          `json:"done"`
	Total         *int64         `json:"total"`
	CurrentCaseId *string        `json:"current_case_id"`
	LastSeq       int64          `json:"last_seq"`
	Cases         []CaseProgress `json:"cases"`
}

type ResourceSource struct {
	Metric    string `json:"metric"`
	Collector string `json:"collector"`
	Name      string `json:"name"`
	ClockID   string `json:"clock_id"`
	Unit      string `json:"unit"`
	Semantics string `json:"semantics"`
}

type ResourceSample struct {
	Source         ResourceSource `json:"source"`
	Seq            int64          `json:"seq"`
	Utc            string         `json:"utc"`
	ReadStartedNs  int64          `json:"read_started_ns"`
	ReadFinishedNs int64          `json:"read_finished_ns"`
	Phase          string         `json:"phase"`
	RequestID      *string        `json:"request_id"`
	Value          *float64       `json:"value"`
	MissingReason  *string        `json:"missing_reason"`
}

type StartRunsRequest struct {
	ConfigPaths        []string `json:"config_paths"`
	AllowSingleBundle  *bool    `json:"allow_single_bundle"`
	InitialExclusivity string   `json:"initial_exclusivity,omitempty"`
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

type ReportListRun struct {
	RunID       string  `json:"run_id"`
	Model       string  `json:"model"`
	BundleLabel string  `json:"bundle_label"`
	Title       *string `json:"title"`
	Quality     ReportQuality
}

type ReportSummary struct {
	ReportId     string          `json:"report_id"`
	Path         string          `json:"path"`
	ModifiedAt   *string         `json:"modified_at"`
	RunIds       []string        `json:"run_ids"`
	RunPaths     []*string       `json:"run_paths"`
	RunSummaries []ReportListRun `json:"run_summaries"`
	Unreadable   bool            `json:"unreadable"`
}

type CreateReportRequest struct {
	RunPaths []string `json:"run_paths"`
}

type DownloadResult struct {
	Bytes  int64  `json:"bytes"`
	Sha256 string `json:"sha256"`
}

type DownloadTask struct {
	Kind               string          `json:"kind"`
	DisplayName        string          `json:"display_name,omitempty"`
	ProgressReason     *string         `json:"progress_reason"`
	EnginePath         *string         `json:"engine_path"`
	TaskId             string          `json:"task_id"`
	State              string          `json:"state"`
	SourceUrl          string          `json:"source_url"`
	RequestedSourceUrl string          `json:"requested_source_url,omitempty"`
	OutName            string          `json:"out_name"`
	OutDir             string          `json:"out_dir"`
	FileName           *string         `json:"file_name"`
	DownloadedBytes    int64           `json:"downloaded_bytes"`
	TotalBytes         *int64          `json:"total_bytes"`
	BytesPerSecond     *float64        `json:"bytes_per_second"`
	Stage              string          `json:"stage,omitempty"`
	Step               *int            `json:"step,omitempty"`
	StepTotal          *int            `json:"step_total,omitempty"`
	Percent            *float64        `json:"percent,omitempty"`
	Detail             *string         `json:"detail,omitempty"`
	ModelSource        string          `json:"model_source,omitempty"`
	LogPath            *string         `json:"log_path,omitempty"`
	InstallationPath   *string         `json:"installation_path,omitempty"`
	ModelConfigPath    *string         `json:"model_config_path,omitempty"`
	StartedAt          string          `json:"started_at"`
	FinishedAt         *string         `json:"finished_at"`
	Result             *DownloadResult `json:"result"`
	Error              *ApiError       `json:"error"`
}

type CreateDownloadRequest struct {
	SourceUrl string `json:"source_url"`
	OutName   string `json:"out_name"`
}

func (t DownloadTask) MarshalJSON() ([]byte, error) {
	type task DownloadTask
	var downloaded *int64
	var rate *float64
	hasProgress := t.Kind != "engine" || t.DownloadedBytes > 0 || t.BytesPerSecond != nil || (t.TotalBytes != nil && t.ProgressReason == nil) || t.Percent != nil
	if hasProgress {
		downloaded = &t.DownloadedBytes
		rate = t.BytesPerSecond
	}
	return json.Marshal(struct {
		task
		DownloadedBytes *int64   `json:"downloaded_bytes"`
		BytesPerSecond  *float64 `json:"bytes_per_second"`
	}{task(t), downloaded, rate})
}
