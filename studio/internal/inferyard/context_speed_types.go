package inferyard

import "encoding/json"

// ContextSpeedResult is an offline projection. Only the CLI authenticates evidence bytes.
type ContextSpeedResult struct {
	ReadError     *SpeedReadError  `json:"-"`
	SchemaVersion int              `json:"schema_version"`
	Kind          string           `json:"kind"`
	ReadState     string           `json:"read_state"`
	Profile       string           `json:"profile"`
	Source        SpeedSource      `json:"source"`
	Metadata      SpeedMetadata    `json:"metadata"`
	Execution     SpeedExecution   `json:"execution"`
	Measurement   SpeedMeasurement `json:"measurement"`
	Samples       []SpeedSample    `json:"samples"`
	Summary       *SpeedSummary    `json:"summary"`
}

type SpeedRef struct {
	Path   string          `json:"path"`
	SHA256 string          `json:"sha256"`
	Member json.RawMessage `json:"member,omitempty"`
}

type SpeedValue[T any] struct {
	Value         *T         `json:"value"`
	MissingReason *string    `json:"missing_reason"`
	EvidenceRefs  []SpeedRef `json:"evidence_refs"`
}

type SpeedSource struct {
	BatchMetadata SpeedRef `json:"batch_metadata"`
	Plan          SpeedRef `json:"plan"`
	Config        SpeedRef `json:"config"`
	Bundle        SpeedRef `json:"bundle"`
	PlanSHA256    string   `json:"plan_sha256"`
	ExperimentID  string   `json:"experiment_id"`
	WorkloadID    string   `json:"workload_id"`
}

type SpeedMetadata struct {
	Model struct {
		DisplayName    string `json:"display_name"`
		SHA256         string `json:"sha256"`
		TemplateSHA256 string `json:"template_sha256"`
	} `json:"model"`
	Engine struct {
		Adapter      string `json:"adapter"`
		Release      string `json:"release"`
		BinarySHA256 string `json:"binary_sha256"`
	} `json:"engine"`
	Bundle struct {
		BundleID string `json:"bundle_id"`
		Version  string `json:"version"`
	} `json:"bundle"`
	SelectedContextTokens int `json:"selected_context_tokens"`
}

type SpeedExecution struct {
	Counts struct {
		Planned     int `json:"planned"`
		Completed   int `json:"completed"`
		Failed      int `json:"failed"`
		Cancelled   int `json:"cancelled"`
		Invalid     int `json:"invalid"`
		NotExecuted int `json:"not_executed"`
	} `json:"counts"`
	StopReason SpeedValue[string] `json:"stop_reason"`
}

type SpeedMeasurement struct {
	Status     string   `json:"status"`
	ValidCount int      `json:"valid_count"`
	Reasons    []string `json:"reasons"`
}

type SpeedRunRef struct {
	Path           string   `json:"path"`
	RunID          string   `json:"run_id"`
	Inputs         SpeedRef `json:"inputs"`
	ManifestSHA256 *string  `json:"manifest_sha256"`
	Integrity      string   `json:"integrity"`
}

type SpeedSample struct {
	TrialID        string             `json:"trial_id"`
	RepeatIndex    int                `json:"repeat_index"`
	CaseID         string             `json:"case_id"`
	Run            *SpeedRunRef       `json:"run"`
	RequestID      *string            `json:"request_id"`
	ExecutionState string             `json:"execution_state"`
	ErrorCategory  *string            `json:"error_category"`
	StopReason     SpeedValue[string] `json:"stop_reason"`
	Measurement    struct {
		Valid        bool     `json:"valid"`
		Reasons      []string `json:"reasons"`
		CohortSHA256 *string  `json:"cohort_sha256"`
	} `json:"measurement"`
	Columns SpeedColumns `json:"columns"`
	Proof   SpeedProof   `json:"proof"`
}

type SpeedColumns struct {
	Context      SpeedValue[int64]   `json:"context"`
	PromptLength SpeedValue[int64]   `json:"prompt_length"`
	Time         SpeedValue[float64] `json:"time"`
	Prefill      SpeedValue[float64] `json:"prefill"`
	Decode       SpeedValue[float64] `json:"decode"`
}

type SpeedProof struct {
	Identity    []SpeedRef `json:"identity"`
	TokenBudget *SpeedRef  `json:"token_budget"`
	Request     *SpeedRef  `json:"request"`
	FinalTiming *struct {
		File     SpeedRef    `json:"file"`
		EventSeq int64       `json:"event_seq"`
		Record   SpeedTiming `json:"record"`
	} `json:"final_timing"`
	FixedOutput []SpeedRef `json:"fixed_output"`
}

type SpeedTiming struct {
	Definition    string   `json:"definition"`
	Final         bool     `json:"final"`
	Speculative   bool     `json:"speculative"`
	MissingReason *string  `json:"missing_reason"`
	CacheN        *int64   `json:"cache_n"`
	PromptN       *int64   `json:"prompt_n"`
	PredictedN    *int64   `json:"predicted_n"`
	PromptMS      *float64 `json:"prompt_ms"`
	PredictedMS   *float64 `json:"predicted_ms"`
}

type SpeedStatistics struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

type SpeedSummary struct {
	TrialIDs []string `json:"trial_ids"`
	Columns  struct {
		Context      SpeedValue[int64] `json:"context"`
		PromptLength SpeedValue[int64] `json:"prompt_length"`
		Time         SpeedStatistics   `json:"time"`
		Prefill      SpeedStatistics   `json:"prefill"`
		Decode       SpeedStatistics   `json:"decode"`
	} `json:"columns"`
}

type SpeedReadError struct {
	Code       string  `json:"code"`
	SourcePath *string `json:"source_path"`
}
