package core

import "inferyard/studio/internal/inferyard"

// Plan and batch references cannot be mistaken for a single run path.
type SpeedPlanRef struct {
	Path           string   `json:"path"`
	SHA256         string   `json:"sha256"`
	TrialIDs       []string `json:"trial_ids"`
	MaxWallSeconds float64  `json:"max_wall_seconds"`
}

type SpeedBatchRef struct {
	Path string                  `json:"path"`
	Runs []inferyard.SpeedRunRef `json:"runs"`
}

// ContextSpeedTask persists identity and lifecycle only, never numerical results.
type ContextSpeedTask struct {
	DisplayModel  string         `json:"display_model,omitempty"`
	DisplayEngine string         `json:"display_engine,omitempty"`
	Completed     int            `json:"completed"`
	Model         string         `json:"model"`
	Engine        string         `json:"engine"`
	Context       int            `json:"context"`
	Stage         string         `json:"stage"`
	Operations    string         `json:"operations"`
	Plan          *SpeedPlanRef  `json:"plan"`
	Batch         *SpeedBatchRef `json:"batch"`
}

type StartContextSpeedRequest struct {
	Model   string
	Engine  string
	Context int
}
