package inferyard

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"unicode/utf8"
)

const (
	StudioReportKind          = "studio-report.v1"
	StudioReportSchemaVersion = 3
	StudioMaxSVGBytes         = 512 * 1024
)

type StudioReport struct {
	SchemaVersion int         `json:"schema_version"`
	Kind          string      `json:"kind"`
	Runs          []StudioRun `json:"runs"`
}
type StudioRun struct {
	Source     StudioSource      `json:"source"`
	Model      string            `json:"model"`
	Threads    *int64            `json:"threads"`
	Diagnostic bool              `json:"diagnostic"`
	Facets     StudioFacets      `json:"facets"`
	Profile    StudioProfile     `json:"profile"`
	Summary    StudioSummary     `json:"summary"`
	Dashboard  StudioDashboard   `json:"dashboard"`
	Requests   []StudioRequest   `json:"requests"`
	SVGOutputs []StudioSVGOutput `json:"svg_outputs,omitempty"`
}
type StudioSource struct {
	RunID string `json:"run_id"`
	Path  string `json:"path"`
}
type StudioFacets struct {
	BundleLabel string `json:"bundle_label"`
}
type StudioProfile struct {
	Title   *string       `json:"title"`
	Engine  StudioEngine  `json:"engine"`
	Machine StudioMachine `json:"machine"`
}
type StudioEngine struct {
	BinaryPath *string `json:"binary_path"`
	Adapter    string  `json:"adapter"`
	Backend    string  `json:"backend"`
}
type StudioMachine struct {
	CPU              *string `json:"cpu"`
	LogicalCPUs      *int64  `json:"logical_cpus"`
	MemoryTotalBytes *int64  `json:"memory_total_bytes"`
}
type StudioCounts struct {
	Numerator   *int64 `json:"numerator"`
	Denominator *int64 `json:"denominator"`
	Excluded    *int64 `json:"excluded"`
}
type StudioSummary struct {
	CompletionRate StudioCounts `json:"completion_rate"`
	StopReason     string       `json:"stop_reason"`
}
type StudioQuality struct {
	StudioCounts
	Complete bool    `json:"complete"`
	Reason   *string `json:"reason"`
}
type StudioCategory struct {
	ID    string       `json:"id"`
	Label string       `json:"label"`
	Rate  StudioCounts `json:"rate"`
}
type StudioLatency struct {
	SampleCount *int64   `json:"sample_count"`
	Excluded    *int64   `json:"excluded"`
	Min         *float64 `json:"min"`
	P50         *float64 `json:"p50"`
	P95         *float64 `json:"p95"`
	Max         *float64 `json:"max"`
}

type StudioThroughput struct {
	RequestsPerSecond           *float64 `json:"requests_per_second"`
	RequestsPerSecondReason     *string  `json:"requests_per_second_reason"`
	OutputTokensPerSecond       *float64 `json:"output_tokens_per_second"`
	OutputTokensPerSecondReason *string  `json:"output_tokens_per_second_reason"`
	GoodputPerSecond            *float64 `json:"goodput_per_second"`
	GoodputPerSecondReason      *string  `json:"goodput_per_second_reason"`
}

type StudioOutputBlockIntervals struct {
	IntervalCount int64    `json:"interval_count"`
	RequestCount  int64    `json:"request_count"`
	Min           *float64 `json:"min"`
	P50           *float64 `json:"p50"`
	P95           *float64 `json:"p95"`
	P95Reason     *string  `json:"p95_reason"`
	Max           *float64 `json:"max"`
	Reason        *string  `json:"reason"`
}

type StudioDashboard struct {
	Quality                      StudioQuality               `json:"quality"`
	Categories                   []StudioCategory            `json:"categories"`
	Latency                      StudioLatency               `json:"latency"`
	SampledPeakRSSBytes          *float64                    `json:"sampled_peak_rss_bytes"`
	SampledPeakRSSReason         *string                     `json:"sampled_peak_rss_reason"`
	SampledMinimumAvailableBytes *float64                    `json:"sampled_minimum_available_bytes"`
	MinimumAvailableReason       *string                     `json:"minimum_available_reason"`
	Throughput                   *StudioThroughput           `json:"throughput"`
	OutputBlockIntervals         *StudioOutputBlockIntervals `json:"output_block_intervals"`
}
type StudioRequest struct {
	CaseID        string   `json:"case_id"`
	CategoryLabel string   `json:"category_label"`
	State         string   `json:"state"`
	Quality       *string  `json:"quality"`
	LatencyMS     *float64 `json:"latency_ms"`
}
type StudioSVGOutput struct {
	CaseID string  `json:"case_id"`
	Prompt string  `json:"prompt"`
	Status string  `json:"status"`
	SVG    *string `json:"svg"`
	Bytes  int64   `json:"bytes"`
}

var (
	studioIdentifier  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	studioBackends    = []string{"cpu", "cuda", "hip", "metal"}
	studioCategoryIDs = []string{"instruction", "extraction", "qa", "math", "classification", "structured"}
	studioStates      = []string{"completed", "failed", "cancelled", "invalid", "not_executed"}
	studioQualities   = []string{"pass", "fail", "unscorable", "not_applicable", "not_scored"}
	studioSVGStatuses = []string{"ok", "not_found", "malformed", "too_large"}
)

func ParseStudioReport(raw []byte) (StudioReport, error) {
	var result StudioReport
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return result, errors.New("stdout must be one JSON object")
	}
	if !utf8.Valid(raw) {
		return result, errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(d); err != nil {
		return result, err
	}
	if _, err := d.Token(); err != io.EOF {
		return result, errors.New("trailing JSON")
	}
	if err := validateStudioReport(raw); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, errors.New("invalid studio report: " + err.Error())
	}
	return result, nil
}

func validateStudioReport(raw []byte) error {
	root, err := studioObject(raw)
	if err != nil {
		return err
	}
	if err := studioClosed(root, "schema_version", "kind", "runs"); err != nil {
		return err
	}
	version, err := studioCount(root["schema_version"])
	if err != nil || version != StudioReportSchemaVersion {
		return errors.New("unsupported studio report version")
	}
	if kind, err := studioText(root["kind"]); err != nil || kind != StudioReportKind {
		return errors.New("unsupported studio report kind")
	}
	runs, err := studioArray(root["runs"])
	if err != nil || len(runs) < 1 || len(runs) > 100 {
		return errors.New("invalid studio report runs")
	}
	seen := map[string]bool{}
	for _, rawRun := range runs {
		run, err := studioObject(rawRun)
		if err != nil {
			return err
		}
		id, err := validateStudioRun(run)
		if err != nil {
			return err
		}
		if seen[id] {
			return errors.New("duplicate studio report run")
		}
		seen[id] = true
	}
	return nil
}

func validateStudioRun(run map[string]json.RawMessage) (string, error) {
	required := []string{"source", "model", "threads", "diagnostic", "facets", "profile", "summary", "dashboard", "requests"}
	if err := studioClosedOptional(run, required, []string{"svg_outputs"}); err != nil {
		return "", err
	}
	source, err := studioObject(run["source"])
	if err != nil {
		return "", err
	}
	if err := studioClosed(source, "run_id", "path"); err != nil {
		return "", err
	}
	runID, err := studioText(source["run_id"])
	if err != nil || !studioIdentifier.MatchString(runID) {
		return "", errors.New("invalid studio report run id")
	}
	if _, err := studioText(source["path"]); err != nil {
		return "", err
	}
	if _, err := studioText(run["model"]); err != nil {
		return "", err
	}
	if _, err := studioNullableCount(run["threads"]); err != nil {
		return "", err
	}
	if _, err := studioBool(run["diagnostic"]); err != nil {
		return "", err
	}
	if err := validateStudioFacets(run["facets"]); err != nil {
		return "", err
	}
	if err := validateStudioProfile(run["profile"]); err != nil {
		return "", err
	}
	if err := validateStudioSummary(run["summary"]); err != nil {
		return "", err
	}
	requests, err := studioArray(run["requests"])
	if err != nil {
		return "", err
	}
	for _, rawRequest := range requests {
		if err := validateStudioRequest(rawRequest); err != nil {
			return "", err
		}
	}
	if err := validateStudioDashboard(run["dashboard"], len(requests)); err != nil {
		return "", err
	}
	if raw, ok := run["svg_outputs"]; ok {
		if err := validateStudioSVGOutputs(raw); err != nil {
			return "", err
		}
	}
	return runID, nil
}
